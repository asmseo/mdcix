#!/usr/bin/env bash
# Install mdcix into the current directory (or $PREFIX/bin).
set -euo pipefail
PREFIX="${PREFIX:-.}"
REPO="${REPO:-github.com/asmseo/mdcix/cmd/mdcix@latest}"

if ! command -v go >/dev/null 2>&1; then
  echo "go is required to install mdcix" >&2
  exit 1
fi

echo "Installing mdcix via: go install $REPO"
GOBIN="$(cd "$PREFIX" && pwd)" go install "$REPO"
echo "Installed to $PREFIX/mdcix (or \$GOBIN)"
echo "Run: ./mdcix serve"
echo "Then: export HTTP_PROXY=http://127.0.0.1:8080 HTTPS_PROXY=http://127.0.0.1:8080"
