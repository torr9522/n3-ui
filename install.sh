#!/usr/bin/env bash

red='\033[0;31m'
green='\033[0;32m'
yellow='\033[0;33m'
plain='\033[0m'

cur_dir=$(pwd)
INSTALL_SCRIPT_DIR=""
XUI_RAW_BASE="${XUI_RAW_BASE:-https://raw.githubusercontent.com/torr9522/n3-ui/main}"
XUI_REPO_URL="${XUI_REPO_URL:-https://github.com/torr9522/n3-ui.git}"
XUI_REPO_BRANCH="${XUI_REPO_BRANCH:-main}"
INSTALL_MODE="${INSTALL_MODE:-source}"
XUI_RELEASES_BASE="${XUI_RELEASES_BASE:-${XUI_RELEASES_RAW_BASE:-https://github.com/torr9522/n3-ui/releases/download/n3-ui-assets}}"

resolve_install_script_dir() {
    local script_source="${BASH_SOURCE[0]:-$0}"
    local link_dir=""

    case "${script_source}" in
        ""|"-"|bash|/dev/fd/*|/proc/*/fd/*)
            return 0
            ;;
    esac

    if [[ "${script_source}" != /* ]]; then
        script_source="${PWD}/${script_source}"
    fi

    while [[ -L "${script_source}" ]]; do
        link_dir="$(cd -P "$(dirname "${script_source}")" >/dev/null 2>&1 && pwd)" || return 0
        script_source="$(readlink "${script_source}")" || return 0
        [[ "${script_source}" != /* ]] && script_source="${link_dir}/${script_source}"
    done

    INSTALL_SCRIPT_DIR="$(cd -P "$(dirname "${script_source}")" >/dev/null 2>&1 && pwd)" || INSTALL_SCRIPT_DIR=""
}

resolve_install_script_dir

error_exit() {
    echo -e "${red}错误：${plain} $1"
    exit 1
}

warn_msg() {
    echo -e "${yellow}$1${plain}"
}

is_numeric() {
    [[ "${1:-}" =~ ^[0-9]+$ ]]
}

command_exists() {
    command -v "$1" >/dev/null 2>&1
}

install_missing_apt_packages() {
    local packages=("$@")
    local missing=()
    local pkg

    for pkg in "${packages[@]}"; do
        case "${pkg}" in
            curl|wget|openssl)
                if ! command_exists "${pkg}"; then
                    missing+=("${pkg}")
                fi
                ;;
            ca-certificates|unzip|socat)
                if ! dpkg -s "${pkg}" >/dev/null 2>&1; then
                    missing+=("${pkg}")
                fi
                ;;
            sqlite3)
                if ! command_exists sqlite3; then
                    missing+=("${pkg}")
                fi
                ;;
            nftables)
                if ! command_exists nft; then
                    missing+=("${pkg}")
                fi
                ;;
            *)
                if ! dpkg -s "${pkg}" >/dev/null 2>&1; then
                    missing+=("${pkg}")
                fi
                ;;
        esac
    done

    if [[ ${#missing[@]} -eq 0 ]]; then
        echo -e "${green}基础依赖已满足。${plain}"
        return 0
    fi

    echo -e "${yellow}安装缺失依赖: ${missing[*]}${plain}"
    apt-get update || error_exit "apt-get update 失败。"
    DEBIAN_FRONTEND=noninteractive apt-get install -y "${missing[@]}" || error_exit "基础依赖安装失败。"
}

require_systemctl() {
    command -v systemctl >/dev/null 2>&1 || error_exit "当前环境未检测到 systemctl，无法管理 x-ui 服务。"
}

download_file() {
    local output="$1"
    local url="$2"

    if command -v wget >/dev/null 2>&1; then
        wget --no-check-certificate -O "$output" "$url"
    elif command -v curl >/dev/null 2>&1; then
        curl -fL -o "$output" "$url"
    else
        error_exit "未找到 wget 或 curl，无法下载文件：${url}"
    fi
}

copy_or_download() {
    local primary_source="$1"
    local secondary_source="$2"
    local target_path="$3"
    local remote_url="$4"

    if [[ -f "${primary_source}" ]]; then
        cp -f "${primary_source}" "${target_path}"
    elif [[ -f "${secondary_source}" ]]; then
        cp -f "${secondary_source}" "${target_path}"
    else
        download_file "${target_path}" "${remote_url}"
    fi
}

find_local_source_dir() {
    [[ -n "${INSTALL_SCRIPT_DIR}" ]] || return 1

    if [[ -f "${INSTALL_SCRIPT_DIR}/go.mod" && -f "${INSTALL_SCRIPT_DIR}/main.go" && -f "${INSTALL_SCRIPT_DIR}/x-ui.service" ]]; then
        echo "${INSTALL_SCRIPT_DIR}"
        return 0
    fi
    if [[ -f "${INSTALL_SCRIPT_DIR}/x-ui/go.mod" && -f "${INSTALL_SCRIPT_DIR}/x-ui/main.go" && -f "${INSTALL_SCRIPT_DIR}/x-ui/x-ui.service" ]]; then
        echo "${INSTALL_SCRIPT_DIR}/x-ui"
        return 0
    fi
    return 1
}

sync_default_xray_assets() {
    local xray_zip="/tmp/xray-${arch}.zip"
    local xray_url="${XUI_XRAY_URL:-${XUI_RELEASES_BASE}/xray-linux-${arch}.zip}"
    local candidate=""
    local local_candidates=(
        "${INSTALL_SCRIPT_DIR}/releases/xray-linux-${arch}.zip"
        "/usr/local/x-ui/releases/xray-linux-${arch}.zip"
    )

    case "${arch}" in
        amd64|arm64)
            ;;
        *)
            echo -e "${red}不支持的 xray 架构: ${arch}${plain}"
            return 1
            ;;
    esac

    command -v unzip >/dev/null 2>&1 || error_exit "未找到 unzip，无法同步默认 xray 版本。"

    rm -f "${xray_zip}"
    for candidate in "${local_candidates[@]}"; do
        if [[ -f "${candidate}" ]]; then
            cp -f "${candidate}" "${xray_zip}"
            break
        fi
    done

    if [[ ! -f "${xray_zip}" ]]; then
        echo -e "${yellow}同步默认 xray 资源包: ${xray_url}${plain}"
        if ! download_file "${xray_zip}" "${xray_url}"; then
            echo -e "${red}下载默认 xray 资源包失败${plain}"
            return 1
        fi
    fi

    if ! unzip -t "${xray_zip}" >/dev/null 2>&1; then
        echo -e "${red}xray 资源包校验失败: ${xray_zip}${plain}"
        rm -f "${xray_zip}"
        return 1
    fi

    mkdir -p /usr/local/x-ui/bin
    if ! unzip -p "${xray_zip}" xray > "/usr/local/x-ui/bin/xray-linux-${arch}"; then
        echo -e "${red}提取 xray 主程序失败${plain}"
        rm -f "${xray_zip}"
        return 1
    fi
    if ! unzip -p "${xray_zip}" geoip.dat > /usr/local/x-ui/bin/geoip.dat; then
        echo -e "${red}提取 geoip.dat 失败${plain}"
        rm -f "${xray_zip}"
        return 1
    fi
    if ! unzip -p "${xray_zip}" geosite.dat > /usr/local/x-ui/bin/geosite.dat; then
        echo -e "${red}提取 geosite.dat 失败${plain}"
        rm -f "${xray_zip}"
        return 1
    fi

    chmod +x "/usr/local/x-ui/bin/xray-linux-${arch}" /usr/local/x-ui/bin/geoip.dat /usr/local/x-ui/bin/geosite.dat
    rm -f "${xray_zip}"
}

# 检查 root
[[ $EUID -ne 0 ]] && echo -e "${red}错误：${plain} 必须使用 root 用户运行此脚本！\n" && exit 1

# 检查操作系统
if [[ -f /etc/redhat-release ]]; then
    release="centos"
elif cat /etc/issue | grep -Eqi "debian"; then
    release="debian"
elif cat /etc/issue | grep -Eqi "ubuntu"; then
    release="ubuntu"
elif cat /etc/issue | grep -Eqi "centos|red hat|redhat"; then
    release="centos"
elif cat /proc/version | grep -Eqi "debian"; then
    release="debian"
elif cat /proc/version | grep -Eqi "ubuntu"; then
    release="ubuntu"
elif cat /proc/version | grep -Eqi "centos|red hat|redhat"; then
    release="centos"
else
    echo -e "${red}未检测到系统版本，请联系脚本作者！${plain}\n" && exit 1
fi

arch=$(arch)

if [[ $arch == "x86_64" || $arch == "x64" || $arch == "amd64" ]]; then
    arch="amd64"
elif [[ $arch == "aarch64" || $arch == "arm64" ]]; then
    arch="arm64"
else
    error_exit "不支持的系统架构: ${arch}，当前仅支持 amd64 / arm64。"
fi

echo "架构: ${arch}"

if [[ "$(getconf LONG_BIT 2>/dev/null || echo 0)" != "64" ]]; then
    echo "本软件不支持 32 位系统，请使用 64 位操作系统。"
    exit 1
fi

os_version=""
os_version_major=""

# 检测系统版本号
if [[ -f /etc/os-release ]]; then
    os_version=$(awk -F= '/^VERSION_ID=/{gsub(/"/, "", $2); print $2; exit}' /etc/os-release)
fi
if [[ -z "$os_version" && -f /etc/lsb-release ]]; then
    os_version=$(awk -F= '/^DISTRIB_RELEASE=/{gsub(/"/, "", $2); print $2; exit}' /etc/lsb-release)
fi
if [[ -n "${os_version}" ]]; then
    os_version_major="${os_version%%.*}"
fi
if [[ -n "${os_version_major}" ]] && ! is_numeric "${os_version_major}"; then
    warn_msg "无法可靠识别系统版本号（${os_version}），跳过最低版本检查。"
    os_version_major=""
fi

if [[ x"${release}" == x"centos" && -n "${os_version_major}" ]]; then
    if (( os_version_major <= 6 )); then
        echo -e "${red}请使用 CentOS 7 或更高版本的系统！${plain}\n" && exit 1
    fi
elif [[ x"${release}" == x"ubuntu" && -n "${os_version_major}" ]]; then
    if (( os_version_major < 16 )); then
        echo -e "${red}请使用 Ubuntu 16 或更高版本的系统！${plain}\n" && exit 1
    fi
elif [[ x"${release}" == x"debian" && -n "${os_version_major}" ]]; then
    if (( os_version_major < 8 )); then
        echo -e "${red}请使用 Debian 8 或更高版本的系统！${plain}\n" && exit 1
    fi
fi

install_base() {
    if [[ x"${release}" == x"centos" ]]; then
        yum install wget curl tar jq nftables sqlite python3 unzip logrotate git -y || error_exit "基础依赖安装失败。"
    else
        install_missing_apt_packages wget curl tar jq nftables sqlite3 python3 unzip logrotate git ca-certificates openssl socat cron
    fi
}


install_build_toolchain() {
    if [[ x"${release}" == x"centos" ]]; then
        yum install gcc gcc-c++ make -y || error_exit "编译依赖安装失败。"
    else
        apt-get update || error_exit "apt-get update 失败。"
        DEBIAN_FRONTEND=noninteractive apt-get install -y build-essential || error_exit "编译依赖安装失败。"
    fi
}

ensure_go_toolchain() {
    local need_install=0
    if ! command -v go >/dev/null 2>&1; then
        need_install=1
    else
        local gv
        gv=$(go version 2>/dev/null | awk '{print $3}' | sed 's/go//')
        local major mino
        major=$(echo "$gv" | cut -d. -f1)
        minor=$(echo "$gv" | cut -d. -f2)
        if [[ -z "$major" || -z "$minor" || "$major" -lt 1 || ( "$major" -eq 1 && "$minor" -lt 16 ) ]]; then
            need_install=1
        fi
    fi

    if [[ "$need_install" -eq 0 ]]; then
        local go_path
        go_path="$(command -v go)"
        if [[ "${go_path}" != "/usr/local/bin/go" && ! -x /usr/local/bin/go ]]; then
            ln -sf "${go_path}" /usr/local/bin/go || return 1
        fi
        return 0
    fi

    local go_arch="${arch}"
    local go_ver="1.22.7"
    local go_tgz="/tmp/go${go_ver}.linux-${go_arch}.tar.gz"

    rm -f "${go_tgz}"
    if ! download_file "${go_tgz}" "https://go.dev/dl/go${go_ver}.linux-${go_arch}.tar.gz"; then
        echo -e "${red}下载 Go ${go_ver} (${go_arch}) 失败${plain}"
        return 1
    fi
    if ! tar -tzf "${go_tgz}" >/dev/null 2>&1; then
        echo -e "${red}下载的 Go 安装包损坏：${go_tgz}${plain}"
        return 1
    fi
    rm -rf /usr/local/go
    if ! tar -C /usr/local -xzf "${go_tgz}"; then
        echo -e "${red}解压 Go 安装包失败：${go_tgz}${plain}"
        return 1
    fi
    if ! ln -sf /usr/local/go/bin/go /usr/local/bin/go; then
        echo -e "${red}配置 Go 命令失败${plain}"
        return 1
    fi
}

ensure_xui_binary_compatible() {
    if /usr/local/x-ui/x-ui -h >/dev/null 2>&1; then
        return 0
    fi

    echo -e "${yellow}Detected incompatible prebuilt x-ui binary, rebuilding on this server...${plain}"
    install_build_toolchain || return 1
    ensure_go_toolchain || return 1

    cd /usr/local/x-ui || return 1
    if ! CGO_ENABLED=1 GO111MODULE=on /usr/local/bin/go build -o x-ui .; then
        echo -e "${red}本机重编译 x-ui 失败${plain}"
        return 1
    fi
    chmod +x /usr/local/x-ui/x-ui
    if ! /usr/local/x-ui/x-ui -h >/dev/null 2>&1; then
        echo -e "${red}重编译后的 x-ui 仍无法运行${plain}"
        return 1
    fi
}

ensure_firewall_ready() {
    if ! command -v nft >/dev/null 2>&1; then
        echo -e "${yellow}未检测到 nftables，正在自动安装...${plain}"
        if [[ x"${release}" == x"centos" ]]; then
            yum install nftables -y || error_exit "nftables 安装失败。"
        else
            apt-get update || error_exit "apt-get update 失败。"
            DEBIAN_FRONTEND=noninteractive apt-get install -y nftables || error_exit "nftables 安装失败。"
        fi
    fi
    if command -v modprobe >/dev/null 2>&1; then
        modprobe nf_tables >/dev/null 2>&1 || true
    fi
    if command -v systemctl >/dev/null 2>&1 && systemctl list-unit-files | grep -q '^nftables\.service'; then
        systemctl enable --now nftables >/dev/null 2>&1 || true
    fi
}

install_iptables_shim() {
    mkdir -p /usr/local/x-ui/shims

    cat >/usr/local/x-ui/shims/iptables <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
MODE="${XUI_IPTABLES_MODE:-off}"
if [[ "${MODE}" == "off" ]]; then
    if [[ "${*}" == *"-L INPUT"* ]]; then
        echo "Chain INPUT (policy ACCEPT)"
        echo "target     prot opt source               destination"
        echo "xui-block-chain  tcp  --  0.0.0.0/0      0.0.0.0/0"
    elif [[ "${*}" == *"-nvL xui-block-chain"* ]]; then
        echo "Chain xui-block-chain (0 references)"
        echo "pkts bytes target     prot opt in     out     source               destination"
        echo "0    0 DROP       tcp  --  *      *       0.0.0.0/0            0.0.0.0/0"
    fi
    exit 0
fi
exec /usr/sbin/iptables "$@"
EOF
    chmod +x /usr/local/x-ui/shims/iptables

    cat >/usr/local/x-ui/shims/ip6tables <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
MODE="${XUI_IPTABLES_MODE:-off}"
if [[ "${MODE}" == "off" ]]; then
    exit 0
fi
exec /usr/sbin/ip6tables "$@"
EOF
    chmod +x /usr/local/x-ui/shims/ip6tables

    mkdir -p /etc/systemd/system/x-ui.service.d
    cat >/etc/systemd/system/x-ui.service.d/10-iptables-shim.conf <<'EOF'
[Service]
Environment=XUI_IPTABLES_MODE=off
Environment=PATH=/usr/local/x-ui/shims:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
EOF
}

install_portlimit_sync() {
    local base_url="${XUI_PORTLIMIT_BASE_URL:-${XUI_RAW_BASE}}"
    local script_dir="${INSTALL_SCRIPT_DIR}"

    if ! copy_or_download "${script_dir}/xui-portlimit-sync.sh" "/usr/local/x-ui/xui-portlimit-sync.sh" "/usr/local/bin/xui-portlimit-sync.sh" "${base_url}/xui-portlimit-sync.sh"; then
        echo -e "${red}安装 xui-portlimit-sync.sh 失败${plain}"
        return 1
    fi

    if ! copy_or_download "${script_dir}/xui-portlimit-sync.service" "/usr/local/x-ui/xui-portlimit-sync.service" "/etc/systemd/system/xui-portlimit-sync.service" "${base_url}/xui-portlimit-sync.service"; then
        echo -e "${red}安装 xui-portlimit-sync.service 失败${plain}"
        return 1
    fi

    if ! copy_or_download "${script_dir}/xui-portlimit-sync.timer" "/usr/local/x-ui/xui-portlimit-sync.timer" "/etc/systemd/system/xui-portlimit-sync.timer" "${base_url}/xui-portlimit-sync.timer"; then
        echo -e "${red}安装 xui-portlimit-sync.timer 失败${plain}"
        return 1
    fi

    rm -f /etc/systemd/system/xui-portlimit-sync.time
    chmod +x /usr/local/bin/xui-portlimit-sync.sh
    if ! systemctl daemon-reload; then
        echo -e "${red}systemd 重新加载失败${plain}"
        return 1
    fi
    if ! systemctl enable --now xui-portlimit-sync.timer; then
        echo -e "${red}启用 xui-portlimit-sync.timer 失败${plain}"
        return 1
    fi
    XUI_PORTLIMIT_FORCE_REBUILD=1 systemctl start xui-portlimit-sync.service || true
}

install_access_logrotate() {
    command -v logrotate >/dev/null 2>&1 || return 0

    cat >/etc/logrotate.d/x-ui-xray-access <<'EOF'
/var/log/xray/access.log {
    daily
    rotate 7
    missingok
    notifempty
    compress
    delaycompress
    copytruncate
    create 0644 root root
}
EOF

    if command -v systemctl >/dev/null 2>&1 && systemctl list-unit-files | grep -q '^logrotate\.timer'; then
        systemctl enable --now logrotate.timer >/dev/null 2>&1 || true
    fi
}

# ── 安装后自动随机配置（不再询问用户）────────────────────────────────────────
AUTO_USERNAME=""
AUTO_PASSWORD=""
AUTO_PORT=""
AUTO_UI_URL=""

is_private_ipv4() {
    local ip="$1"
    if [[ ! "${ip}" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]]; then
        return 1
    fi
    IFS='.' read -r o1 o2 o3 o4 <<<"${ip}"
    if ((o1 == 10 || o1 == 127)); then
        return 0
    fi
    if ((o1 == 169 && o2 == 254)); then
        return 0
    fi
    if ((o1 == 192 && o2 == 168)); then
        return 0
    fi
    if ((o1 == 172 && o2 >= 16 && o2 <= 31)); then
        return 0
    fi
    if ((o1 == 100 && o2 >= 64 && o2 <= 127)); then
        return 0
    fi
    return 1
}

get_panel_public_host() {
    local ip=""
    local probe_urls=(
        "https://api64.ipify.org"
        "https://ipv4.icanhazip.com"
        "https://ifconfig.me/ip"
    )

    for url in "${probe_urls[@]}"; do
        ip=$(curl -4fsS --max-time 3 "${url}" 2>/dev/null | tr -d '[:space:]')
        if [[ "${ip}" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]] && ! is_private_ipv4 "${ip}"; then
            echo "${ip}"
            return 0
        fi
    done

    ip=$(ip -4 route get 1.1.1.1 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i=="src"){print $(i+1); exit}}')
    if [[ "${ip}" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]] && ! is_private_ipv4 "${ip}"; then
        echo "${ip}"
        return 0
    fi

    ip=$(hostname -I 2>/dev/null | tr ' ' '\n' | awk '/^[0-9]+(\.[0-9]+){3}$/ {print; exit}')
    if [[ "${ip}" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]] && ! is_private_ipv4 "${ip}"; then
        echo "${ip}"
        return 0
    fi

    return 1
}

config_after_install() {
    echo -e "${yellow}正在自动生成面板登录信息...${plain}"

    # 随机用户名：10 位字母+数字（循环确保足够长度）
    while true; do
        AUTO_USERNAME=$(head -c 32 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 10)
        [[ ${#AUTO_USERNAME} -eq 10 ]] && break
    done
    # 随机密码：16 位字母+数字
    while true; do
        AUTO_PASSWORD=$(head -c 48 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 16)
        [[ ${#AUTO_PASSWORD} -eq 16 ]] && break
    done
    # 随机端口：10000-60000（shuf 优先，fallback 用 awk）
    if command -v shuf >/dev/null 2>&1; then
        AUTO_PORT=$(shuf -i 10000-60000 -n 1)
    else
        AUTO_PORT=$(awk 'BEGIN{srand(); print int(rand()*50001)+10000}')
    fi

    if ! /usr/local/x-ui/x-ui setting -username "${AUTO_USERNAME}" -password "${AUTO_PASSWORD}"; then
        error_exit "面板账号密码初始化失败。"
    fi
    echo -e "${yellow}账户与密码已随机生成并设置完成${plain}"

    if ! /usr/local/x-ui/x-ui setting -port "${AUTO_PORT}"; then
        error_exit "面板端口初始化失败。"
    fi
    echo -e "${yellow}面板端口已随机生成并设置完成${plain}"

    # 优先展示公网可访问地址，避免默认给出内网地址
    local server_ip
    server_ip=$(get_panel_public_host || true)
    if [[ -n "${server_ip}" ]]; then
        AUTO_UI_URL="http://${server_ip}:${AUTO_PORT}"
    else
        AUTO_UI_URL="http://<服务器公网IP>:${AUTO_PORT}"
    fi
}

install_x-ui() {
    require_systemctl
    systemctl stop x-ui 2>/dev/null || true
    cd /usr/local/ || error_exit "无法进入 /usr/local 目录。"

    local last_version
    local package_arch="${arch}"
    local package_file
    local url
    local local_source_dir=""
    if [[ $# -eq 0 || -z "${1:-}" ]]; then
        last_version="n3-ui-source"
    else
        last_version="$1"
    fi
    local_source_dir="$(find_local_source_dir || true)"
    if [[ -e /usr/local/x-ui/ ]]; then
        rm -rf /usr/local/x-ui/
    fi

    if [[ -n "${local_source_dir}" ]]; then
        echo -e "install source: local source ${local_source_dir}"
        if ! cp -a "${local_source_dir}" /usr/local/x-ui; then
            error_exit "复制本地源码到 /usr/local/x-ui 失败。"
        fi
        rm -rf /usr/local/x-ui/.git
        cd /usr/local/x-ui || error_exit "无法进入 /usr/local/x-ui 目录。"
        install_build_toolchain || error_exit "编译依赖安装失败。"
        ensure_go_toolchain || error_exit "Go 工具链安装失败。"
        if ! CGO_ENABLED=1 GO111MODULE=on /usr/local/bin/go build -o x-ui .; then
            error_exit "本地源码编译 x-ui 失败。"
        fi
        cd /usr/local/ || error_exit "无法进入 /usr/local 目录。"
    elif [[ "${INSTALL_MODE}" == "source" ]]; then
        local build_root
        local source_dir
        build_root="$(mktemp -d /tmp/n3-ui-build.XXXXXX)"
        source_dir="${build_root}/repo"
        echo -e "install source: ${XUI_REPO_URL} (${XUI_REPO_BRANCH})"
        install_build_toolchain || error_exit "编译依赖安装失败。"
        ensure_go_toolchain || error_exit "Go 工具链安装失败。"
        if ! git clone --depth 1 --branch "${XUI_REPO_BRANCH}" "${XUI_REPO_URL}" "${source_dir}"; then
            rm -rf "${build_root}"
            error_exit "clone n3-ui 源码失败。"
        fi
        cd "${source_dir}" || {
            rm -rf "${build_root}"
            error_exit "无法进入 n3-ui 源码目录。"
        }
        if ! CGO_ENABLED=1 GO111MODULE=on /usr/local/bin/go build -o x-ui .; then
            rm -rf "${build_root}"
            error_exit "源码编译 x-ui 失败。"
        fi
        cd /usr/local/ || {
            rm -rf "${build_root}"
            error_exit "无法进入 /usr/local 目录。"
        }
        if ! cp -a "${source_dir}" /usr/local/x-ui; then
            rm -rf "${build_root}"
            error_exit "复制 n3-ui 源码到 /usr/local/x-ui 失败。"
        fi
        rm -rf /usr/local/x-ui/.git
        rm -rf "${build_root}"
    else
        if [[ "${package_arch}" == "arm64" ]]; then
            echo -e "${yellow}Only amd64 local release is provided, fallback to amd64 package and rebuild locally.${plain}"
            package_arch="amd64"
        fi
        url="${XUI_PACKAGE_URL:-${XUI_RELEASES_BASE}/x-ui-linux-${package_arch}.tar.gz}"
        package_file="/usr/local/x-ui-linux-${package_arch}.tar.gz"
        echo -e "install source: ${url}"
        if ! download_file "${package_file}" "${url}"; then
            error_exit "download failed, please check n3-ui release assets"
        fi
        if ! tar -tzf "${package_file}" >/dev/null 2>&1; then
            error_exit "下载的 x-ui 安装包损坏：${package_file}"
        fi
        if ! tar zxf "${package_file}"; then
            error_exit "解压 x-ui 安装包失败。"
        fi
        rm -f "${package_file}"
    fi
    cd x-ui || error_exit "解压后未找到 x-ui 目录，安装失败"
    chmod +x x-ui || error_exit "设置 x-ui 可执行权限失败。"
    chmod +x bin/xray-linux-* 2>/dev/null || true
    sync_default_xray_assets || error_exit "同步默认 xray 版本失败。"
    cp -f x-ui.service /etc/systemd/system/ || error_exit "安装 x-ui.service 失败。"
    if ! copy_or_download "${INSTALL_SCRIPT_DIR}/x-ui.sh" "/usr/local/x-ui/x-ui.sh" "/usr/bin/x-ui" "${XUI_RAW_BASE}/x-ui.sh"; then
        error_exit "下载 x-ui 管理脚本失败。"
    fi
    [[ -f /usr/local/x-ui/x-ui.sh ]] && chmod +x /usr/local/x-ui/x-ui.sh
    chmod +x /usr/bin/x-ui || error_exit "设置 /usr/bin/x-ui 可执行权限失败。"
    ensure_xui_binary_compatible || error_exit "x-ui 二进制兼容性修复失败。"

    # ── 自动配置随机账号 ───────────────────────────────────────────────────────
    config_after_install

    systemctl daemon-reload || error_exit "systemd 重新加载失败。"
    systemctl enable x-ui || error_exit "设置 x-ui 开机自启失败。"
    systemctl start x-ui || error_exit "x-ui 启动失败，请检查 journalctl -u x-ui -n 50 --no-pager。"
    install_iptables_shim
    systemctl daemon-reload || error_exit "systemd 重新加载失败。"
    systemctl restart x-ui || error_exit "x-ui 重启失败，请检查 journalctl -u x-ui -n 50 --no-pager。"
    if ! install_portlimit_sync; then
        warn_msg "xui-portlimit-sync 安装失败，不影响面板启动，可稍后手动重试。"
    fi
    install_access_logrotate

    local display_version=""
    local display_name="n3-ui"
    if [[ -f /usr/local/x-ui/config/version ]]; then
        display_version="$(tr -d '[:space:]' < /usr/local/x-ui/config/version)"
    fi
    if [[ -n "${display_version}" ]]; then
        display_version="${display_version#v}"
        display_name="n3-ui v${display_version}"
    elif [[ -n "${last_version}" ]]; then
        display_name="n3-ui ${last_version}"
    fi

    # ── 安装完成，展示面板信息 ─────────────────────────────────────────────────
    echo -e ""
    echo -e "${green}================================================================${plain}"
    echo -e "${green}  ${display_name} 安装完成，面板已启动！${plain}"
    echo -e "${green}================================================================${plain}"
    echo -e ""
    echo -e "  ${yellow}面板登录信息${plain}"
    echo -e "  ┌─────────────────────────────────────────────┐"
    echo -e "  │  登录地址：${green}${AUTO_UI_URL}${plain}"
    echo -e "  │  用 户 名：${green}${AUTO_USERNAME}${plain}"
    echo -e "  │  密    码：${green}${AUTO_PASSWORD}${plain}"
    echo -e "  │  端    口：${green}${AUTO_PORT}${plain}"
    echo -e "  └─────────────────────────────────────────────┘"
    echo -e ""
    echo -e "  ${yellow}提示：${plain}如忘记登录信息，可输入 ${green}x-ui${plain} 并选择菜单选项查看"
    echo -e ""
    echo -e "${green}================================================================${plain}"
    echo -e "  x-ui 管理命令："
    echo -e "  ┌─────────────────────────────────────────────┐"
    echo -e "  │  x-ui              显示管理菜单（功能更多）"
    echo -e "  │  x-ui start        启动 x-ui 面板"
    echo -e "  │  x-ui stop         停止 x-ui 面板"
    echo -e "  │  x-ui restart      重启 x-ui 面板"
    echo -e "  │  x-ui status       查看 x-ui 状态"
    echo -e "  │  x-ui enable       设置开机自启"
    echo -e "  │  x-ui disable      取消开机自启"
    echo -e "  │  x-ui log          查看 x-ui 日志"
    echo -e "  │  x-ui update       更新 x-ui 面板"
    echo -e "  │  x-ui install      安装 x-ui 面板"
    echo -e "  │  x-ui uninstall    卸载 x-ui 面板"
    echo -e "  │  x-ui geo          更新 geo 数据"
    echo -e "  └─────────────────────────────────────────────┘"
    echo -e "${green}================================================================${plain}"
    echo -e ""
}

echo -e "${green}开始安装依赖...${plain}"
install_base
ensure_firewall_ready
install_x-ui "$@"
