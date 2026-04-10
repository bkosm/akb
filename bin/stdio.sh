#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

export WORKSPACE="${WORKSPACE:-$REPO_ROOT}"

if command -v direnv &>/dev/null && [ -f "$WORKSPACE/.envrc" ]; then
  eval "$(direnv export bash 2>/dev/null || true)"
fi

cd "$WORKSPACE"
exec go run go/akb/cmd/stdio/main.go "$@"
