#!/usr/bin/env bash
# 构建 relayx-agent 二进制，生成 SBOM 与签名（可选工具）。
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
OUT="${OUT:-dist}"
mkdir -p "$OUT"

echo "building agent ($VERSION)"
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o "$OUT/agent" ./cmd/agent

if command -v syft >/dev/null 2>&1; then
  syft "$OUT/agent" -o spdx-json > "$OUT/agent.sbom.json"
fi
if command -v cosign >/dev/null 2>&1; then
  cosign sign-blob --yes "$OUT/agent" > "$OUT/agent.sig"
fi

echo "done: $OUT"
