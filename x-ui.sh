#!/bin/bash

red='\033[0;31m'
green='\033[0;32m'
yellow='\033[0;33m'
plain='\033[0m'

XUI_RAW_BASE="${XUI_RAW_BASE:-https://raw.githubusercontent.com/torr9522/n3-ui/main}"
XUI_LOCAL_INSTALL_SCRIPT="/usr/local/x-ui/install.sh"
XUI_LOCAL_SHELL_SCRIPT="/usr/local/x-ui/x-ui.sh"
XUI_BBR_URL="${XUI_BBR_URL:-${XUI_RAW_BASE}/scripts/bbr.sh}"
XUI_ACME_INSTALL_URL="${XUI_ACME_INSTALL_URL:-${XUI_RAW_BASE}/scripts/acme_install.sh}"
XUI_CERT_DIR="/etc/x-ui/certs"
XUI_DB_PATH="/etc/x-ui/x-ui.db"

#consts for log check and clear,unit:M
declare -r DEFAULT_LOG_FILE_DELETE_TRIGGER=35

# consts for geo update
PATH_FOR_GEO_IP='/usr/local/x-ui/bin/geoip.dat'
PATH_FOR_CONFIG='/usr/local/x-ui/bin/config.json'
PATH_FOR_GEO_SITE='/usr/local/x-ui/bin/geosite.dat'
URL_FOR_GEO_IP="${XUI_GEOIP_URL:-${XUI_RAW_BASE}/bin/geoip.dat}"
URL_FOR_GEO_SITE="${XUI_GEOSITE_URL:-${XUI_RAW_BASE}/bin/geosite.dat}"

#Add some basic function here
function LOGD() {
    echo -e "${yellow}[DEG] $* ${plain}"
}

function LOGE() {
    echo -e "${red}[ERR] $* ${plain}"
}

function LOGI() {
    echo -e "${green}[INF] $* ${plain}"
}

is_valid_port() {
    [[ "${1:-}" =~ ^[0-9]+$ ]] && [[ "$1" -ge 1 && "$1" -le 65535 ]]
}

run_install_script() {
    local script_url="$1"
    shift

    if [[ -f "${XUI_LOCAL_INSTALL_SCRIPT}" ]]; then
        bash "${XUI_LOCAL_INSTALL_SCRIPT}" "$@"
        return $?
    fi

    bash <(curl -Ls "${script_url}") "$@"
}
# check root
[[ $EUID -ne 0 ]] && LOGE "错误:  必须使用root用户运行此脚本!\n" && exit 1

# check os
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
    LOGE "未检测到系统版本，请联系脚本作者！\n" && exit 1
fi

os_version=""

# os version
if [[ -f /etc/os-release ]]; then
    os_version=$(awk -F'[= ."]' '/VERSION_ID/{print $3}' /etc/os-release)
fi
if [[ -z "$os_version" && -f /etc/lsb-release ]]; then
    os_version=$(awk -F'[= ."]+' '/DISTRIB_RELEASE/{print $2}' /etc/lsb-release)
fi

if [[ x"${release}" == x"centos" ]]; then
    if [[ ${os_version} -le 6 ]]; then
        LOGE "请使用 CentOS 7 或更高版本的系统！\n" && exit 1
    fi
elif [[ x"${release}" == x"ubuntu" ]]; then
    if [[ ${os_version} -lt 16 ]]; then
        LOGE "请使用 Ubuntu 16 或更高版本的系统！\n" && exit 1
    fi
elif [[ x"${release}" == x"debian" ]]; then
    if [[ ${os_version} -lt 8 ]]; then
        LOGE "请使用 Debian 8 或更高版本的系统！\n" && exit 1
    fi
fi

