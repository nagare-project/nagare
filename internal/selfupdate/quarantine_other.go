//go:build !darwin

package selfupdate

// clearQuarantine 只有 macOS 需要（Gatekeeper 的 quarantine 标记是 macOS 独有的）。
func clearQuarantine(string) {}
