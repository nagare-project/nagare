package api

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

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

// maxScanEntries 是一个目录最多读多少项（文件与目录合计），到了就不往下读：
// 先全读完再截断的话，十万项的目录每次请求都要几十 MB 内存。测试里会调小。
var maxScanEntries = 20000

// browseTimeout 是一次读目录的上限。离线的网络盘（睡着的 NAS、断开的 SMB）上
// stat / readdir 会一直阻塞，而系统调用取消不了 —— 只能不等它：超时先回话，
// 卡住的那个 goroutine 等它自己返回。测试里会调小。
var browseTimeout = 4 * time.Second

// browseSlots 限制同时在跑的目录读取数。卡在死盘上的调用回不来，
// 不设上限的话用户每重试一次就多压一个线程。
var browseSlots = make(chan struct{}, 4)

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
	// 不 TrimSpace：路径是上一次列表原样回传的，名字以空格结尾的文件夹也是合法的，
	// 修掉空格就会列出（并添加）另一个文件夹。手敲的路径由前端表单自己 trim。
	path := r.URL.Query().Get("path")
	listing, err := withBrowseTimeout(r.Context(), func() (dirListing, error) {
		if path == "" {
			home, _ := os.UserHomeDir()
			return dirListing{Dirs: placeDirs(runtime.GOOS, home)}, nil
		}
		return listDir(path)
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, listing)
}

// withBrowseTimeout 在独立 goroutine 里跑一次目录读取，最多等 browseTimeout。
// 名额（browseSlots）一直占到读取真正返回为止，所以卡死的调用最多压住 cap(browseSlots) 个线程。
func withBrowseTimeout(ctx context.Context, read func() (dirListing, error)) (dirListing, error) {
	select {
	case browseSlots <- struct{}{}:
	default:
		return dirListing{}, errs.New(errs.CategoryFS, "fs.browse",
			"还有文件夹读取卡着没回来（多半是离线的网络盘）", "等一会儿再试，或换一个文件夹")
	}
	type result struct {
		listing dirListing
		err     error
	}
	done := make(chan result, 1)
	go func() {
		defer func() { <-browseSlots }()
		listing, err := read()
		done <- result{listing, err}
	}()
	timer := time.NewTimer(browseTimeout)
	defer timer.Stop()
	select {
	case res := <-done:
		return res.listing, res.err
	case <-timer.C:
		return dirListing{}, errs.New(errs.CategoryFS, "fs.browse",
			"读取文件夹超时（可能是离线的网络盘）", "确认这块盘已经连上，或换一个文件夹")
	case <-ctx.Done():
		return dirListing{}, ctx.Err()
	}
}

// listDir 列出 path 下的可见子目录（含指向目录的符号链接），按文件名自然序。
func listDir(raw string) (dirListing, error) {
	path := filepath.Clean(raw)
	if !filepath.IsAbs(path) {
		return dirListing{}, errs.New(errs.CategoryInput, "fs.browse", "请输入绝对路径", "")
	}
	if deviceNamespace(path) {
		return dirListing{}, errs.New(errs.CategoryInput, "fs.browse", "不支持设备路径："+path, "")
	}
	info, err := os.Stat(path)
	if err != nil {
		return dirListing{}, browseError(path, err)
	}
	if !info.IsDir() {
		return dirListing{}, errs.New(errs.CategoryInput, "fs.browse", "这不是文件夹："+path, "")
	}
	dirs, partial, err := readSubdirs(path)
	if err != nil {
		return dirListing{}, browseError(path, err)
	}
	sort.SliceStable(dirs, func(i, j int) bool {
		return library.CompareFileNames(dirs[i].Name, dirs[j].Name) < 0
	})
	truncated := partial || len(dirs) > maxBrowseEntries
	if len(dirs) > maxBrowseEntries {
		dirs = dirs[:maxBrowseEntries]
	}
	parent := filepath.Dir(path)
	if parent == path {
		parent = ""
	}
	return dirListing{Path: path, Parent: parent, Dirs: dirs, Truncated: truncated}, nil
}

// readSubdirs 分批读目录，读满 maxScanEntries 项就停（第二个返回值为 true）。
func readSubdirs(path string) ([]dirEntry, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	dirs := []dirEntry{}
	scanned := 0
	for {
		batch, err := f.ReadDir(512)
		for _, e := range batch {
			full := filepath.Join(path, e.Name())
			// 不是合法 UTF-8 的名字（Linux 上可能有）过不了 JSON 往返，列出来也选不中
			if hiddenDir(e.Name()) || !utf8.ValidString(e.Name()) || !entryIsDir(e, full) {
				continue
			}
			dirs = append(dirs, dirEntry{Name: e.Name(), Path: full})
		}
		scanned += len(batch)
		switch {
		case errors.Is(err, io.EOF):
			return dirs, false, nil
		case err != nil:
			return nil, false, err
		case scanned >= maxScanEntries:
			return dirs, true, nil
		}
	}
}

// deviceNamespace 认出 Windows 的设备 / 原始路径（\\.\、\\?\）。这类路径指向设备而不是文件夹，
// 一律不浏览；普通的网络共享 \\nas\share 照常放行（NAS 上放番很常见）。
func deviceNamespace(path string) bool {
	return strings.HasPrefix(path, `\\.\`) || strings.HasPrefix(path, `\\?\`)
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

// placeDirs 是「常用位置」：主目录及其下的影片/视频、下载、桌面，再加挂载着的盘。
// 主目录下的位置不存在就不列；挂载盘只看目录项本身，不去 stat 它 —— 离线的网络盘一 stat 就卡住。
func placeDirs(goos, home string) []dirEntry {
	out := []dirEntry{}
	if home != "" && isDir(home) {
		out = append(out, dirEntry{Name: "主目录", Path: home})
		for _, p := range homePlaces(goos) {
			if path := filepath.Join(home, p.dir); isDir(path) {
				out = append(out, dirEntry{Name: p.label, Path: path})
			}
		}
	}
	return append(out, volumeDirs(goos)...)
}

func homePlaces(goos string) []place {
	if goos == "darwin" {
		return []place{{"影片", "Movies"}, {"下载", "Downloads"}, {"桌面", "Desktop"}}
	}
	return []place{{"视频", "Videos"}, {"下载", "Downloads"}, {"桌面", "Desktop"}}
}

// volumeDirs 列出挂载着的盘。macOS 的 /Volumes 里系统盘是一个指回 / 的符号链接，跳过它；
// Windows 用系统给的盘符位掩码，不逐个探测（映射了却离线的网络盘一探测就卡住）。
func volumeDirs(goos string) []dirEntry {
	if goos == "windows" {
		var out []dirEntry
		for _, root := range logicalDrives() {
			out = append(out, dirEntry{Name: root, Path: root})
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
			if hiddenDir(e.Name()) || !e.IsDir() || !utf8.ValidString(e.Name()) {
				continue
			}
			out = append(out, dirEntry{Name: e.Name(), Path: filepath.Join(root, e.Name())})
		}
	}
	return out
}
