package desktop

// mpv 有父窗口（Windows 的 --wid）时不管全屏、最大化、最小化、置顶（w32_common.c 里这几条在 w32->parent
// 时直接返回），Lua 界面照旧 cycle fullscreen、t 键 cycle ontop —— 属性变了、窗口不动。宿主经 IPC 观察这几个
// 属性、落到自己的顶层窗口上；用户在窗口上的操作（标题栏的最大化、Win+↑）再写回去，界面的图标才对得上。
//
// mpv 的属性通知会合并：它报的是取通知那一刻的值，与上次报给这条连接的一样就不报。宿主写过去的值紧跟着另一次改动
// （mpv 的子窗口处理 WM_SIZE 时把 window-maximized 改成假、界面或脚本的一次请求）时，两下合在一起常常「没变」，
// 宿主一个通知都收不到 —— CI 冒烟实测：还原时宿主写假、紧接着请求最大化写真，宿主再也听不到那个真。所以宿主改完
// 窗口、mpv 的子窗口跟上之后，把这几个属性读回来再对一遍（reconcile），一直读到 mpv 把宿主写过去的值报回来为止
// （pending）：报回来之前它还没取过这次改动，之后的请求随时可能被合并掉；报回来之后再有改动它一定会报。
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
	mpv      map[string]bool // mpv 那边最近一次报来 / 写过去的值
	reported map[string]bool // mpv 最近一次真的报来的值（只有通知，不含宿主写的、读回来的）
	// touched：每个属性收到报告、或宿主写过去的次数。读回来的值只在读的这段时间里它没变时才算数（见 reconcile）
	touched map[string]int
}

func newWindowSync() *windowSync {
	return &windowSync{mpv: map[string]bool{}, reported: map[string]bool{}, touched: map[string]int{}}
}

// fromMpv 处理 mpv 报来的一次属性变化：返回宿主要对窗口做的事，以及要写回 mpv 的值。
func (s *windowSync) fromMpv(name string, v bool, host hostWindow) ([]winAction, []mpvWrite) {
	s.touched[name]++
	s.reported[name] = v
	if old, known := s.mpv[name]; known && old == v {
		return nil, nil
	}
	return s.decide(name, v, host)
}

// pending：有属性宿主认为 mpv 那边是一个值（写过去的、读回来的），mpv 最近报来的却是另一个 —— mpv 还没取过那次
// 改动，在它取之前再来一次改动（请求），两下可能合并成「没变」、不发通知。这时候只能接着读。
func (s *windowSync) pending(names []string) bool {
	for _, name := range names {
		v, known := s.mpv[name]
		if !known {
			continue
		}
		if r, ok := s.reported[name]; !ok || r != v {
			return true
		}
	}
	return false
}

// marks 是这几个属性此刻的 touched，读回来之前记下，交给 reconcile。
func (s *windowSync) marks(names []string) []int {
	out := make([]int, len(names))
	for i, name := range names {
		out[i] = s.touched[name]
	}
	return out
}

// reconcile 处理从 mpv 读回来的值（get_property，与宿主的写走同一条连接、按顺序执行，读到的是宿主写过之后的值）。
// 两个方向都会动：读到的是一次没报到的请求就照做（最大化、全屏、置顶），读到最大化被盖成假就写回真。
// 规则与 fromMpv 相同，只是不跳过「与记下的一样」：被合并掉的那次改动，记下的值看不出来。读的这段时间里这个属性
// 来过报告或宿主又写过（touched 与 mark 对不上），读到的可能已经过时，不算数 —— 照它动窗口会把用户刚做的撤回去。
func (s *windowSync) reconcile(name string, v bool, host hostWindow, mark int) ([]winAction, []mpvWrite) {
	if s.touched[name] != mark {
		return nil, nil
	}
	return s.decide(name, v, host)
}

// decide 是 mpv 那边的值 v 与宿主窗口不一致时该做的事。全屏、置顶、最大化的动作只在两边不一致时才有，执行之后两边
// 一致，重复调不会多做；window-minimized 是一次性的请求，读到真就最小化（所以不拿它来 reconcile）。
func (s *windowSync) decide(name string, v bool, host hostWindow) ([]winAction, []mpvWrite) {
	s.mpv[name] = v
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
			return []winAction{winMinimize}, s.written(mpvWrite{name, false})
		}
	case "window-maximized":
		// 只认「要最大化」：mpv 的子窗口每收到一次 WM_SIZE（SIZE_RESTORED）就把自己的 window-maximized 改成假
		// （w32_common.c 的 WM_SIZE 分支），宿主每改一次子窗口大小它就报一次假 —— 那不是用户的意思，照着还原
		// 就会把刚最大化的窗口又弹回去。有父窗口时 mpv 自己从不报真，报真一定是有人请求了（cycle / set）。
		if v && !host.Maximized && !host.Fullscreen {
			return []winAction{winMaximize}, nil
		}
		// 那个假盖掉了宿主写过去的真（子窗口的 WM_SIZE 比宿主的写晚到一步），宿主却还是最大化：再写一次真。
		// 有父窗口时 mpv 收到这个写什么都不做（w32_common.c 的 update_maximized_state 开头就返回），不会让子窗口
		// 改大小、不会再来一个假，不会来回打乒乓（CI 的记录里也是写一次、报一次真就停）。
		if !v && host.Maximized && !host.Fullscreen {
			return nil, s.written(mpvWrite{name, true})
		}
	}
	return nil, nil
}

// written 记下宿主要写过去的值（mpv 那边的值、touched），原样返回。
func (s *windowSync) written(ws ...mpvWrite) []mpvWrite {
	for _, w := range ws {
		s.mpv[w.Name] = w.Value
		s.touched[w.Name]++
	}
	return ws
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
		out = append(out, s.written(w)...)
	}
	return out
}

func pick(v bool, yes, no winAction) winAction {
	if v {
		return yes
	}
	return no
}
