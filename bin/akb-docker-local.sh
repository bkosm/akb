#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

export WORKSPACE="${WORKSPACE:-$REPO_ROOT}"

if command -v direnv &>/dev/null && [ -f "$WORKSPACE/.envrc" ]; then
  eval "$(direnv export bash 2>/dev/null || true)"
fi

cd "$WORKSPACE"

DOCKER_TMP="$WORKSPACE/tmp/docker"
mkdir -p "$DOCKER_TMP"

CONTAINER_CONFIG=/home/akb/.config/akb/config.json

exec docker run --rm -i --privileged \
  -v "$HOME/.config/akb:/home/akb/.config/akb" \
  -v "$DOCKER_TMP:/tmp/docker" \
  ghcr.io/bkosm/akb:main local --path "$CONTAINER_CONFIG" "$@"
