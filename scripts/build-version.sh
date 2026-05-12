#!/bin/sh

set -eu

if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
	printf 'dev\n'
	exit 0
fi

commit="$(git rev-parse --short=8 HEAD 2>/dev/null || printf 'dev')"
dirty_suffix=''

if [ -n "$(git status --porcelain --untracked-files=no 2>/dev/null)" ]; then
	dirty_suffix='-dirty'
fi

if tag="$(git describe --tags --exact-match 2>/dev/null)"; then
	printf '%s%s\n' "$tag" "$dirty_suffix"
	exit 0
fi

if tag="$(git describe --tags --abbrev=0 2>/dev/null)"; then
	printf '%s-%s%s\n' "$tag" "$commit" "$dirty_suffix"
	exit 0
fi

printf '%s%s\n' "$commit" "$dirty_suffix"
