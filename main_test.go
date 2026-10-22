package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// launchURL 必须指向 127.0.0.1（不是 localhost：Host 白名单两者都收，但
// 127.0.0.1 不经 DNS，少一层被劫持的可能）并带上 token。
func TestLaunchURL(t *testing.T) {
	url := launchURL(8590, "deadbeef")
	require.Equal(t, "http://127.0.0.1:8590/?token=deadbeef", url)
}

// 托盘菜单显示 / 复制的是裸地址：token 不能进菜单也不能进剪贴板。
func TestBaseURLCarriesNoToken(t *testing.T) {
	require.Equal(t, "http://127.0.0.1:8590/", baseURL(8590))
}

// 「关于」直接落到设置页的 about 分区，token 在 query、分区在 hash。
func TestAboutURL(t *testing.T) {
	require.Equal(t, "http://127.0.0.1:8590/settings?token=deadbeef#about", aboutURL(8590, "deadbeef"))
}
