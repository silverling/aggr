#!/usr/bin/env bash

set -euo pipefail

if [ "$#" -ne 2 ]; then
	echo "Usage: scripts/package-release.sh <goos> <goarch>" >&2
	exit 1
fi

TARGET_OS="$1"
TARGET_ARCH="$2"
ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
DIST_DIR="${ROOT_DIR}/dist"
VERSION="${VERSION:-$("${ROOT_DIR}/scripts/build-version.sh")}"
ASSET_NAME="aggr-${TARGET_OS}-${TARGET_ARCH}.tar.gz"
WORK_DIR="$(mktemp -d)"

cleanup() {
	rm -rf "${WORK_DIR}"
}

trap cleanup EXIT

mkdir -p "${DIST_DIR}"
pnpm --dir "${ROOT_DIR}/web" build
GOOS="${TARGET_OS}" GOARCH="${TARGET_ARCH}" go build -ldflags "-X github.com/silverling/aggr/server.buildVersion=${VERSION} -w -s" -o "${WORK_DIR}/aggr" "${ROOT_DIR}/server/cmd/aggr"
cp "${ROOT_DIR}/deploy/systemd/aggr.service" "${WORK_DIR}/aggr.service"
tar -czf "${DIST_DIR}/${ASSET_NAME}" -C "${WORK_DIR}" aggr aggr.service
echo "Created ${DIST_DIR}/${ASSET_NAME}"
