#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SYSTEMD_USER_DIR="${HOME}/.config/systemd/user"
SERVICE_NAME="cogmemory.service"

mkdir -p "${SYSTEMD_USER_DIR}"
cp "${SCRIPT_DIR}/${SERVICE_NAME}" "${SYSTEMD_USER_DIR}/${SERVICE_NAME}"

if command -v systemctl >/dev/null 2>&1; then
	systemctl --user daemon-reload
fi

echo "Installed ${SERVICE_NAME} to ${SYSTEMD_USER_DIR}/${SERVICE_NAME}"
echo "Enable and start with:"
echo "  systemctl --user enable --now cogmemory"
