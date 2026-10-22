#!/usr/bin/env bash
# 下载 versions.lock 里的全部上游源码到 ${SOURCES_DIR}（默认 $WORK/sources）并逐个校验 sha256。
# 已下载且哈希相符的跳过。build.sh 自己也会按需下载，这个脚本给 CI 预热缓存、给源码包用。
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=lib/env.sh
. "$here/lib/env.sh"
# shellcheck source=lib/sources.sh
. "$here/lib/sources.sh"

require_tools curl shasum
for name in $(lock_names); do
  fetch_source "$name"
done
log "源码齐全：$SOURCES_DIR"
