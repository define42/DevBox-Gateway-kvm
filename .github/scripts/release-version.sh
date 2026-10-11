#!/usr/bin/env bash
set -euo pipefail

if (( $# > 1 )); then
    printf 'Usage: %s [commit]\n' "$0" >&2
    exit 1
fi

revision=${1:-HEAD}
commit=$(git rev-parse --verify --end-of-options "${revision}^{commit}")
timestamp=$(git show --no-patch --format=%ct "$commit")
version_date=$(date --utc --date="@$timestamp" '+%Y.%-m.%-d-%H%M%S')
printf 'v%s-g%s\n' "$version_date" "${commit:0:12}"
