package mpv

import (
	"bufio"
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// 进度条缩略图（thumbfast，po5/thumbfast，原样内嵌在 ui/scripts/thumbfast.lua）在两种宿主下的差别：
//
//   - 命令行 mpv：配置目录里的脚本自动加载，thumbfast 自己读 display-hidpi-scale 放大缩略图。
//   - macOS 桌面窗口（进程内 libmpv，DesktopLibmpvSpec）：配置目录里的脚本不自动加载（thumbfast 在
//     macOS 上找不到 mpv 时会用 `ps -o comm=` 把「自己这个进程」当 mpv 拉起来 —— 那就是 nagare），
//     只在有命令行 mpv 可用时点名加载它（thumbfastCLI）；render API 下读不到 display-hidpi-scale，
//     缩略图尺寸由宿主按屏幕缩放算好，经 script-opts 传进去（ThumbfastScaledSize）。thumbfast.lua 本身不改。

// ThumbfastScript 是 thumbfast 在界面配置目录（PrepareUIConfig）里的相对路径，正斜杠分隔。
const ThumbfastScript = "scripts/thumbfast.lua"

// thumbfastConf 是 nagare 给 thumbfast 的设置文件（嵌在 ui/ 里，max_width / max_height 是逻辑像素）。
const thumbfastConf = "ui/script-opts/thumbfast.conf"

// thumbfastDefaultMax 是读不到设置文件时的兜底（与 thumbfast.conf 的值一致；测试钉着两者相等）。
const thumbfastDefaultMax = 192

var (
	thumbfastMaxOnce sync.Once
	thumbfastMaxW    = thumbfastDefaultMax
	thumbfastMaxH    = thumbfastDefaultMax
)

// ThumbfastMaxSize 返回 thumbfast.conf 里的缩略图最大尺寸（逻辑像素）。
func ThumbfastMaxSize() (w, h int) {
	thumbfastMaxOnce.Do(func() {
		content, err := uiFiles.ReadFile(thumbfastConf)
		if err != nil {
			return
		}
		if mw, mh, err := parseThumbfastMax(content); err == nil {
			thumbfastMaxW, thumbfastMaxH = mw, mh
		}
	})
	return thumbfastMaxW, thumbfastMaxH
}

// parseThumbfastMax 从 thumbfast 的设置文件里读 max_width / max_height（mpv 的 script-opts 文件格式：
// 每行 key=value，# 开头是注释）。两项都要有、都是正整数。
func parseThumbfastMax(content []byte) (w, h int, err error) {
	sc := bufio.NewScanner(bytes.NewReader(content))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "max_width":
			w, err = strconv.Atoi(strings.TrimSpace(value))
		case "max_height":
			h, err = strconv.Atoi(strings.TrimSpace(value))
		}
		if err != nil {
			return 0, 0, fmt.Errorf("thumbfast.conf 的 %s 不是整数：%w", strings.TrimSpace(key), err)
		}
	}
	if w <= 0 || h <= 0 {
		return 0, 0, fmt.Errorf("thumbfast.conf 缺少 max_width / max_height（读到 %d × %d）", w, h)
	}
	return w, h, nil
}

// ThumbfastScaledSize 返回按屏幕缩放放大后的 thumbfast-max_width / thumbfast-max_height
// （script-opts 的两项，key=value）。scale 不是正的有限数时按 1 算。
//
// 与 thumbfast 自己在命令行 mpv 里做的一样：最大尺寸 × display-hidpi-scale，四舍五入。
func ThumbfastScaledSize(scale float64) []string {
	if scale <= 0 || math.IsNaN(scale) || math.IsInf(scale, 0) {
		scale = 1
	}
	w, h := ThumbfastMaxSize()
	return []string{
		"thumbfast-max_width=" + strconv.Itoa(int(math.Round(float64(w)*scale))),
		"thumbfast-max_height=" + strconv.Itoa(int(math.Round(float64(h)*scale))),
	}
}

// ThumbfastMPVPath 返回 script-opts 里交给 thumbfast 的 mpv 路径（thumbfast-mpv_path，最后一条为准，
// 与 mpv 合并 script-opts 的规则一致）；没有就是空串。
func ThumbfastMPVPath(scriptOpts []string) string {
	path := ""
	for _, kv := range scriptOpts {
		if v, ok := strings.CutPrefix(kv, "thumbfast-mpv_path="); ok {
			path = v
		}
	}
	return path
}

// thumbfastCLI 从起播参数里取出交给 thumbfast 的命令行 mpv（player 在探测到命令行 mpv 时给
// thumbfast-mpv_path），确认它能用：绝对路径、是个普通文件、而且不是 nagare 自己（self 返回 nagare
// 自己的可执行文件，解析过符号链接）。不能用就返回空串和原因 —— 进程内 libmpv 就不加载 thumbfast。
func thumbfastCLI(scriptOpts []string, self func() (string, error)) (string, string) {
	path := ThumbfastMPVPath(scriptOpts)
	if path == "" {
		return "", "没有命令行 mpv"
	}
	if !filepath.IsAbs(path) {
		return "", "命令行 mpv 的路径不是绝对路径"
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", "命令行 mpv 不存在"
	}
	st, err := os.Stat(resolved)
	if err != nil || !st.Mode().IsRegular() {
		return "", "命令行 mpv 不是普通文件"
	}
	selfPath, err := self()
	if err != nil {
		return "", "取不到 nagare 自己的路径，没法确认 thumbfast 不会把 nagare 当 mpv 拉起来"
	}
	if selfSt, err := os.Stat(selfPath); err != nil || resolved == selfPath || os.SameFile(st, selfSt) {
		return "", "交给 thumbfast 的 mpv 就是 nagare 自己"
	}
	return path, ""
}

// ThumbfastScriptOpts 按 thumbfastCLI 的结论改写 script-opts（返回新切片，不动传进来的）：
// 能用就追加按屏幕缩放放大的缩略图尺寸（render API 下 thumbfast 读不到 display-hidpi-scale）；
// 不能用就去掉 thumbfast-mpv_path（thumbfast 不加载，这一项没人读）。
func ThumbfastScriptOpts(scriptOpts []string, usable bool, hidpi float64) []string {
	out := make([]string, 0, len(scriptOpts)+2)
	for _, kv := range scriptOpts {
		if !usable && strings.HasPrefix(kv, "thumbfast-mpv_path=") {
			continue
		}
		out = append(out, kv)
	}
	if usable {
		out = append(out, ThumbfastScaledSize(hidpi)...)
	}
	return out
}
