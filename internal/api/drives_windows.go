//go:build windows

package api

import "golang.org/x/sys/windows"

// logicalDrives 按 GetLogicalDrives 的位掩码列出盘符根目录（C:\ D:\ …）。
// 只读系统记下的盘符表，不碰盘本身：映射了却离线的网络盘不会把请求卡住。
func logicalDrives() []string {
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return nil
	}
	var roots []string
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) != 0 {
			roots = append(roots, string(rune('A'+i))+`:\`)
		}
	}
	return roots
}
