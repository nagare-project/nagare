#!/usr/bin/env bash
# 为一个 iOS 目标构建 libmpv 及其全部依赖（静态库），装进 $WORK/prefix/<目标>。
#
# 用法：
#   build.sh --arch ios-arm64|iossim-arm64|iossim-x86_64 [组件...]
#   build.sh --keys --arch <目标>               只打印每个组件的缓存键
#
# 构建流程、缓存与源码校验全部复用 macOS 那套（scripts/macos/libmpv/build.sh）：这里只是把
# lib/recipes.sh 作为 overlay 交给它，换掉与目标平台有关的函数和几个组件的配方。
# 工作目录默认 dist/libmpv-ios（LIBMPV_WORK 可改）。
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
case "${1:-}" in -h | --help) sed -n '2,11p' "$0"; exit 0 ;; esac
LIBMPV_OVERLAY="$here/lib/recipes.sh" exec bash "$here/../../macos/libmpv/build.sh" "$@"
