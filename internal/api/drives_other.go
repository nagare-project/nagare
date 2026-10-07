//go:build !windows

package api

// logicalDrives 只在 Windows 上有意义（见 drives_windows.go）。
func logicalDrives() []string { return nil }
