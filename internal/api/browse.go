package api

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	errs "github.com/nagare-project/nagare/internal/errors"
	"github.com/nagare-project/nagare/internal/httpserver"
	"github.com/nagare-project/nagare/internal/library"
)

// 本机目录浏览（GET /api/fs/dirs）：添加媒体库文件夹时逐层点选目录，
// 免得第一次用的人手敲 /Users/xxx/... 这种绝对路径（做法照 seanime 的 directory-selector）。
//
// 只返回【目录名】，不列文件、不读内容；与其余 /api/* 一样走 token + Host 白名单，
// 浏览器里别的网页拿不到。nagare 本来就能扫描用户给的任意目录，这里没有多给出权限。

// maxBrowseEntries 是一次最多返回多少个子目录；再多就截断并标出来（列表本就是给人点的）。
const maxBrowseEntries = 1000

type dirEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type dirListing struct {
	// Path 为空表示「常用位置」视图：主目录、影片、下载、外接硬盘……
	Path      string     `json:"path"`
	Parent    string     `json:"parent"`
	Dirs      []dirEntry `json:"dirs"`
	Truncated bool       `json:"truncated"`
}

// place 是主目录下一个值得放进「常用位置」的子目录。
type place struct {
	label string
	dir   string
}

// volumeRoots 返回「其下每个子目录都是一块挂载盘」的目录（测试时替换成临时目录）。
// Windows 不走这里，盘符单独枚举。
var volumeRoots = func(goos string) []string {
	switch goos {
	case "darwin":
		return []string{"/Volumes"}
	case "linux":
		user := os.Getenv("USER")
		roots := []string{"/mnt"}
		if user != "" {
			roots = append([]string{filepath.Join("/media", user), filepath.Join("/run/media", user)}, roots...)
		}
		return roots
	}
	return nil
}

func (h *Handler) browseDirs(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		home, _ := os.UserHomeDir()
		httpserver.WriteJSON(w, http.StatusOK, dirListing{Dirs: placeDirs(runtime.GOOS, home)})
		return
	}
	listing, err := listDir(path)
	if err != nil {
		writeErr(w, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, listing)
}

// listDir 列出 path 下的可见子目录（含指向目录的符号链接），按文件名自然序。
func listDir(raw string) (dirListing, error) {
	path := filepath.Clean(raw)
	if !filepath.IsAbs(path) {
		return dirListing{}, errs.New(errs.CategoryInput, "fs.browse", "请输入绝对路径", "")
	}
	info, err := os.Stat(path)
	if err != nil {
		return dirListing{}, browseError(path, err)
	}
	if !info.IsDir() {
		return dirListing{}, errs.New(errs.CategoryInput, "fs.browse", "这不是文件夹："+path, "")
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return dirListing{}, browseError(path, err)
	}

	dirs := make([]dirEntry, 0, len(entries))
	for _, e := range entries {
		full := filepath.Join(path, e.Name())
		if hiddenDir(e.Name()) || !entryIsDir(e, full) {
			continue
		}
		dirs = append(dirs, dirEntry{Name: e.Name(), Path: full})
	}
	sort.SliceStable(dirs, func(i, j int) bool {
		return library.CompareFileNames(dirs[i].Name, dirs[j].Name) < 0
	})
	truncated := len(dirs) > maxBrowseEntries
	if truncated {
		dirs = dirs[:maxBrowseEntries]
	}
	parent := filepath.Dir(path)
	if parent == path {
		parent = ""
	}
	return dirListing{Path: path, Parent: parent, Dirs: dirs, Truncated: truncated}, nil
}

// browseError 把读目录失败分成「不存在」「没权限」「其他」三种用户能看懂的说法。
func browseError(path string, err error) error {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return errs.Wrap(errs.CategoryFS, "fs.browse", "文件夹不存在："+path, "", err)
	case errors.Is(err, fs.ErrPermission):
		// macOS 的「下载」「桌面」与外接硬盘受隐私权限保护，第一次访问会弹授权框；点了拒绝就到这里
		return errs.Wrap(errs.CategoryFS, "fs.browse", "没有权限读取这个文件夹："+path,
			"如果是 macOS 的隐私权限拦下的，在「系统设置 › 隐私与安全性」里允许 nagare 访问它", err)
	default:
		return errs.Wrap(errs.CategoryFS, "fs.browse", "读取文件夹失败："+path, "", err)
	}
}

// hiddenDir：点开头的隐藏目录，以及 Windows 盘根上的两个系统目录。
func hiddenDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch strings.ToLower(name) {
	case "$recycle.bin", "system volume information":
		return true
	}
	return false
}

// entryIsDir 认目录，也认指向目录的符号链接（媒体库常用软链接把别处的番链进来）。
func entryIsDir(e fs.DirEntry, full string) bool {
	if e.IsDir() {
		return true
	}
	if e.Type()&fs.ModeSymlink == 0 {
		return false
	}
	info, err := os.Stat(full)
	return err == nil && info.IsDir()
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// placeDirs 是「常用位置」：主目录及其下的影片/视频、下载、桌面，再加挂载着的外接硬盘。
// 不存在的一律不列。
func placeDirs(goos, home string) []dirEntry {
	out := []dirEntry{}
	seen := map[string]bool{}
	add := func(name, path string) {
		if path == "" || seen[path] || !isDir(path) {
			return
		}
		seen[path] = true
		out = append(out, dirEntry{Name: name, Path: path})
	}
	if home != "" {
		add("主目录", home)
		for _, p := range homePlaces(goos) {
			add(p.label, filepath.Join(home, p.dir))
		}
	}
	for _, v := range volumeDirs(goos) {
		add(v.Name, v.Path)
	}
	return out
}

func homePlaces(goos string) []place {
	if goos == "darwin" {
		return []place{{"影片", "Movies"}, {"下载", "Downloads"}, {"桌面", "Desktop"}}
	}
	return []place{{"视频", "Videos"}, {"下载", "Downloads"}, {"桌面", "Desktop"}}
}

// volumeDirs 列出挂载着的盘。macOS 的 /Volumes 里系统盘是一个指回 / 的符号链接，跳过它。
func volumeDirs(goos string) []dirEntry {
	if goos == "windows" {
		var out []dirEntry
		for letter := 'C'; letter <= 'Z'; letter++ {
			root := string(letter) + `:\`
			if isDir(root) {
				out = append(out, dirEntry{Name: root, Path: root})
			}
		}
		return out
	}
	var out []dirEntry
	for _, root := range volumeRoots(goos) {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if hiddenDir(e.Name()) || !e.IsDir() {
				continue
			}
			out = append(out, dirEntry{Name: e.Name(), Path: filepath.Join(root, e.Name())})
		}
	}
	return out
}
