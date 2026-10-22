package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nagare.exe 的资源（scripts/windows/gen-winres.sh 生成、入库）：manifest 必须是 PerMonitorV2（与 mpv.exe 一致，
// 否则桌面窗口里跨进程挂进来的 mpv 窗口会被系统强制重置 DPI）、asInvoker，还要带应用图标。goreleaser 与 CI 的
// windows 构建靠「同目录的 .syso 自动链进去」，文件没了也不会报错 —— 这里守着。
func TestWindowsResourcesHaveManifestAndIcon(t *testing.T) {
	for _, name := range []string{"rsrc_windows_amd64.syso", "rsrc_windows_arm64.syso"} {
		b, err := os.ReadFile(name)
		require.NoError(t, err, "缺少 %s：跑 scripts/windows/gen-winres.sh", name)
		s := string(b)
		assert.Contains(t, s, "permonitorv2", name)
		assert.Contains(t, s, `level="asInvoker"`, name)
		assert.Contains(t, s, "A\x00P\x00P\x00", name+" 里要有名为 APP 的图标资源（窗口类按这个名字取图标）")
	}
}
