package mpv

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// 播放器界面：nagare 自己的 mpv 配置目录（mpv.conf、input.conf、Lua 写的控制条与右键菜单），
// 随二进制分发。
//
// nagare 一律用这个目录启动 mpv（--config-dir），不读用户自己的 mpv 配置（2026-10-22 用户定）。
// 这也顺带消掉了两处冲突：用户配置里常见的 autocreate-playlist 会让 mpv 自己接着播同目录的下一集，
// 而 nagare 的会话仍以为在播第一集（进度写错、弹幕错配）；save-position-on-quit 记的续播位置会和
// nagare 传的 --start 打架。

//go:embed all:ui
var uiFiles embed.FS

// uiDirName 是运行时目录下放界面配置的子目录名。
const uiDirName = "mpv-config"

// PrepareUIConfig 把内置界面配置同步到 dir/mpv-config，返回它的绝对路径（交给 --config-dir）。
//
// 每次起播都会调：内容没变的不重写；被改过的改回去；旧版本留下、这一版已经没有的文件删掉 ——
// mpv 会加载 scripts/ 下的每一个文件，留着旧脚本就是两套界面叠在一起。只清理内置配置自己占的
// 那几个顶层路径，mpv 往配置目录里写的其他东西（比如 watch_later/）不碰。
func PrepareUIConfig(dir string) (string, error) {
	root, err := filepath.Abs(filepath.Join(dir, uiDirName))
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", fmt.Errorf("创建播放器界面目录: %w", err)
	}
	want := map[string]bool{}
	managed := map[string]bool{} // 内置配置占用的顶层名字
	err = fs.WalkDir(uiFiles, "ui", func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(p, "ui"), "/")
		if rel == "" {
			return nil
		}
		managed[strings.SplitN(rel, "/", 2)[0]] = true
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(root, filepath.FromSlash(rel)), 0o700)
		}
		want[rel] = true
		content, err := uiFiles.ReadFile(p)
		if err != nil {
			return err
		}
		return writeIfChanged(filepath.Join(root, filepath.FromSlash(rel)), content)
	})
	if err != nil {
		return "", fmt.Errorf("写出播放器界面: %w", err)
	}
	if err := removeStale(root, want, managed); err != nil {
		return "", fmt.Errorf("清理旧的播放器界面文件: %w", err)
	}
	return root, nil
}

// writeIfChanged 只在内容不同时写：先写临时文件再改名，别让 mpv 读到半截脚本。
func writeIfChanged(target string, content []byte) error {
	if existing, err := os.ReadFile(target); err == nil && bytes.Equal(existing, content) {
		return nil
	}
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, content, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// removeStale 删掉 managed 顶层路径之下、不在 want 里的文件，再删掉因此变空的目录。
func removeStale(root string, want, managed map[string]bool) error {
	var dirs []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relOS, err := filepath.Rel(root, p)
		if err != nil || relOS == "." {
			return err
		}
		rel := filepath.ToSlash(relOS)
		if !managed[strings.SplitN(rel, "/", 2)[0]] {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			dirs = append(dirs, p)
			return nil
		}
		if !want[rel] {
			return os.Remove(p)
		}
		return nil
	})
	if err != nil {
		return err
	}
	// 由深到浅删空目录；非空的 Remove 会失败，忽略即可
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, d := range dirs {
		if rel, _ := filepath.Rel(root, d); embedHasDir(filepath.ToSlash(rel)) {
			continue
		}
		_ = os.Remove(d)
	}
	return nil
}

// embedHasDir 报告内置配置里有没有这个目录（有的话即使空了也保留）。
func embedHasDir(rel string) bool {
	info, err := fs.Stat(uiFiles, path.Join("ui", rel))
	return err == nil && info.IsDir()
}
