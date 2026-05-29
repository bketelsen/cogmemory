#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SYSTEMD_USER_DIR="${HOME}/.config/systemd/user"
CONFIG_DIR="${HOME}/.config/cogmemory"
STORE_DIR="${HOME}/.local/share/cogmemory-test/memory"
SERVICE_NAME="cogmemory-test.service"

# Create directories
mkdir -p "${SYSTEMD_USER_DIR}/${SERVICE_NAME}.d" "${CONFIG_DIR}" "${STORE_DIR}"

# Install unit + socket-perms drop-in
cp "${SCRIPT_DIR}/${SERVICE_NAME}" "${SYSTEMD_USER_DIR}/${SERVICE_NAME}"
cp "${SCRIPT_DIR}/cogmemory-test.service.d/socket-perms.conf" \
	"${SYSTEMD_USER_DIR}/${SERVICE_NAME}.d/socket-perms.conf"

# Render config from the example, substituting the real $HOME (daemon does not expand ~).
# Won't clobber an existing config-test.yml.
if [[ ! -f "${CONFIG_DIR}/config-test.yml" ]]; then
	sed "s|__HOME__|${HOME}|g" "${SCRIPT_DIR}/config-test.example.yml" \
		>"${CONFIG_DIR}/config-test.yml"
	echo "Wrote ${CONFIG_DIR}/config-test.yml"
else
	echo "Kept existing ${CONFIG_DIR}/config-test.yml"
fi

if command -v systemctl >/dev/null 2>&1; then
	systemctl --user daemon-reload
fi

echo "Installed ${SERVICE_NAME} to ${SYSTEMD_USER_DIR}/${SERVICE_NAME}"
echo "Enable and start with:"
echo "  systemctl --user enable --now cogmemory-test"
