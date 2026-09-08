#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd -P "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
output_dir="${1:-${repo_root}/dist}"
go_binary="${GO_BINARY:-go}"
expected_go_version="${EXPECTED_GO_VERSION:-go1.22.7}"
go_binary_path="$(command -v "${go_binary}")"
go_root="$(cd -P "$(dirname "${go_binary_path}")/.." && pwd)"
commit="$(git -C "${repo_root}" rev-parse HEAD)"
version="$(git -C "${repo_root}" show "${commit}:config/version" | tr -d '[:space:]')"
artifact="n3-ui-v${version}-linux-amd64.tar.gz"
build_root="$(mktemp -d /tmp/n3-ui-prebuilt-build.XXXXXX)"
source_dir="${build_root}/source"
package_dir="${build_root}/package/x-ui"

cleanup() {
    rm -rf "${build_root}"
}
trap cleanup EXIT

if [[ "$(uname -s)" != "Linux" || "$(uname -m)" != "x86_64" ]]; then
    echo "ERROR: this artifact must be built on Linux amd64." >&2
    exit 1
fi
if [[ ! -r /etc/os-release ]] || ! grep -Eq '^ID=debian$' /etc/os-release \
    || ! grep -Eq '^VERSION_ID="?11"?$' /etc/os-release; then
    echo "ERROR: this artifact must be built on Debian 11." >&2
    exit 1
fi
if [[ "$(${go_binary} version | awk '{print $3}')" != "${expected_go_version}" ]]; then
    echo "ERROR: expected ${expected_go_version}, got $(${go_binary} version)." >&2
    exit 1
fi

mkdir -p "${source_dir}" "${package_dir}/bin" "${package_dir}/config" "${output_dir}"
git -C "${repo_root}" archive "${commit}" | tar -x -C "${source_dir}"

(
    cd "${source_dir}"
    GOROOT="${go_root}" CGO_ENABLED=1 GOOS=linux GOARCH=amd64 GO111MODULE=on \
        "${go_binary_path}" build -o "${package_dir}/x-ui" .
)

install -m 0644 "${source_dir}/x-ui.service" "${package_dir}/x-ui.service"
install -m 0755 "${source_dir}/x-ui.sh" "${package_dir}/x-ui.sh"
install -m 0755 "${source_dir}/install.sh" "${package_dir}/install.sh"
install -m 0644 "${source_dir}/config/version" "${package_dir}/config/version"
install -m 0644 "${source_dir}/bin/config.json" "${package_dir}/bin/config.json"

cat >"${package_dir}/BUILD-INFO" <<EOF
version=${version}
commit=${commit}
goos=linux
goarch=amd64
go_version=$(GOROOT="${go_root}" "${go_binary_path}" version | awk '{print $3}')
cgo_enabled=1
build_os=$(awk -F= '/^PRETTY_NAME=/{gsub(/"/, "", $2); print $2}' /etc/os-release)
build_arch=$(dpkg --print-architecture)
glibc=$(ldd --version 2>&1 | head -n 1)
artifact=${artifact}
EOF

tar --sort=name --owner=0 --group=0 --numeric-owner \
    -czf "${output_dir}/${artifact}" -C "${build_root}/package" x-ui
(
    cd "${output_dir}"
    sha256sum "${artifact}" >"${artifact}.sha256"
)

echo "artifact=${output_dir}/${artifact}"
echo "checksum=${output_dir}/${artifact}.sha256"
echo "sha256=$(sha256sum "${output_dir}/${artifact}" | awk '{print $1}')"
cat "${package_dir}/BUILD-INFO"