confirm() {
    if [[ $# -gt 1 ]]; then
        echo && read -p "$1 [默认$2]: " temp
        if [[ x"${temp}" == x"" ]]; then
            temp=$2
        fi
    else
        read -p "$1 [y/n]: " temp
    fi
    if [[ x"${temp}" == x"y" || x"${temp}" == x"Y" ]]; then
        return 0
    else
        return 1
    fi
}

confirm_restart() {
    confirm "是否重启面板，重启面板也会重启 xray" "y"
    if [[ $? == 0 ]]; then
        restart
    else
        show_menu
    fi
}

before_show_menu() {
    echo && echo -n -e "${yellow}按回车返回主菜单: ${plain}" && read temp
    show_menu
}

install() {
    run_install_script "${XUI_RAW_BASE}/install.sh"
    if [[ $? == 0 ]]; then
        if [[ $# == 0 ]]; then
            start
        else
            start 0
        fi
    fi
}

update() {
    confirm "本功能会强制重装当前最新版，数据不会丢失，是否继续?" "n"
    if [[ $? != 0 ]]; then
        LOGE "已取消"
        if [[ $# == 0 ]]; then
            before_show_menu
        fi
        return 0
    fi
    run_install_script "${XUI_RAW_BASE}/install.sh"
    if [[ $? == 0 ]]; then
        LOGI "更新完成，已自动重启面板 "
        exit 0
    fi
}

uninstall() {
    confirm "确定要卸载面板吗,xray 也会卸载?" "n"
    if [[ $? != 0 ]]; then
        if [[ $# == 0 ]]; then
            show_menu
        fi
        return 0
    fi
    systemctl stop x-ui
    systemctl disable x-ui
    rm /etc/systemd/system/x-ui.service -f
    systemctl daemon-reload
    systemctl reset-failed
    rm /etc/x-ui/ -rf
    rm /usr/local/x-ui/ -rf

    echo ""
    echo -e "卸载成功，如果你想删除此脚本，则退出脚本后运行 ${green}rm /usr/bin/x-ui -f${plain} 进行删除"
    echo ""

    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

reset_user() {
    confirm "确定要将用户名和密码重置为 admin 吗" "n"
    if [[ $? != 0 ]]; then
        if [[ $# == 0 ]]; then
            show_menu
        fi
        return 0
    fi
    /usr/local/x-ui/x-ui setting -username admin -password admin
    echo -e "用户名和密码已重置为 ${green}admin${plain}，现在请重启面板"
    confirm_restart
}

reset_config() {
    confirm "确定要重置所有面板设置吗，账号数据不会丢失，用户名和密码不会改变" "n"
    if [[ $? != 0 ]]; then
        if [[ $# == 0 ]]; then
            show_menu
        fi
        return 0
    fi
    /usr/local/x-ui/x-ui setting -reset
    echo -e "所有面板设置已重置为默认值，现在请重启面板，并使用默认的 ${green}54321${plain} 端口访问面板"
    confirm_restart
}

check_config() {
    info=$(/usr/local/x-ui/x-ui setting -show true)
    if [[ $? != 0 ]]; then
        LOGE "get current settings error,please check logs"
        show_menu
    fi
    LOGI "${info}"
}

set_port() {
    echo && echo -n -e "输入端口号[1-65535]: " && read port
    if [[ -z "${port}" ]]; then
        LOGD "已取消"
        before_show_menu
    else
        /usr/local/x-ui/x-ui setting -port ${port}
        echo -e "设置端口完毕，现在请重启面板，并使用新设置的端口 ${green}${port}${plain} 访问面板"
        confirm_restart
    fi
}

start() {
    check_status
    if [[ $? == 0 ]]; then
        echo ""
        LOGI "面板已运行，无需再次启动，如需重启请选择重启"
    else
        systemctl start x-ui
        sleep 2
        check_status
        if [[ $? == 0 ]]; then
            LOGI "x-ui 启动成功"
        else
            LOGE "面板启动失败，可能是因为启动时间超过了两秒，请稍后查看日志信息"
        fi
    fi

    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

stop() {
    check_status
    if [[ $? == 1 ]]; then
        echo ""
        LOGI "面板已停止，无需再次停止"
    else
        systemctl stop x-ui
        sleep 2
        check_status
        if [[ $? == 1 ]]; then
            LOGI "x-ui 与 xray 停止成功"
        else
            LOGE "面板停止失败，可能是因为停止时间超过了两秒，请稍后查看日志信息"
        fi
    fi

    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

restart() {
    systemctl restart x-ui
    sleep 2
    check_status
    if [[ $? == 0 ]]; then
        LOGI "x-ui 与 xray 重启成功"
    else
        LOGE "面板重启失败，可能是因为启动时间超过了两秒，请稍后查看日志信息"
    fi
    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

status() {
    systemctl status x-ui -l
    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

enable() {
    systemctl enable x-ui
    if [[ $? == 0 ]]; then
        LOGI "x-ui 设置开机自启成功"
    else
        LOGE "x-ui 设置开机自启失败"
    fi

    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

disable() {
    systemctl disable x-ui
    if [[ $? == 0 ]]; then
        LOGI "x-ui 取消开机自启成功"
    else
        LOGE "x-ui 取消开机自启失败"
    fi

    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

show_log() {
    journalctl -u x-ui.service -e --no-pager -f
    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

migrate_v2_ui() {
    /usr/local/x-ui/x-ui v2-ui

    before_show_menu
}

install_bbr() {
    # temporary workaround for installing bb
    bash <(curl -L -s "${XUI_BBR_URL}")
    echo ""
    before_show_menu
}

update_shell() {
    if [[ -f "${XUI_LOCAL_SHELL_SCRIPT}" ]]; then
        cp -f "${XUI_LOCAL_SHELL_SCRIPT}" /usr/bin/x-ui
    else
        wget -O /usr/bin/x-ui -N --no-check-certificate "${XUI_RAW_BASE}/x-ui.sh"
    fi
    if [[ $? != 0 ]]; then
        echo ""
        LOGE "下载脚本失败，请检查本机能否连接 Github"
        before_show_menu
    else
        chmod +x /usr/bin/x-ui
        LOGI "升级脚本成功，请重新运行脚本" && exit 0
    fi
}

# 0: running, 1: not running, 2: not installed
check_status() {
    if [[ ! -f /etc/systemd/system/x-ui.service ]]; then
        return 2
    fi
    temp=$(systemctl status x-ui | grep Active | awk '{print $3}' | cut -d "(" -f2 | cut -d ")" -f1)
    if [[ x"${temp}" == x"running" ]]; then
        return 0
    else
        return 1
    fi
}

check_enabled() {
    temp=$(systemctl is-enabled x-ui)
    if [[ x"${temp}" == x"enabled" ]]; then
        return 0
    else
        return 1
    fi
}

check_uninstall() {
    check_status
    if [[ $? != 2 ]]; then
        echo ""
        LOGE "面板已安装，请不要重复安装"
        if [[ $# == 0 ]]; then
            before_show_menu
        fi
        return 1
    else
        return 0
    fi
}

check_install() {
    check_status
    if [[ $? == 2 ]]; then
        echo ""
        LOGE "请先安装面板"
        if [[ $# == 0 ]]; then
            before_show_menu
        fi
        return 1
    else
        return 0
    fi
}

show_status() {
    check_status
    case $? in
    0)
        echo -e "面板状态: ${green}已运行${plain}"
        show_enable_status
        ;;
    1)
        echo -e "面板状态: ${yellow}未运行${plain}"
        show_enable_status
        ;;
    2)
        echo -e "面板状态: ${red}未安装${plain}"
        ;;
    esac
    show_xray_status
}

show_enable_status() {
    check_enabled
    if [[ $? == 0 ]]; then
        echo -e "是否开机自启: ${green}是${plain}"
    else
        echo -e "是否开机自启: ${red}否${plain}"
    fi
}

check_xray_status() {
    count=$(ps -ef | grep "xray-linux" | grep -v "grep" | wc -l)
    if [[ ${count} -ne 0 ]]; then
        return 0
    else
        return 1
    fi
}

show_xray_status() {
    check_xray_status
    if [[ $? == 0 ]]; then
        echo -e "xray 状态: ${green}运行${plain}"
    else
        echo -e "xray 状态: ${red}未运行${plain}"
    fi
}

cert_safe_domain() {
    echo "$1" | sed 's/^\*\.//' | sed 's/[^A-Za-z0-9._-]/_/g' | sed 's/^[._-]*//;s/[._-]*$//'
}

cert_domain_dir() {
    local domain="$1"
    local safe_domain
    safe_domain=$(cert_safe_domain "${domain}")
    if [[ -z "${safe_domain}" ]]; then
        safe_domain="unknown"
    fi
    echo "${XUI_CERT_DIR}/${safe_domain}"
}

write_cert_meta() {
    local domain="$1"
    local provider="$2"
    local auto_renew="$3"
    local dir
    local expire=""
    dir=$(cert_domain_dir "${domain}")
    if command -v openssl &>/dev/null && [[ -f "${dir}/fullchain.pem" ]]; then
        expire=$(openssl x509 -enddate -noout -in "${dir}/fullchain.pem" 2>/dev/null | cut -d= -f2)
    fi
    cat > "${dir}/meta.json" <<EOF
{
  "domain": "${domain}",
  "provider": "${provider}",
  "source": "acme.sh",
  "certFile": "${dir}/fullchain.pem",
  "keyFile": "${dir}/privkey.pem",
  "created": $(date +%s),
  "expireText": "${expire}",
  "autoRenew": ${auto_renew}
}
EOF
}

list_certificates() {
    if [[ ! -d "${XUI_CERT_DIR}" ]]; then
        LOGI "证书目录不存在: ${XUI_CERT_DIR}"
        return 0
    fi
    LOGI "证书目录: ${XUI_CERT_DIR}"
    local found=0
    for dir in "${XUI_CERT_DIR}"/*; do
        [[ -d "${dir}" ]] || continue
        found=1
        local domain
        domain=$(basename "${dir}")
        if [[ -f "${dir}/meta.json" ]] && command -v jq &>/dev/null; then
            domain=$(jq -r '.domain // empty' "${dir}/meta.json")
        fi
        echo "----------------------------------------"
        LOGI "域名: ${domain}"
        LOGI "证书: ${dir}/fullchain.pem"
        LOGI "私钥: ${dir}/privkey.pem"
        if command -v openssl &>/dev/null && [[ -f "${dir}/fullchain.pem" ]]; then
            openssl x509 -enddate -noout -in "${dir}/fullchain.pem" 2>/dev/null
        fi
    done
    if [[ ${found} -eq 0 ]]; then
        LOGI "暂无已管理证书"
    fi
}

path_references_cert_dir() {
    local path="$1"
    local dir="$2"
    [[ -n "${path}" && "${path}" == "${dir}/"* ]]
}

panel_cert_reference_status() {
    local dir="$1"
    require_sqlite || return 2
    local cert_file key_file
    cert_file=$(get_panel_setting "webCertFile")
    key_file=$(get_panel_setting "webKeyFile")
    if path_references_cert_dir "${cert_file}" "${dir}" || path_references_cert_dir "${key_file}" "${dir}"; then
        LOGE "引用状态: 面板HTTPS正在使用该证书"
        LOGI "面板证书: ${cert_file:-未设置}"
        LOGI "面板私钥: ${key_file:-未设置}"
        return 0
    fi
    LOGI "引用状态: 面板HTTPS未引用"
    return 1
}

inbound_cert_references() {
    local dir="$1"
    require_sqlite || return 2
    if ! command -v jq &>/dev/null; then
        LOGE "未找到 jq，无法解析入站 stream_settings"
        return 2
    fi

    local rows refs jq_filter
    rows=$(sqlite3 -separator $'\t' "${XUI_DB_PATH}" "SELECT id, remark, port, protocol, stream_settings FROM inbounds;")
    jq_filter='[.tlsSettings.certificates[]?, .xtlsSettings.certificates[]?] | .[] | select(((.certificateFile // "") | startswith($dir)) or ((.keyFile // "") | startswith($dir))) | [(.certificateFile // ""), (.keyFile // "")] | @tsv'
    refs=""
    while IFS=$'\t' read -r inbound_id remark port protocol stream_settings; do
        [[ -n "${inbound_id}" ]] || continue
        local cert_refs
        cert_refs=$(echo "${stream_settings}" | jq -r --arg dir "${dir}/" "${jq_filter}" 2>/dev/null)
        if [[ $? -ne 0 ]]; then
            LOGE "解析入站 stream_settings 失败: id=${inbound_id}"
            return 2
        fi
        if [[ -n "${cert_refs}" ]]; then
            while IFS=$'\t' read -r cert_file key_file; do
                refs="${refs}
id=${inbound_id} remark=${remark} port=${port} protocol=${protocol} cert=${cert_file} key=${key_file}"
            done <<< "${cert_refs}"
        fi
    done <<< "${rows}"

    if [[ -n "${refs}" ]]; then
        LOGE "引用状态: 入站TLS正在使用该证书"
        echo "${refs}" | sed '/^$/d'
        return 0
    fi
    LOGI "引用状态: 入站TLS未引用"
    return 1
}

renew_certificate() {
    local domain=""
    read -p "请输入要续期的域名:" domain
    if [[ -z "${domain}" ]]; then
        LOGE "域名不能为空"
        return 1
    fi
    if ! command -v ~/.acme.sh/acme.sh &>/dev/null; then
        LOGE "未找到 acme.sh"
        return 1
    fi
    local dir
    dir=$(cert_domain_dir "${domain}")
    mkdir -p "${dir}"
    ~/.acme.sh/acme.sh --renew -d "${domain}" --force
    if [[ $? -ne 0 ]]; then
        LOGE "证书续期失败"
        return 1
    fi
    ~/.acme.sh/acme.sh --installcert -d "${domain}" --cert-file "${dir}/cert.pem" \
        --key-file "${dir}/privkey.pem" --fullchain-file "${dir}/fullchain.pem" \
        --ca-file "${dir}/ca.cer"
    if [[ $? -ne 0 ]]; then
        LOGE "证书安装失败"
        return 1
    fi
    write_cert_meta "${domain}" "acme.sh" "true"
    LOGI "证书续期完成: ${dir}"
}

delete_certificate() {
    local domain=""
    read -p "请输入要删除的域名:" domain
    if [[ -z "${domain}" ]]; then
        LOGE "域名不能为空"
        return 1
    fi
    local dir
    dir=$(cert_domain_dir "${domain}")
    if [[ ! -d "${dir}" ]]; then
        LOGE "证书不存在: ${dir}"
        return 1
    fi

    echo "----------------------------------------"
    LOGI "待删除域名: ${domain}"
    LOGI "托管目录: ${dir}"
    local blocked=0
    panel_cert_reference_status "${dir}"
    case $? in
    0)
        blocked=1
        ;;
    2)
        return 1
        ;;
    esac
    inbound_cert_references "${dir}"
    case $? in
    0)
        blocked=1
        ;;
    2)
        return 1
        ;;
    esac
    if [[ ${blocked} -ne 0 ]]; then
        LOGE "该证书正在被使用，禁止删除"
        LOGI "如需删除，请先关闭面板HTTPS、切换其它证书，或删除/修改引用该证书的入站"
        return 1
    fi

    confirm "确认删除 ${dir} 并同步清理 acme.sh 记录吗" "n"
    if [[ $? -ne 0 ]]; then
        return 0
    fi
    remove_acme_cert_record "${domain}"
    if [[ $? -ne 0 ]]; then
        LOGE "证书删除失败: acme.sh 记录清理失败，n3-ui托管目录未删除"
        return 1
    fi
    rm -rf "${dir}"
    if [[ -d "${dir}" ]]; then
        LOGE "n3-ui托管目录删除失败: ${dir}"
        return 1
    fi
    LOGI "n3-ui托管目录删除成功: ${dir}"
    LOGI "证书删除完成: ${domain}"
}

sql_escape() {
    echo "$1" | sed "s/'/''/g"
}

require_sqlite() {
    if ! command -v sqlite3 &>/dev/null; then
        LOGE "未找到 sqlite3，无法读取或写入面板设置"
        return 1
    fi
    if [[ ! -f "${XUI_DB_PATH}" ]]; then
        LOGE "未找到面板数据库: ${XUI_DB_PATH}"
        return 1
    fi
    return 0
}

get_panel_setting() {
    local key="$1"
    sqlite3 "${XUI_DB_PATH}" "SELECT value FROM settings WHERE key='${key}' ORDER BY id DESC LIMIT 1;"
}

acme_sh_path() {
    if [[ -f "/root/.acme.sh/acme.sh" ]]; then
        echo "/root/.acme.sh/acme.sh"
        return 0
    fi
    if [[ -f "${HOME}/.acme.sh/acme.sh" ]]; then
        echo "${HOME}/.acme.sh/acme.sh"
        return 0
    fi
    return 1
}

acme_record_dir_exists() {
    local domain="$1"
    [[ -d "/root/.acme.sh/${domain}" || -d "/root/.acme.sh/${domain}_ecc" || -d "${HOME}/.acme.sh/${domain}" || -d "${HOME}/.acme.sh/${domain}_ecc" ]]
}

acme_exact_record_exists() {
    local domain="$1"
    local acme
    acme=$(acme_sh_path) || return 1
    "${acme}" --list 2>/dev/null | awk -v domain="${domain}" 'NR > 1 && $1 == domain { found = 1 } END { exit found ? 0 : 1 }'
}

acme_record_exists() {
    local domain="$1"
    acme_record_dir_exists "${domain}" || acme_exact_record_exists "${domain}"
}

acme_active_record_exists() {
    local domain="$1"
    acme_exact_record_exists "${domain}"
}

remove_acme_cert_record() {
    local domain="$1"
    local acme
    acme=$(acme_sh_path) || {
        if acme_record_dir_exists "${domain}"; then
            LOGE "acme记录清理失败: 未找到 acme.sh"
            return 1
        fi
        LOGI "acme记录清理: 未找到 acme.sh，且未发现记录"
        return 0
    }

    if ! acme_active_record_exists "${domain}"; then
        if acme_record_dir_exists "${domain}"; then
            LOGI "acme记录清理: 未发现有效列表记录，仅存在 acme.sh 残留目录"
        else
            LOGI "acme记录清理: 未发现 ${domain} 的 acme.sh 记录"
        fi
        return 0
    fi

    local removed=1
    "${acme}" --remove -d "${domain}"
    if [[ $? -eq 0 ]]; then
        removed=0
    fi
    if [[ -d "/root/.acme.sh/${domain}_ecc" || -d "${HOME}/.acme.sh/${domain}_ecc" ]]; then
        "${acme}" --remove -d "${domain}" --ecc
        if [[ $? -eq 0 ]]; then
            removed=0
        fi
    fi

    if [[ ${removed} -eq 0 ]]; then
        LOGI "acme记录清理成功: ${domain}"
        return 0
    fi
    LOGE "acme记录清理失败: ${domain}"
    return 1
}

prepare_acme_domain_for_issue() {
    local domain="$1"
    local cert_path
    ACME_REISSUE_FORCE=0
    cert_path=$(cert_domain_dir "${domain}")
    if [[ -d "${cert_path}" && ( -f "${cert_path}/fullchain.pem" || -f "${cert_path}/privkey.pem" ) ]]; then
        LOGE "域名合法性校验失败,n3-ui 已存在对应域名证书: ${cert_path}"
        return 1
    fi
    if acme_active_record_exists "${domain}"; then
        LOGI "发现同域名旧 acme.sh 记录，先执行 acme.sh --remove 清理后重新申请"
        remove_acme_cert_record "${domain}" || return 1
        ACME_REISSUE_FORCE=1
    elif acme_record_dir_exists "${domain}"; then
        LOGI "发现同域名 acme.sh 残留目录，重新申请将使用 --force 覆盖旧 key 文件"
        ACME_REISSUE_FORCE=1
    fi
    LOGI "域名合法性校验通过..."
    return 0
}

cert_key_match() {
    local cert_file="$1"
    local key_file="$2"
    if ! command -v openssl &>/dev/null; then
        LOGE "未找到 openssl，无法验证证书和私钥是否匹配"
        return 1
    fi
    local cert_pub key_pub
    cert_pub=$(mktemp)
    key_pub=$(mktemp)
    openssl x509 -noout -pubkey -in "${cert_file}" > "${cert_pub}" 2>/dev/null
    local cert_ok=$?
    openssl pkey -in "${key_file}" -pubout > "${key_pub}" 2>/dev/null
    local key_ok=$?
    if [[ ${cert_ok} -ne 0 || ${key_ok} -ne 0 ]]; then
        rm -f "${cert_pub}" "${key_pub}"
        return 1
    fi
    cmp -s "${cert_pub}" "${key_pub}"
    local match=$?
    rm -f "${cert_pub}" "${key_pub}"
    return ${match}
}

show_panel_https_status() {
    require_sqlite || return 1

    local cert_file key_file
    cert_file=$(get_panel_setting "webCertFile")
    key_file=$(get_panel_setting "webKeyFile")

    echo "----------------------------------------"
    LOGI "面板 HTTPS 状态"
    if [[ -z "${cert_file}" && -z "${key_file}" ]]; then
        LOGI "是否启用HTTPS: 否"
        return 0
    fi
    if [[ -n "${cert_file}" && -n "${key_file}" ]]; then
        LOGI "是否启用HTTPS: 是"
    else
        LOGE "是否启用HTTPS: 配置不完整"
    fi

    LOGI "证书路径: ${cert_file:-未设置}"
    if [[ -n "${cert_file}" && -f "${cert_file}" ]]; then
        LOGI "证书文件: 存在"
    else
        LOGE "证书文件: 不存在"
    fi

    LOGI "私钥路径: ${key_file:-未设置}"
    if [[ -n "${key_file}" && -f "${key_file}" ]]; then
        LOGI "私钥文件: 存在"
    else
        LOGE "私钥文件: 不存在"
    fi

    if [[ -n "${cert_file}" && -f "${cert_file}" ]] && command -v openssl &>/dev/null; then
        echo "----------------------------------------"
        LOGI "openssl 证书信息:"
        openssl x509 -noout -subject -issuer -dates -in "${cert_file}" 2>/dev/null
        local expire
        expire=$(openssl x509 -enddate -noout -in "${cert_file}" 2>/dev/null | cut -d= -f2)
        if [[ -n "${expire}" ]]; then
            LOGI "过期时间: ${expire}"
        fi
    elif ! command -v openssl &>/dev/null; then
        LOGE "未找到 openssl，无法显示证书详情"
    fi

    if [[ -n "${cert_file}" && -f "${cert_file}" && -n "${key_file}" && -f "${key_file}" ]]; then
        if cert_key_match "${cert_file}" "${key_file}"; then
            LOGI "证书和私钥: 匹配"
        else
            LOGE "证书和私钥: 不匹配或无法解析"
        fi
    fi
}

disable_panel_https() {
    require_sqlite || return 1

    confirm "确认关闭面板 HTTPS 吗？不会删除任何证书文件" "n"
    if [[ $? -ne 0 ]]; then
        return 0
    fi

    sqlite3 "${XUI_DB_PATH}" "DELETE FROM settings WHERE key IN ('webCertFile','webKeyFile'); INSERT INTO settings (key,value) VALUES ('webCertFile',''); INSERT INTO settings (key,value) VALUES ('webKeyFile','');"
    if [[ $? -ne 0 ]]; then
        LOGE "关闭面板 HTTPS 失败"
        return 1
    fi
    LOGI "面板 HTTPS 已关闭，证书文件未删除"
    LOGI "请执行 systemctl restart x-ui 后使用 HTTP 访问面板"
}

save_panel_cert_setting() {
    local cert_file="$1"
    local key_file="$2"
    if [[ ! -f "${cert_file}" || ! -f "${key_file}" ]]; then
        LOGE "证书或私钥文件不存在"
        return 1
    fi
    if ! cert_key_match "${cert_file}" "${key_file}"; then
        LOGE "证书和私钥不匹配或无法解析"
        return 1
    fi
    require_sqlite || return 1
    local cert_sql key_sql
    cert_sql=$(sql_escape "${cert_file}")
    key_sql=$(sql_escape "${key_file}")
    sqlite3 "${XUI_DB_PATH}" "DELETE FROM settings WHERE key IN ('webCertFile','webKeyFile'); INSERT INTO settings (key,value) VALUES ('webCertFile','${cert_sql}'); INSERT INTO settings (key,value) VALUES ('webKeyFile','${key_sql}');"
    if [[ $? -ne 0 ]]; then
        LOGE "面板 HTTPS 证书设置失败"
        return 1
    fi
    LOGI "面板 HTTPS 证书已设置，重启面板后生效"
    confirm_restart
}

set_panel_https_certificate() {
    list_certificates
    local domain=""
    read -p "请输入要设置为面板 HTTPS 的域名:" domain
    if [[ -z "${domain}" ]]; then
        LOGE "域名不能为空"
        return 1
    fi
    local dir
    dir=$(cert_domain_dir "${domain}")
    save_panel_cert_setting "${dir}/fullchain.pem" "${dir}/privkey.pem"
}

repair_panel_https_certificate() {
    if [[ ! -d "${XUI_CERT_DIR}" ]]; then
        LOGE "证书目录不存在: ${XUI_CERT_DIR}"
        return 1
    fi

    LOGI "可用证书:"
    local found=0
    for dir in "${XUI_CERT_DIR}"/*; do
        [[ -d "${dir}" ]] || continue
        local cert_file="${dir}/fullchain.pem"
        local key_file="${dir}/privkey.pem"
        [[ -f "${cert_file}" && -f "${key_file}" ]] || continue
        found=1
        local domain
        domain=$(basename "${dir}")
        if [[ -f "${dir}/meta.json" ]] && command -v jq &>/dev/null; then
            local meta_domain
            meta_domain=$(jq -r '.domain // empty' "${dir}/meta.json")
            if [[ -n "${meta_domain}" ]]; then
                domain="${meta_domain}"
            fi
        fi
        echo "----------------------------------------"
        LOGI "域名: ${domain}"
        LOGI "证书: ${cert_file}"
        LOGI "私钥: ${key_file}"
        if command -v openssl &>/dev/null; then
            openssl x509 -enddate -noout -in "${cert_file}" 2>/dev/null
        fi
    done
    if [[ ${found} -eq 0 ]]; then
        LOGE "未找到可用证书"
        return 1
    fi

    local domain=""
    read -p "请输入要修复/设置为面板 HTTPS 的域名:" domain
    if [[ -z "${domain}" ]]; then
        LOGE "域名不能为空"
        return 1
    fi
    local dir cert_file key_file
    dir=$(cert_domain_dir "${domain}")
    cert_file="${dir}/fullchain.pem"
    key_file="${dir}/privkey.pem"
    if [[ ! -f "${cert_file}" || ! -f "${key_file}" ]]; then
        LOGE "证书或私钥文件不存在: ${dir}"
        return 1
    fi
    if ! cert_key_match "${cert_file}" "${key_file}"; then
        LOGE "证书和私钥不匹配或无法解析"
        return 1
    fi
    LOGI "证书和私钥验证通过"
    save_panel_cert_setting "${cert_file}" "${key_file}"
}

cert_manage() {
    echo -e "
  ${green}证书管理${plain}
  ${green}0.${plain} 返回主菜单
  ${green}1.${plain} 申请证书
  ${green}2.${plain} 查看证书
  ${green}3.${plain} 续期证书
  ${green}4.${plain} 删除证书
  ${green}5.${plain} 设置面板HTTPS证书
  ${green}6.${plain} 查看当前面板HTTPS状态
  ${green}7.${plain} 关闭面板HTTPS
  ${green}8.${plain} 修复/重新设置面板HTTPS
 "
    echo && read -p "请输入选择 [0-8]: " num
    case "${num}" in
    0)
        show_menu
        ;;
    1)
        ssl_cert_issue
        ;;
    2)
        list_certificates
        ;;
    3)
        renew_certificate
        ;;
    4)
        delete_certificate
        ;;
    5)
        set_panel_https_certificate
        ;;
    6)
        show_panel_https_status
        ;;
    7)
        disable_panel_https
        ;;
    8)
        repair_panel_https_certificate
        ;;
    *)
        LOGE "请输入正确的数字 [0-8]"
        ;;
    esac
}

#this will be an entrance for ssl cert issue
#here we can provide two different methods to issue cert
#first.standalone mode second.DNS API mode
ssl_cert_issue() {
    local method=""
    echo -E ""
    LOGD "******使用说明******"
    LOGI "该脚本提供两种方式实现证书签发,证书安装路径为${XUI_CERT_DIR}/域名"
    LOGI "方式1:acme standalone mode,需要保持端口开放"
    LOGI "方式2:acme DNS API mode,需要提供Cloudflare Global API Key"
    LOGI "如域名属于免费域名,则推荐使用方式1进行申请"
    LOGI "如域名非免费域名且使用Cloudflare进行解析使用方式2进行申请"
    read -p "请选择你想使用的方式,输入数字1或者2后回车": method
    LOGI "你所使用的方式为${method}"

    if [ "${method}" == "1" ]; then
        ssl_cert_issue_standalone
    elif [ "${method}" == "2" ]; then
        ssl_cert_issue_by_cloudflare
    else
        LOGE "输入无效,请检查你的输入,脚本将退出..."
        exit 1
    fi
}

install_acme() {
    cd ~
    LOGI "开始安装acme脚本..."
    curl -Ls "${XUI_ACME_INSTALL_URL}" | sh
    if [ $? -ne 0 ]; then
        LOGE "acme安装失败"
        return 1
    else
        LOGI "acme安装成功"
    fi
    return 0
}

#method for standalone mode
ssl_cert_issue_standalone() {
    #check for acme.sh first
    if ! command -v ~/.acme.sh/acme.sh &>/dev/null; then
        install_acme
        if [ $? -ne 0 ]; then
            LOGE "安装 acme 失败，请检查日志"
            exit 1
        fi
    fi
    #install socat second
    if ! command -v socat &>/dev/null; then
        if [[ x"${release}" == x"centos" ]]; then
            yum install socat -y
        else
            apt install socat -y
        fi
        if [ $? -ne 0 ]; then
            LOGE "无法安装socat,请检查错误日志"
            exit 1
        else
            LOGI "socat安装成功..."
        fi
    else
        LOGI "socat已安装..."
    fi
    #get the domain here,and we need verify it
    local domain=""
    read -p "请输入你的域名:" domain
    LOGD "你输入的域名为:${domain},正在进行域名合法性校验..."
    prepare_acme_domain_for_issue "${domain}"
    if [[ $? -ne 0 ]]; then
        exit 1
    fi
    #creat a directory for install cert
    certPath=$(cert_domain_dir "${domain}")
    if [ ! -d "$certPath" ]; then
        mkdir -p "$certPath"
    fi
    #get needed port here
    local WebPort=80
    local input_port=""
    while true; do
        read -p "请输入HTTP验证端口,直接回车使用默认80端口:" input_port
        if [[ -z "${input_port}" ]]; then
            WebPort=80
            LOGI "未输入端口，使用默认80"
            break
        fi
        if is_valid_port "${input_port}"; then
            WebPort="${input_port}"
            break
        fi
        LOGE "端口无效，请输入1-65535之间的数字"
    done
    LOGI "将会使用${WebPort}进行证书申请,请确保端口处于开放状态..."
    #NOTE:This should be handled by use
    #open the port and kill the occupied progress
    ~/.acme.sh/acme.sh --set-default-ca --server letsencrypt
    local issue_args=(--issue -d "${domain}" --standalone --httpport "${WebPort}")
    if [[ "${ACME_REISSUE_FORCE}" == "1" ]]; then
        issue_args+=(--force)
    fi
    ~/.acme.sh/acme.sh "${issue_args[@]}"
    if [ $? -ne 0 ]; then
        LOGE "证书申请失败,原因请参见报错信息"
        remove_acme_cert_record "${domain}"
        exit 1
    else
        LOGI "证书申请成功,开始安装证书..."
    fi
    #install cert
    ~/.acme.sh/acme.sh --installcert -d ${domain} --ca-file "${certPath}/ca.cer" \
        --cert-file "${certPath}/cert.pem" --key-file "${certPath}/privkey.pem" \
        --fullchain-file "${certPath}/fullchain.pem"

    if [ $? -ne 0 ]; then
        LOGE "证书安装失败,脚本退出"
        remove_acme_cert_record "${domain}"
        exit 1
    else
        LOGI "证书安装成功,开启自动更新..."
    fi
    write_cert_meta "${domain}" "standalone" "true"
    ~/.acme.sh/acme.sh --upgrade --auto-upgrade
    if [ $? -ne 0 ]; then
        LOGE "自动更新设置失败,脚本退出"
        ls -lah "${certPath}"
        chmod 755 $certPath
        exit 1
    else
        LOGI "证书已安装且已开启自动更新,具体信息如下"
        ls -lah "${certPath}"
        chmod 755 $certPath
    fi

}

#method for DNS API mode
ssl_cert_issue_by_cloudflare() {
    echo -E ""
    LOGD "******使用说明******"
    LOGI "该脚本将使用Acme脚本申请证书,使用时需保证:"
    LOGI "1.知晓Cloudflare 注册邮箱"
    LOGI "2.知晓Cloudflare Global API Key"
    LOGI "3.域名已通过Cloudflare进行解析到当前服务器"
    LOGI "4.该脚本申请证书默认安装路径为${XUI_CERT_DIR}/域名"
    confirm "我已确认以上内容[y/n]" "y"
    if [ $? -eq 0 ]; then
        install_acme
        if [ $? -ne 0 ]; then
            LOGE "无法安装acme,请检查错误日志"
            exit 1
        fi
        CF_Domain=""
        CF_GlobalKey=""
        CF_AccountEmail=""
        LOGD "请设置域名:"
        read -p "Input your domain here:" CF_Domain
        LOGD "你的域名设置为:${CF_Domain},正在进行域名合法性校验..."
        prepare_acme_domain_for_issue "${CF_Domain}"
        if [[ $? -ne 0 ]]; then
            exit 1
        fi
        certPath=$(cert_domain_dir "${CF_Domain}")
        if [ ! -d "$certPath" ]; then
            mkdir -p "$certPath"
        fi
        LOGD "请设置API密钥:"
        read -p "Input your key here:" CF_GlobalKey
        LOGD "你的API密钥为:${CF_GlobalKey}"
        LOGD "请设置注册邮箱:"
        read -p "Input your email here:" CF_AccountEmail
        LOGD "你的注册邮箱为:${CF_AccountEmail}"
        ~/.acme.sh/acme.sh --set-default-ca --server letsencrypt
        if [ $? -ne 0 ]; then
            LOGE "修改默认CA为Lets'Encrypt失败,脚本退出"
            exit 1
        fi
        export CF_Key="${CF_GlobalKey}"
        export CF_Email=${CF_AccountEmail}
        local issue_args=(--issue --dns dns_cf -d "${CF_Domain}" -d "*.${CF_Domain}" --log)
        if [[ "${ACME_REISSUE_FORCE}" == "1" ]]; then
            issue_args+=(--force)
        fi
        ~/.acme.sh/acme.sh "${issue_args[@]}"
        if [ $? -ne 0 ]; then
            LOGE "证书签发失败,脚本退出"
            remove_acme_cert_record "${CF_Domain}"
            exit 1
        else
            LOGI "证书签发成功,安装中..."
        fi
        ~/.acme.sh/acme.sh --installcert -d ${CF_Domain} -d *.${CF_Domain} --ca-file "${certPath}/ca.cer" \
            --cert-file "${certPath}/cert.pem" --key-file "${certPath}/privkey.pem" \
            --fullchain-file "${certPath}/fullchain.pem"
        if [ $? -ne 0 ]; then
            LOGE "证书安装失败,脚本退出"
            remove_acme_cert_record "${CF_Domain}"
            exit 1
        else
            LOGI "证书安装成功,开启自动更新..."
        fi
        write_cert_meta "${CF_Domain}" "cloudflare" "true"
        ~/.acme.sh/acme.sh --upgrade --auto-upgrade
        if [ $? -ne 0 ]; then
            LOGE "自动更新设置失败,脚本退出"
            ls -lah "${certPath}"
            chmod 755 $certPath
            exit 1
        else
            LOGI "证书已安装且已开启自动更新,具体信息如下"
            ls -lah "${certPath}"
            chmod 755 $certPath
        fi
    else
        show_menu
    fi
}

#add for cron jobs,including sync geo data,check logs and restart x-ui
cron_jobs() {
    clea
    echo -e "
  ${green}定时任务管理${plain}
  ${green}0.${plain}  返回主菜单
  ${green}1.${plain}  开启定时更新geo
  ${green}2.${plain}  关闭定时更新geo
  ${green}3.${plain}  开启定时删除xray日志
  ${green}4.${plain}  关闭定时删除xray日志
  "
    echo && read -p "请输入选择 [0-4]: " num
    case "${num}" in
    0)
        show_menu
        ;;
    1)
        enable_auto_update_geo
        ;;
    2)
        disable_auto_update_geo
        ;;
    3)
        enable_auto_clear_log
        ;;
    4)
        disable_auto_clear_log
        ;;
    *)
        LOGE "请输入正确的数字 [0-4]"
        ;;
    esac
}

#update geo data
update_geo() {
    #back up first
    mv ${PATH_FOR_GEO_IP} ${PATH_FOR_GEO_IP}.bak
    #update data
    curl -s -L -o ${PATH_FOR_GEO_IP} ${URL_FOR_GEO_IP}
    if [[ $? -ne 0 ]]; then
        echo "update geoip.dat failed"
        mv ${PATH_FOR_GEO_IP}.bak ${PATH_FOR_GEO_IP}
    else
        echo "update geoip.dat succeed"
        rm -f ${PATH_FOR_GEO_IP}.bak
    fi
    mv ${PATH_FOR_GEO_SITE} ${PATH_FOR_GEO_SITE}.bak
    curl -s -L -o ${PATH_FOR_GEO_SITE} ${URL_FOR_GEO_SITE}
    if [[ $? -ne 0 ]]; then
        echo "update geosite.dat failed"
        mv ${PATH_FOR_GEO_SITE}.bak ${PATH_FOR_GEO_SITE}
    else
        echo "update geosite.dat succeed"
        rm -f ${PATH_FOR_GEO_SITE}.bak
    fi
    #restart x-ui
    systemctl restart x-ui
}

enable_auto_update_geo() {
    LOGI "正在开启自动更新geo数据..."
    local tmp="/tmp/crontabTask.$$.tmp"
    crontab -l 2>/dev/null > "${tmp}"
    echo "00 4 */2 * * x-ui geo > /dev/null" >> "${tmp}"
    crontab "${tmp}"
    rm -f "${tmp}"
    LOGI "开启自动更新geo数据成功"
}

disable_auto_update_geo() {
    crontab -l | grep -v "x-ui geo" | crontab -
    if [[ $? -ne 0 ]]; then
        LOGI "取消x-ui 自动更新geo数据失败"
    else
        LOGI "取消x-ui 自动更新geo数据成功"
    fi
}

#clear xray log,need enable log in config template
#here we need input an absolute path for log
clear_log() {
    LOGI "清除xray日志中..."
    local filePath=''
    if [[ $# -gt 0 ]]; then
        filePath=$1
    else
        LOGE "未输入有效文件路径,脚本退出"
        exit 1
    fi
    LOGI "日志路径为:${filePath}"
    if [[ ! -f ${filePath} ]]; then
        LOGE "清除xray日志文件失败,${filePath}不存在,请确认"
        exit 1
    fi
    fileSize=$(ls -la ${filePath} --block-size=M | awk '{print $5}' | awk -F 'M' '{print$1}')
    if [[ ${fileSize} -gt ${DEFAULT_LOG_FILE_DELETE_TRIGGER} ]]; then
        rm $1
        if [[ $? -ne 0 ]]; then
            LOGE "清除xray日志文件:${filePath}失败"
        else
            LOGI "清除xray日志文件:${filePath}成功"
            systemctl restart x-ui
        fi
    else
        LOGI "当前日志大小为${fileSize}M,小于${DEFAULT_LOG_FILE_DELETE_TRIGGER}M,将不会清除"
    fi
}

#enable auto delete log，need file path as
enable_auto_clear_log() {
    LOGI "设置定时清除xray日志..."
    local accessfilePath=''
    local errorfilePath=''
    accessfilePath=$(cat ${PATH_FOR_CONFIG} | jq .log.access | tr -d '"')
    errorfilePath=$(cat ${PATH_FOR_CONFIG} | jq .log.error | tr -d '"')
    if [[ -z "${accessfilePath}" || -z "${errorfilePath}" ]]; then
        LOGI "配置文件中的日志文件路径无效,脚本退出"
        exit 1
    fi
    if [[ -f ${accessfilePath} ]]; then
        local tmp="/tmp/crontabTask.$$.tmp"
        crontab -l 2>/dev/null > "${tmp}"
        echo "30 4 */2 * * x-ui clear ${accessfilePath} > /dev/null" >> "${tmp}"
        crontab "${tmp}"
        rm -f "${tmp}"
        LOGI "设置定时清除xray日志:${accessfilePath}成功"
    else
        LOGE "accesslog不存在,将不会为其设置定时清除"
    fi

    if [[ -f ${errorfilePath} ]]; then
        local tmp="/tmp/crontabTask.$$.tmp"
        crontab -l 2>/dev/null > "${tmp}"
        echo "30 4 */2 * * x-ui clear ${errorfilePath} > /dev/null" >> "${tmp}"
        crontab "${tmp}"
        rm -f "${tmp}"
        LOGI "设置定时清除xray日志:${errorfilePath}成功"
    else
        LOGE "errorlog不存在,将不会为其设置定时清除"
    fi
}

#disable auto dlete log
disable_auto_clear_log() {
    crontab -l | grep -v "x-ui clear" | crontab -
    if [[ $? -ne 0 ]]; then
        LOGI "取消 定时清除xray日志失败"
    else
        LOGI "取消 定时清除xray日志成功"
    fi
}

show_usage() {
    echo "x-ui 管理脚本使用方法: "
    echo "------------------------------------------"
    echo "x-ui              - 显示管理菜单 (功能更多)"
    echo "x-ui start        - 启动 x-ui 面板"
    echo "x-ui stop         - 停止 x-ui 面板"
    echo "x-ui restart      - 重启 x-ui 面板"
    echo "x-ui status       - 查看 x-ui 状态"
    echo "x-ui enable       - 设置 x-ui 开机自启"
    echo "x-ui disable      - 取消 x-ui 开机自启"
    echo "x-ui log          - 查看 x-ui 日志"
    echo "x-ui v2-ui        - 迁移本机器的 v2-ui 账号数据至 x-ui"
    echo "x-ui update       - 更新 x-ui 面板"
    echo "x-ui install      - 安装 x-ui 面板"
    echo "x-ui uninstall    - 卸载 x-ui 面板"
    echo "x-ui clear        - 清除 x-ui 日志"
    echo "x-ui geo          - 更新 x-ui geo数据"
    echo "x-ui cron         - 配置 x-ui 定时任务"
    echo "------------------------------------------"
}

show_menu() {
    echo -e "
  ${green}x-ui 面板管理脚本${plain}
  ${green}0.${plain} 退出脚本
————————————————
  ${green}1.${plain} 安装 x-ui
  ${green}2.${plain} 更新 x-ui
  ${green}3.${plain} 卸载 x-ui
————————————————
  ${green}4.${plain} 重置用户名密码
  ${green}5.${plain} 重置面板设置
  ${green}6.${plain} 设置面板端口
  ${green}7.${plain} 查看当前面板信息
————————————————
  ${green}8.${plain} 启动 x-ui
  ${green}9.${plain} 停止 x-ui
  ${green}10.${plain} 重启 x-ui
  ${green}11.${plain} 查看 x-ui 状态
  ${green}12.${plain} 查看 x-ui 日志
————————————————
  ${green}13.${plain} 设置 x-ui 开机自启
  ${green}14.${plain} 取消 x-ui 开机自启
————————————————
  ${green}15.${plain} 一键安装 bbr (最新内核)
  ${green}16.${plain} 证书管理
  ${green}17.${plain} 配置x-ui定时任务
 "
    show_status
    echo && read -p "请输入选择 [0-17],查看面板登录信息请输入数字7:" num

    case "${num}" in
    0)
        exit 0
        ;;
    1)
        check_uninstall && install
        ;;
    2)
        check_install && update
        ;;
    3)
        check_install && uninstall
        ;;
    4)
        check_install && reset_user
        ;;
    5)
        check_install && reset_config
        ;;
    6)
        check_install && set_port
        ;;
    7)
        check_install && check_config
        ;;
    8)
        check_install && start
        ;;
    9)
        check_install && stop
        ;;
    10)
        check_install && restart
        ;;
    11)
        check_install && status
        ;;
    12)
        check_install && show_log
        ;;
    13)
        check_install && enable
        ;;
    14)
        check_install && disable
        ;;
    15)
        install_bbr
        ;;
    16)
        cert_manage
        ;;
    17)
        check_install && cron_jobs
        ;;
    *)
        LOGE "请输入正确的数字 [0-17],查看面板登录信息请输入数字7"
        ;;
    esac
}

if [[ $# > 0 ]]; then
    case $1 in
    "start")
        check_install 0 && start 0
        ;;
    "stop")
        check_install 0 && stop 0
        ;;
    "restart")
        check_install 0 && restart 0
        ;;
    "status")
        check_install 0 && status 0
        ;;
    "enable")
        check_install 0 && enable 0
        ;;
    "disable")
        check_install 0 && disable 0
        ;;
    "log")
        check_install 0 && show_log 0
        ;;
    "v2-ui")
        check_install 0 && migrate_v2_ui 0
        ;;
    "update")
        check_install 0 && update 0
        ;;
    "install")
        check_uninstall 0 && install 0
        ;;
    "uninstall")
        check_install 0 && uninstall 0
        ;;
    "geo")
        check_install 0 && update_geo
        ;;
    "clear")
        check_install 0 && clear_log $2
        ;;
    "cron")
        check_install && cron_jobs
        ;;
    *) show_usage ;;
    esac
else
    show_menu
fi
