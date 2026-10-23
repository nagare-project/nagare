package selfupdate

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

// scripts/macos/build-app.sh 的体积守卫按旧客户端自更新上限的一半卡 .app.zip。那些上限一旦发出去就改不了
// （装着旧版本的人用的是旧版本里编进去的数）：这里把两边绑住 —— 守卫不许高于上限的一半，上限也不许
// 被调小到守卫之下（调小会让已经发出去的包在新客户端上更新失败）。
func TestBuildAppSizeGuardBelowUpdaterLimits(t *testing.T) {
	script, err := os.ReadFile(filepath.Join("..", "..", "scripts", "macos", "build-app.sh"))
	require.NoError(t, err)
	guard := func(name string) int64 {
		// \r?：Windows 的 checkout 可能把换行转成 CRLF
		m := regexp.MustCompile(`(?m)^` + name + `=(\d+)\r?$`).FindSubmatch(script)
		require.NotNil(t, m, "build-app.sh 里没有 %s", name)
		v, err := strconv.ParseInt(string(m[1]), 10, 64)
		require.NoError(t, err)
		return v
	}
	const mb = 1 << 20
	cases := []struct {
		name  string
		guard int64
		limit int64
	}{
		{"SIZE_GUARD_ARCHIVE_MB", guard("SIZE_GUARD_ARCHIVE_MB") * mb, maxArchiveBytes},
		{"SIZE_GUARD_ENTRY_MB", guard("SIZE_GUARD_ENTRY_MB") * mb, maxEntryBytes},
		{"SIZE_GUARD_TOTAL_MB", guard("SIZE_GUARD_TOTAL_MB") * mb, maxTotalBytes},
		{"SIZE_GUARD_ENTRIES", guard("SIZE_GUARD_ENTRIES"), maxEntries},
	}
	for _, c := range cases {
		require.Positive(t, c.guard, c.name)
		require.LessOrEqual(t, c.guard*2, c.limit, "%s 应不高于自更新上限的一半", c.name)
	}
}
