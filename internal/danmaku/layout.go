package danmaku

import "math"

// event 是一条完成排轨、可直接渲染成 Dialogue 行的弹幕事件。
type event struct {
	start float64 // 出现时间（秒）
	end   float64 // 消失时间（秒）
	mode  int     // modeScroll / modeBottom / modeTop
	color int     // 0xRRGGBB
	text  string  // 已清洗正文
	width int     // 估算像素宽，滚动模式的 \move 终点用
	y     int     // 纵坐标：滚动/顶部为文本上缘（\an7、\an8），底部为文本下缘（\an2）
}

// geometry 集中存放排轨需要的几何参数，全部由 Options 推导。
type geometry struct {
	// lineHeight 轨道行高 = round(FontSize × 1.2)。
	lineHeight int
	// availableH 弹幕可用垂直区域 = floor(PlayResY × DisplayRatio)。
	// 其下的空带留给正片内嵌字幕，弹幕不得越入。
	availableH int
	// trackCount 轨道数 = availableH / lineHeight，向下取整，至少 1（保底防除零）。
	trackCount int
}

func newGeometry(o Options) geometry {
	lh := int(math.Round(float64(o.FontSize) * 1.2))
	if lh < 1 {
		lh = 1
	}
	avail := int(float64(playResY) * o.DisplayRatio)
	n := avail / lh
	if n < 1 {
		n = 1
	}
	return geometry{lineHeight: lh, availableH: avail, trackCount: n}
}

// layout 对已按（时间升序、同刻按输入序）排好的弹幕逐条排轨，
// 返回同顺序的事件列表和强制放置（允许重叠）的条数。
// 滚动、顶部、底部三类各用独立的轨道池，互不挤占。
func layout(items []parsed, o Options) ([]event, int) {
	geo := newGeometry(o)
	scroll := newScrollTracks(geo.trackCount)
	top := newStillTracks(geo.trackCount)
	bottom := newStillTracks(geo.trackCount)

	events := make([]event, 0, len(items))
	overlapped := 0
	for _, it := range items {
		w := textWidth(it.text, o.FontSize)
		ev := event{start: it.start, mode: it.mode, color: it.color, text: it.text, width: w}
		var track int
		var forced bool
		switch it.mode {
		case modeTop:
			track, forced = top.place(it.start, o.StillDuration)
			// \an8 锚点在文本上缘：顶部第 i 条轨道的上缘 = i × 行高（自上而下排）。
			ev.y = track * geo.lineHeight
			ev.end = it.start + o.StillDuration
		case modeBottom:
			track, forced = bottom.place(it.start, o.StillDuration)
			// \an2 锚点在文本下缘：底部 0 号轨道的下缘贴住可用区域底边 availableH
			//（再往下是内嵌字幕保留区），自下而上每轨抬高一个行高。
			ev.y = geo.availableH - track*geo.lineHeight
			ev.end = it.start + o.StillDuration
		default:
			track, forced = scroll.place(it.start, w, o.ScrollDuration)
			ev.y = track * geo.lineHeight
			ev.end = it.start + o.ScrollDuration
		}
		if forced {
			overlapped++
		}
		events = append(events, ev)
	}
	return events, overlapped
}

// scrollSlot 记录一条滚动轨道上最后放置的弹幕。
// 只记最后一条即可：弹幕按时间升序放入，正常放置满足的安全条件对更早的弹幕
// 有传递性；强制放置本就允许重叠，不再向前追溯。
type scrollSlot struct {
	used  bool
	start float64 // tA：最后一条的出现时间
	width int     // LA：最后一条的像素宽
}

type scrollTracks struct{ slots []scrollSlot }

func newScrollTracks(n int) *scrollTracks {
	return &scrollTracks{slots: make([]scrollSlot, n)}
}

