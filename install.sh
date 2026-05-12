#!/usr/bin/env bash

set -euo pipefail

REPOSITORY="${AGGR_GITHUB_REPO:-silverling/aggr}"
SERVICE_TEMPLATE_INSTALL_DIR="/opt/aggr"
INSTALL_DIR="${INSTALL_DIR:-${SERVICE_TEMPLATE_INSTALL_DIR}}"
SERVICE_NAME="aggr"
SERVICE_PATH="/etc/systemd/system/${SERVICE_NAME}.service"
ENV_PATH=""
GENERATED_ACCESS_KEY=""

require_root() {
	if [ "$(id -u)" -ne 0 ]; then
		echo "Please run install.sh as root, for example with: sudo bash install.sh" >&2
		exit 1
	fi
}

require_command() {
	if ! command -v "$1" >/dev/null 2>&1; then
		echo "Missing required command: $1" >&2
		exit 1
	fi
}

validate_install_dir() {
	case "${INSTALL_DIR}" in
	"")
		echo "INSTALL_DIR must not be empty." >&2
		exit 1
		;;
	/)
		echo "INSTALL_DIR must not be /." >&2
		exit 1
		;;
	/*)
		;;
	*)
		echo "INSTALL_DIR must be an absolute path. Got: ${INSTALL_DIR}" >&2
		exit 1
		;;
	esac

	case "${INSTALL_DIR}" in
	*[[:space:]]*)
		echo "INSTALL_DIR must not contain whitespace. Got: ${INSTALL_DIR}" >&2
		exit 1
		;;
	esac

	ENV_PATH="${INSTALL_DIR}/.env"
}

detect_platform() {
	case "$(uname -s)" in
	Linux)
		PLATFORM_OS="linux"
		;;
	*)
		echo "install.sh currently supports Linux only." >&2
		exit 1
		;;
	esac

	case "$(uname -m)" in
	x86_64 | amd64)
		PLATFORM_ARCH="amd64"
		;;
	aarch64 | arm64)
		PLATFORM_ARCH="arm64"
		;;
	*)
		echo "Unsupported CPU architecture: $(uname -m)" >&2
		exit 1
		;;
	esac
}

download_release_archive() {
	RELEASE_URL="https://github.com/${REPOSITORY}/releases/latest/download/aggr-${PLATFORM_OS}-${PLATFORM_ARCH}.tar.gz"
	TEMP_DIR="$(mktemp -d)"
	trap 'rm -rf "${TEMP_DIR}"' EXIT

	echo "Downloading ${RELEASE_URL}"
	curl -fL "${RELEASE_URL}" -o "${TEMP_DIR}/aggr.tar.gz"
	tar -xzf "${TEMP_DIR}/aggr.tar.gz" -C "${TEMP_DIR}"
}

detect_nologin_shell() {
	if [ -x /usr/sbin/nologin ]; then
		printf '/usr/sbin/nologin\n'
		return
	fi
	if [ -x /sbin/nologin ]; then
		printf '/sbin/nologin\n'
		return
	fi

	printf '/bin/false\n'
}

escape_sed_replacement() {
	printf '%s\n' "$1" | sed 's/[\/&\\]/\\&/g'
}

install_service_unit() {
	local escaped_install_dir
	local rendered_service_path

	escaped_install_dir="$(escape_sed_replacement "${INSTALL_DIR}")"
	rendered_service_path="${TEMP_DIR}/aggr.service.rendered"
	sed "s/\/opt\/aggr/${escaped_install_dir}/g" "${TEMP_DIR}/aggr.service" >"${rendered_service_path}"
	install -m 0644 "${rendered_service_path}" "${SERVICE_PATH}"
}

ensure_service_account() {
	if ! getent group aggr >/dev/null 2>&1; then
		groupadd --system aggr
	fi

	if ! id -u aggr >/dev/null 2>&1; then
		useradd --system --gid aggr --home-dir "${INSTALL_DIR}" --shell "$(detect_nologin_shell)" aggr
	fi
}

ensure_environment_file() {
	if [ ! -f "${ENV_PATH}" ]; then
		GENERATED_ACCESS_KEY="$(openssl rand -hex 32)"
		cat >"${ENV_PATH}" <<EOF
AGGR_ACCESS_KEY=${GENERATED_ACCESS_KEY}
# AGGR_ADDR=:8080
# AGGR_DB_PATH=aggr.db
EOF
		return
	fi

	if grep -Eq '^AGGR_ACCESS_KEY=' "${ENV_PATH}"; then
		return
	fi

	GENERATED_ACCESS_KEY="$(openssl rand -hex 32)"
	printf '\nAGGR_ACCESS_KEY=%s\n' "${GENERATED_ACCESS_KEY}" >>"${ENV_PATH}"
}

install_files() {
	mkdir -p "${INSTALL_DIR}"
	install -m 0755 "${TEMP_DIR}/aggr" "${INSTALL_DIR}/aggr"
	install_service_unit
	ensure_environment_file
	chown -R aggr:aggr "${INSTALL_DIR}"
	chmod 0750 "${INSTALL_DIR}"
	chmod 0600 "${ENV_PATH}"
}

restart_service() {
	systemctl daemon-reload
	systemctl enable "${SERVICE_NAME}" >/dev/null

	if systemctl is-active --quiet "${SERVICE_NAME}"; then
		systemctl restart "${SERVICE_NAME}"
	else
		systemctl start "${SERVICE_NAME}"
	fi
}

print_summary() {
	echo
	echo "Installed aggr to ${INSTALL_DIR}"
	echo "Systemd service: ${SERVICE_NAME}"
	echo "Environment file: ${ENV_PATH}"
	if [ -n "${GENERATED_ACCESS_KEY}" ]; then
		echo "Generated AGGR_ACCESS_KEY: ${GENERATED_ACCESS_KEY}"
	else
		echo "Existing AGGR_ACCESS_KEY preserved from ${ENV_PATH}"
	fi
	echo
	echo "Check service status with:"
	echo "  sudo systemctl status ${SERVICE_NAME}"
}

main() {
	require_root
	require_command curl
	require_command tar
	require_command systemctl
	require_command openssl
	validate_install_dir
	detect_platform
	download_release_archive
	ensure_service_account
	install_files
	restart_service
	print_summary
}

main "$@"
