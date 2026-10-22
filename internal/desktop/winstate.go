package desktop

// mpv 有父窗口（Windows 的 --wid）时不管全屏、最大化、最小化、置顶（w32_common.c 里这几条在 w32->parent
// 时直接返回），Lua 界面照旧 cycle fullscreen、t 键 cycle ontop —— 属性变了、窗口不动。宿主经 IPC 观察这几个
// 属性、落到自己的顶层窗口上；用户在窗口上的操作（标题栏的最大化、Win+↑）再写回去，界面的图标才对得上。
//
// 这里只有规则本身（纯函数、各平台都能测），真正动窗口的是 window_windows.go。

// hostWindow 是宿主顶层窗口此刻的状态。
type hostWindow struct {
	Fullscreen bool
	Maximized  bool
	Minimized  bool
	Topmost    bool
}

// winAction 是宿主要对顶层窗口做的事。
type winAction int

const (
	winEnterFullscreen winAction = iota + 1
	winExitFullscreen
	winMaximize
	winMinimize
	winTopmostOn
	winTopmostOff
)

// mpvWrite 是要写回 mpv 的一个开关属性。
type mpvWrite struct {
	Name  string
	Value bool
}

// windowSync 记着 mpv 那边最近一次的值，只在真的不一致时动手，免得来回打乒乓。
type windowSync struct {
	mpv map[string]bool // mpv 那边最近一次报来 / 写过去的值
}

func newWindowSync() *windowSync {
	return &windowSync{mpv: map[string]bool{}}
}

// fromMpv 处理 mpv 报来的一次属性变化：返回宿主要对窗口做的事，以及要写回 mpv 的值。
func (s *windowSync) fromMpv(name string, v bool, host hostWindow) ([]winAction, []mpvWrite) {
	old, known := s.mpv[name]
	s.mpv[name] = v
	if known && old == v {
		return nil, nil
	}
	switch name {
	case "fullscreen":
		if v != host.Fullscreen {
			return []winAction{pick(v, winEnterFullscreen, winExitFullscreen)}, nil
		}
	case "ontop":
		if v != host.Topmost {
			return []winAction{pick(v, winTopmostOn, winTopmostOff)}, nil
		}
	case "window-minimized":
		// 一次性的请求（Lua 的最小化按钮在嵌入时不画，但 set window-minimized yes 仍可能来）：
		// 最小化宿主，再把 mpv 的值放回假，下一次还能再请求
		if v {
			s.mpv[name] = false
			return []winAction{winMinimize}, []mpvWrite{{name, false}}
		}
	case "window-maximized":
		// 只认「要最大化」：mpv 的子窗口每收到一次 WM_SIZE（SIZE_RESTORED）就把自己的 window-maximized 改成假
		// （w32_common.c 的 WM_SIZE 分支），宿主每改一次子窗口大小它就报一次假 —— 那不是用户的意思，照着还原
		// 就会把刚最大化的窗口又弹回去。有父窗口时 mpv 自己从不报真，报真一定是有人请求了（cycle / set）。
		if v && !host.Maximized && !host.Fullscreen {
			return []winAction{winMaximize}, nil
		}
	}
	return nil, nil
}

// fromHost 在宿主窗口的状态变了之后（用户点了标题栏、Win+↑、宿主自己执行了上面的动作）给出要写回 mpv 的值。
// 只写全屏、最大化、置顶；最小化不写（看不见的时候界面画什么都无所谓，而 mpv 会把它当成一次请求）。
func (s *windowSync) fromHost(host hostWindow) []mpvWrite {
	var out []mpvWrite
	for _, w := range []mpvWrite{
		{"fullscreen", host.Fullscreen},
		{"window-maximized", host.Maximized && !host.Fullscreen},
		{"ontop", host.Topmost},
	} {
		if old, known := s.mpv[w.Name]; known && old == w.Value {
			continue
		}
		s.mpv[w.Name] = w.Value
		out = append(out, w)
	}
	return out
}

func pick(v bool, yes, no winAction) winAction {
	if v {
		return yes
	}
	return no
}
