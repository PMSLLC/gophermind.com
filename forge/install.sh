#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT_DIR"

echo "Building Forge..."
go build -o forge ./cmd/forge

echo "Built: $ROOT_DIR/forge"
echo
echo "Example usage:"
echo "  ./forge ask \"Explain this repository\""
echo "  ./forge --mode auto edit \"Add tests for the auth middleware\""
