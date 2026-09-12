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
// 约定（与 .goreleaser.yaml / build-app.sh / nagare.nsi 对齐），引擎 → 规则目录：
//   - <exeDir>/nagare-source-<GOARCH>            → <exeDir>/../Resources/nagare-source/repo
//     macOS .app：bundle 规范要求 MacOS/ 下只放可执行文件，数据在 Resources/
//   - <exeDir>/nagare-source/nagare-source-<GOARCH>[.exe] → 同目录 repo/   macOS tar.gz
//   - <exeDir>/nagare-source/nagare-source[.exe]          → 同目录 repo/   Windows / Linux 归档
//   - <exeDir>/../lib/nagare/nagare-source/nagare-source  → 同目录 repo/   deb / rpm
//
// 规则目录里必须有 schema/source-v1.schema.json，否则不算捆绑（lock 未钉版本时那里
// 只有一个 README.txt）。exeDir 为空时返回 false，纯 Go 单测里不会去碰真实文件系统。
func DetectBundled(exeDir string) (Bundled, bool) {
	if exeDir == "" {
		return Bundled{}, false
	}
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	type candidate struct{ executable, root string }
	sibling := func(executable string) candidate {
		return candidate{executable, filepath.Join(filepath.Dir(executable), "repo")}
	}
	candidates := []candidate{
		{filepath.Join(exeDir, "nagare-source-"+runtime.GOARCH), filepath.Join(exeDir, "..", "Resources", "nagare-source", "repo")},
		sibling(filepath.Join(exeDir, "nagare-source", "nagare-source-"+runtime.GOARCH+suffix)),
		sibling(filepath.Join(exeDir, "nagare-source", "nagare-source"+suffix)),
		sibling(filepath.Join(exeDir, "..", "lib", "nagare", "nagare-source", "nagare-source"+suffix)),
	}
	for _, c := range candidates {
		executable, root := c.executable, c.root
		info, err := os.Stat(executable)
		if err != nil || info.IsDir() {
			continue
		}
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
