#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

export WORKSPACE="${WORKSPACE:-$REPO_ROOT}"

if command -v direnv &>/dev/null && [ -f "$WORKSPACE/.envrc" ]; then
  eval "$(direnv export bash 2>/dev/null || true)"
fi

cd "$WORKSPACE"

CONTAINER_AWS_CONFIG=/home/akb/.aws/config

exec docker run --rm -i \
  -e AWS_PROFILE \
  -e AWS_REGION \
  -e AWS_SDK_LOAD_CONFIG=1 \
  -e "AWS_CONFIG_FILE=$CONTAINER_AWS_CONFIG" \
  -v "${AWS_CONFIG_FILE:-$HOME/.aws/config}:$CONTAINER_AWS_CONFIG:ro" \
  -v "$HOME/.aws/sso/cache:/home/akb/.aws/sso/cache:ro" \
  -v "$HOME/.aws/cli/cache:/home/akb/.aws/cli/cache:ro" \
  ghcr.io/bkosm/akb:main s3 "$@"
