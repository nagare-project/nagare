package sourceplugin

import (
	"os"
	"path/filepath"
	"runtime"
)

// Bundled 是随安装包捆绑的插件位置。
type Bundled struct {
	Executable string
	Root       string
}

// DetectBundled 在安装包约定的位置找捆绑的 Nagare Source（引擎 + 只含 BT 规则的 repo）。
//
// 约定（与 .goreleaser.yaml / build-app.sh / nagare.nsi 对齐）：
//   - <exeDir>/nagare-source/nagare-source-<GOARCH>[.exe]  macOS universal .app 带两个引擎
//   - <exeDir>/nagare-source/nagare-source[.exe]           Windows / Linux 归档
//   - <exeDir>/../lib/nagare/nagare-source/nagare-source   deb / rpm（/usr/bin → /usr/lib/nagare）
//
// 引擎旁边必须有 repo/schema/source-v1.schema.json，否则不算捆绑（lock 未钉版本时那里
// 只有一个 README.txt）。exeDir 为空时返回 false，纯 Go 单测里不会去碰真实文件系统。
func DetectBundled(exeDir string) (Bundled, bool) {
	if exeDir == "" {
		return Bundled{}, false
	}
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	candidates := []string{
		filepath.Join(exeDir, "nagare-source", "nagare-source-"+runtime.GOARCH+suffix),
		filepath.Join(exeDir, "nagare-source", "nagare-source"+suffix),
		filepath.Join(exeDir, "..", "lib", "nagare", "nagare-source", "nagare-source"+suffix),
	}
	for _, executable := range candidates {
		info, err := os.Stat(executable)
		if err != nil || info.IsDir() {
			continue
		}
		root := filepath.Join(filepath.Dir(executable), "repo")
		if _, err := os.Stat(filepath.Join(root, "schema", "source-v1.schema.json")); err != nil {
			continue
		}
		absExecutable, err := filepath.Abs(executable)
		if err != nil {
			continue
		}
		absRoot, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		return Bundled{Executable: filepath.Clean(absExecutable), Root: filepath.Clean(absRoot)}, true
	}
	return Bundled{}, false
}
