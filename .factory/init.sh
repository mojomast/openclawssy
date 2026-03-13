#!/bin/bash
set -e

cd "$(dirname "$0")/.."

echo "=== openclawssy mission init ==="

# Go dependencies
echo "Checking Go toolchain..."
go version

# Build Go binary to verify compilation
echo "Building openclawssy binary..."
go build -o ./bin/openclawssy ./cmd/openclawssy
echo "Build OK"

# Frontend dependencies
echo "Installing frontend dependencies..."
cd internal/channels/dashboard/ui
if [ -f package.json ]; then
  npm install --prefer-offline 2>/dev/null || npm install
fi
cd - > /dev/null

echo "=== init complete ==="