// place 为出现于 tB、宽 widthB 的滚动弹幕选轨，返回（轨道号, 是否强制放置）。
//
// 记屏宽 W=playResX、跨屏历时 D。弹幕 X 的左端从 x=W 匀速移到 x=-LX，
// 速度 vX = (W+LX)/D。新弹幕 B 要安全接在轨道最后一条 A 之后，
// 须同时满足两个条件（danmaku2ass 同款判据）：
//
//  1. 入场不追尾：B 头部在右缘出现时，A 必须已完整入屏。
//     A 完整入屏需行进自身宽度 LA，历时 LA/vA = D·LA/(W+LA)，
//     即 tB ≥ tA + D·LA/(W+LA)。
//  2. 离场不追尾：B 比 A 宽（更快）时可能在屏内追上。B 头部抵达左缘于
//     tB + W/vB，A 尾部离开左缘于 tA + D；要求前者不早于后者，
//     化简得 tB ≥ tA + D·LB/(W+LB)。
//
// 两式取 max 就是该轨道对 B 的空闲时刻 freeAt。匀速直线运动下两车间距单调变化，
// 校验入场、离场两个端点即可覆盖全程。
func (s *scrollTracks) place(tB float64, widthB int, d float64) (int, bool) {
	bestIdx, bestFree := 0, math.Inf(1)
	for i, slot := range s.slots {
		if !slot.used {
			s.slots[i] = scrollSlot{used: true, start: tB, width: widthB}
			return i, false
		}
		entry := d * float64(slot.width) / float64(playResX+slot.width) // 条件 1：前车完整入屏
		exit := d * float64(widthB) / float64(playResX+widthB)          // 条件 2：后车不先到左缘
		freeAt := slot.start + math.Max(entry, exit)
		if tB >= freeAt {
			s.slots[i] = scrollSlot{used: true, start: tB, width: widthB}
			return i, false
		}
		if freeAt < bestFree {
			bestIdx, bestFree = i, freeAt
		}
	}
	// 无空闲轨道：不丢弃，选最早空闲的轨道直接放（允许视觉重叠），由调用方计数。
	s.slots[bestIdx] = scrollSlot{used: true, start: tB, width: widthB}
	return bestIdx, true
}

// stillTracks 维护顶部/底部固定弹幕每条轨道的占用截止时刻（零值 0 表示从未占用）。
type stillTracks struct{ freeAt []float64 }

func newStillTracks(n int) *stillTracks {
	return &stillTracks{freeAt: make([]float64, n)}
}

// place 为固定弹幕选轨：一条弹幕占用其轨道 [tB, tB+still)。
// 从 0 号轨开始找第一条空闲的；全忙时选最早空闲者强制放置。
func (s *stillTracks) place(tB, still float64) (int, bool) {
	bestIdx, bestFree := 0, math.Inf(1)
	for i, free := range s.freeAt {
		if tB >= free {
			s.freeAt[i] = tB + still
			return i, false
		}
		if free < bestFree {
			bestIdx, bestFree = i, free
		}
	}
	s.freeAt[bestIdx] = tB + still
	return bestIdx, true
}

// textWidth 估算正文的渲染像素宽，用于滚动速度与 \move 终点计算。
// 逐 rune 简化判定（不引入完整 East Asian Width 数据表，够用即可）：
//   - ASCII 与 Unicode 半角形式区段 ≈ 0.5 × 字号；
//   - 其余（CJK、全角标点、Emoji 等）≈ 1.0 × 字号。
//
// 用整数“半宽单位”累加后一次性换算并向上取整，全程整数运算保证确定性。
func textWidth(text string, fontSize int) int {
	halfUnits := 0
	for _, r := range text {
		if isHalfWidthRune(r) {
			halfUnits++
		} else {
			halfUnits += 2
		}
	}
	return (halfUnits*fontSize + 1) / 2 // 等价 ceil(halfUnits × fontSize / 2)
}

// isHalfWidthRune 判定字符是否按半宽估算：ASCII，以及
// U+FF61..U+FFDC（半角片假名、半角谚文）、U+FFE8..U+FFEE（半角符号）。
func isHalfWidthRune(r rune) bool {
	if r < 0x80 {
		return true
	}
	return (r >= 0xFF61 && r <= 0xFFDC) || (r >= 0xFFE8 && r <= 0xFFEE)
}
