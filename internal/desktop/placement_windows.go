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
	if json.Unmarshal(b, &p) != nil || p.Right-p.Left < minWidth || p.Bottom-p.Top < minHeight {
		return savedPlacement{}, false
	}
	r := rect{p.Left, p.Top, p.Right, p.Bottom}
	if mon, _, _ := pMonitorFromRect.Call(uintptr(unsafe.Pointer(&r)), 0); mon == 0 { // MONITOR_DEFAULTTONULL
		return savedPlacement{}, false
	}
	return p, true
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
	if err := os.WriteFile(path, b, 0o600); err != nil {
		log.Printf("desktop: 记不下窗口位置：%v", err)
	}
}
