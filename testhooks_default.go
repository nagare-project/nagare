//go:build !desktoptest

package main

// torrentDisabledForTest 在正式构建里恒为假（开关只在测试构建里，见 testhooks_desktoptest.go）。
func torrentDisabledForTest() bool { return false }

// browserDisabledForTest 在正式构建里恒为假。
func browserDisabledForTest() bool { return false }
