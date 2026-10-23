//go:build desktoptest

package main

import "os"

// 测试构建（-tags desktoptest，Windows 测试包与 CI 的冒烟测试）才有的开关：NAGARE_TEST_NO_TORRENT=1 时不起磁力引擎。
// 引擎一起来就在所有网卡上监听 BT 端口、还会向路由器要 UPnP 映射 —— 用户的 Windows 会在测试半路弹防火墙对话框抢走焦点，
// 点了「允许」还会留下一条永久规则。测试用不着磁力。
func torrentDisabledForTest() bool { return os.Getenv("NAGARE_TEST_NO_TORRENT") == "1" }

// NAGARE_TEST_NO_BROWSER=1：不开系统默认浏览器（自动测试跑在用户自己的电脑上：WebView2 起不来退回浏览器模式时、
// 第二次启动找不到窗口时，都不能把用户的浏览器开出来 —— 那一步记成失败就够了）。手动检查不设它。
func browserDisabledForTest() bool { return os.Getenv("NAGARE_TEST_NO_BROWSER") == "1" }
