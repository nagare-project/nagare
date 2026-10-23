//go:build windows

package desktop

// 窗口的位置大小记在 <配置目录>\desktop-window.json，下次打开放回原处（同 macOS 的 setFrameAutosaveName）。
// 记下的位置不在任何一块显示器上（拔了外接屏）就不用，改成主屏居中的默认尺寸。

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

const placementFile = "desktop-window.json"

type savedPlacement struct {
	Left, Top, Right, Bottom int32
	Maximized                bool
}

var pMonitorFromRect = user32.NewProc("MonitorFromRect")

func (a *app) placementPath() string {
	dir := a.options().DataDir
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, placementFile)
}

// restorePlacement 在窗口还藏着的时候放好位置（第一次 ShowWindow 才看得见）。
func (a *app) restorePlacement() {
	if p, ok := a.loadPlacement(); ok {
		wp := windowPlacement{
			Length:           uint32(unsafe.Sizeof(windowPlacement{})),
			ShowCmd:          swHide,
			RcNormalPosition: rect{p.Left, p.Top, p.Right, p.Bottom},
		}
		pSetWindowPlacement.Call(a.top, uintptr(unsafe.Pointer(&wp)))
		a.startMaximized = p.Maximized
		return
	}
	a.defaultPlacement()
}

func (a *app) loadPlacement() (savedPlacement, bool) {
	path := a.placementPath()
	if path == "" {
		return savedPlacement{}, false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return savedPlacement{}, false
	}
	var p savedPlacement
	if json.Unmarshal(b, &p) != nil {
		return savedPlacement{}, false
	}
	// 标题栏得在某块显示器上（拖得动、点得着关闭）：只露出一个角不算。记的是物理像素
	bar := rect{p.Left + titleBarMargin, p.Top, p.Right - titleBarMargin, p.Top + titleBarHeight}
	if bar.width() <= 0 {
		return savedPlacement{}, false
	}
	mon, _, _ := pMonitorFromRect.Call(uintptr(unsafe.Pointer(&bar)), 0) // MONITOR_DEFAULTTONULL
	if mon == 0 {
		return savedPlacement{}, false
	}
	// 最小尺寸按那块显示器的缩放换成物理像素再比（记的位置是物理像素，minWidth / minHeight 是逻辑像素）
	dpi := int32(monitorDPI(mon))
	if p.Right-p.Left < minWidth*dpi/96 || p.Bottom-p.Top < minHeight*dpi/96 {
		return savedPlacement{}, false
	}
	return p, true
}

// 判断「标题栏在不在显示器上」用的那一条：左右各让出一点（窗口边框），高度取一个标题栏。物理像素
const (
	titleBarMargin = 40
	titleBarHeight = 30
)

var pGetDpiForMonitor = windows.NewLazySystemDLL("shcore.dll").NewProc("GetDpiForMonitor")

// monitorDPI 是显示器的有效 DPI（读不到按 96）。
func monitorDPI(mon uintptr) uintptr {
	if pGetDpiForMonitor.Find() != nil {
		return 96
	}
	var x, y uint32
	if hr, _, _ := pGetDpiForMonitor.Call(mon, 0, uintptr(unsafe.Pointer(&x)), uintptr(unsafe.Pointer(&y))); hr != 0 || x == 0 { // MDT_EFFECTIVE_DPI
		return 96
	}
	return uintptr(x)
}

// defaultPlacement：所在显示器工作区的正中，默认尺寸（逻辑像素换成物理像素），放不下就缩到工作区的九成。
func (a *app) defaultPlacement() {
	mon, _, _ := pMonitorFromWindow.Call(a.top, monitorDefaultToNearest)
	mi := monitorInfo{CbSize: uint32(unsafe.Sizeof(monitorInfo{}))}
	if !boolCall(pGetMonitorInfoW, mon, uintptr(unsafe.Pointer(&mi))) {
		return
	}
	dpi := int32(windowDPI(a.top))
	work := mi.RcWork
	w := min(int32(defaultWidth)*dpi/96, work.width()*9/10)
	h := min(int32(defaultHeight)*dpi/96, work.height()*9/10)
	x := work.Left + (work.width()-w)/2
	y := work.Top + (work.height()-h)/2
	pSetWindowPos.Call(a.top, 0, uintptr(x), uintptr(y), uintptr(w), uintptr(h), swpNoZOrder|swpNoActivate)
}

// savePlacement 记下不全屏时的位置大小（全屏时记的是进全屏之前的那一份）。写不进去只记日志：下次居中打开而已。
func (a *app) savePlacement() {
	path := a.placementPath()
	if path == "" || a.top == 0 || !a.shownOnce {
		return
	}
	wp := a.fs.placement
	if !a.fs.on {
		wp = windowPlacement{Length: uint32(unsafe.Sizeof(windowPlacement{}))}
		if !boolCall(pGetWindowPlacement, a.top, uintptr(unsafe.Pointer(&wp))) {
			return
		}
	}
	r := wp.RcNormalPosition
	p := savedPlacement{Left: r.Left, Top: r.Top, Right: r.Right, Bottom: r.Bottom,
		Maximized: wp.ShowCmd == swMaximize || boolCall(pIsZoomed, a.top)}
	b, _ := json.Marshal(p)
	// 先写临时文件再改名：写到一半被结束（关机）时不留半截文件
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		log.Printf("desktop: 记不下窗口位置：%v", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		log.Printf("desktop: 记不下窗口位置：%v", err)
		_ = os.Remove(tmp)
	}
}
