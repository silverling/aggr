#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
VERSION="${1:-}"
PREVIOUS_TAG="${2:-}"
END_REF="${3:-HEAD}"
CONTRIBUTORS_FILE="${4:-}"
DATE_UTC="$(date -u +%Y-%m-%d)"

if [ -z "${VERSION}" ]; then
	echo "Usage: scripts/generate-changelog.sh <version> [previous-tag]" >&2
	exit 1
fi

if [ -n "${PREVIOUS_TAG}" ]; then
	RANGE="${PREVIOUS_TAG}..${END_REF}"
else
	RANGE="${END_REF}"
fi

COMMITS="$(git -C "${ROOT_DIR}" log --no-merges --pretty=format:'- %s (%h)' "${RANGE}")"
if [ -z "${COMMITS}" ]; then
	COMMITS="- No user-facing changes recorded."
fi

CONTRIBUTORS=""
if [ -n "${CONTRIBUTORS_FILE}" ] && [ -f "${CONTRIBUTORS_FILE}" ]; then
	CONTRIBUTORS="$(grep -v '^[[:space:]]*$' "${CONTRIBUTORS_FILE}" | sort -u | sed 's/^/- @/')"
fi
if [ -z "${CONTRIBUTORS}" ]; then
	CONTRIBUTORS="- No pull-request contributors recorded."
fi

RELEASE_NOTES_FILE="$(mktemp)"
cat >"${RELEASE_NOTES_FILE}" <<EOF
## ${VERSION} - ${DATE_UTC}

${COMMITS}

### Contributors

${CONTRIBUTORS}
EOF

printf '%s\n' "${RELEASE_NOTES_FILE}"
