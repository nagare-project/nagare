#!/usr/bin/env bash
# goreleaser before hook：前端构建不在 goreleaser 里做（release workflow 先跑 bun run build），
# 这里只确认 go:embed 的输入已经就位，否则会静默打出一个 API-only 的二进制。
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
if [ ! -f "$root/web/index.html" ]; then
  echo "错误：web/index.html 不存在，二进制会退化成 API-only 模式。" >&2
  echo "先执行：cd frontend && bun install --frozen-lockfile && bun run build" >&2
  exit 1
fi
echo "==> web/ 前端产物就绪"
