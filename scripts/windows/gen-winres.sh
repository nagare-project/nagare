#!/usr/bin/env bash
# 生成 nagare.exe 的 Windows 资源（rsrc_windows_amd64.syso / rsrc_windows_arm64.syso，放在仓库根目录）：
#   - 应用图标（packaging/icon/nagare.ico，资源名 APP：资源管理器、任务栏、窗口标题栏都用它）
#   - manifest：PerMonitorV2 的 DPI 感知（必须与 mpv.exe 一致 —— 桌面窗口里 mpv 的窗口是 nagare 窗口的子窗口，
#     跨进程父子窗口 DPI 模式不一致时系统会强制重置子窗口进程的 DPI 设置）、asInvoker、最低 Windows 10
# go build 在 CGO_ENABLED=0 下也会把同目录的 .syso 链进去（goreleaser、CI 的交叉编译都不用额外步骤）。
#
# 产物已入库；只有改了 packaging/windows/winres.json 或图标才需要重跑，然后把新的 .syso 一起提交。
# go-winres（MIT）只是构建工具，不进二进制。版本钉死，改版本前先在本机对比产物。
set -euo pipefail

root=$(cd "$(dirname "$0")/../.." && pwd)
cd "$root"
go run github.com/tc-hib/go-winres@v0.3.3 make \
  --in packaging/windows/winres.json \
  --arch amd64,arm64 \
  --out rsrc
ls -la rsrc_windows_*.syso
