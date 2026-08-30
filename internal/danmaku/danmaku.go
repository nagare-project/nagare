// Package danmaku 把 dandanplay 格式的弹幕评论转换成 ASS 字幕文本，
// 供 mpv 以外挂字幕轨（--sub-file / sub-reload）方式加载渲染。
//
// 三条设计约束：
//   - 确定性：相同输入与选项 → 逐字节相同输出（黄金文件测试依赖）。
//     内部只用切片与稳定排序，绝不让 map 迭代序泄漏进输出。
//   - 不静默：坏评论逐条跳过并如实计入 Stats，绝不让整体失败，也绝不悄悄丢数。
//   - 不盖字幕：垂直方向只用 DisplayRatio 比例的区域排轨，
//     底部留白是给正片内嵌字幕的，弹幕不得越入。
package danmaku

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Comment 是 dandanplay v2 API 返回的单条弹幕评论。
type Comment struct {
	CID  int64  `json:"cid"`
	P    string `json:"p"` // "time,mode,color[,uid...]" 逗号分隔，第 4 段起忽略
	Text string `json:"m"`
}

// Options 控制 ASS 生成的呈现参数。零值/非法值一律回落默认值，
// 因此 Options{} 就是推荐配置。
type Options struct {
	FontName       string  // 字体名，默认 "PingFang SC"（libass 找不到会自动回落，无害）
	FontSize       int     // 字号（像素，相对 1920×1080 画布），默认 48
	ScrollDuration float64 // 滚动弹幕跨屏历时（秒），默认 12
	StillDuration  float64 // 顶部/底部固定弹幕停留时长（秒），默认 5
	Opacity        float64 // 弹幕不透明度 0..1，默认 0.75
	DisplayRatio   float64 // 垂直可用区域占比 0..1，默认 0.72；底部留白给内嵌字幕
}

// fontNameSanitizer 清掉会破坏 Style 行逗号分隔格式的字符。
var fontNameSanitizer = strings.NewReplacer(",", " ", "\r", " ", "\n", " ")

// withDefaults 返回填充默认值后的副本。非法值（≤0、超上限）同样回落，
// 保证后续几何计算不会除零、不会算出画面外坐标。
func (o Options) withDefaults() Options {
	if strings.TrimSpace(o.FontName) == "" {
		o.FontName = "PingFang SC"
	}
	o.FontName = fontNameSanitizer.Replace(o.FontName)
	if o.FontSize <= 0 {
		o.FontSize = 48
	}
	if o.ScrollDuration <= 0 {
		o.ScrollDuration = 12
	}
	if o.StillDuration <= 0 {
		o.StillDuration = 5
	}
	if o.Opacity <= 0 {
		o.Opacity = 0.75
	}
	if o.Opacity > 1 {
		o.Opacity = 1
	}
	if o.DisplayRatio <= 0 {
		o.DisplayRatio = 0.72
	}
	if o.DisplayRatio > 1 {
		o.DisplayRatio = 1
	}
	return o
}

// Stats 汇总一次转换的处理结果。恒有 Total = Converted + Skipped。
type Stats struct {
	Total      int // 输入评论总数
	Converted  int // 成功生成事件行的条数
	Skipped    int // 坏评论（p 非法/负时间）或清洗后为空而跳过的条数
	Overlapped int // 无空闲轨道被强制放置（允许视觉重叠）的条数，含于 Converted
}

// Convert 把评论列表转成完整 ASS 文本。
// 坏评论逐条跳过并计入 Stats.Skipped，不让整体失败；Stats 如实反映每条去向。
// 事件按出现时间升序输出，同一时刻保持输入顺序——输出是确定性的。
func Convert(comments []Comment, opts Options) (string, Stats) {
	o := opts.withDefaults()
	stats := Stats{Total: len(comments)}

	items := make([]parsed, 0, len(comments))
	for _, c := range comments {
		it, ok := parseComment(c)
		if !ok {
			stats.Skipped++
			continue
		}
		items = append(items, it)
	}

	// 稳定排序 + 只比时间：时间相同自动保持输入序。排轨算法要求时间单调不减。
	sort.SliceStable(items, func(i, j int) bool { return items[i].start < items[j].start })

	events, overlapped := layout(items, o)
	stats.Converted = len(events)
	stats.Overlapped = overlapped

	var b strings.Builder
	writeHeader(&b, o)
	for _, ev := range events {
		writeDialogue(&b, ev, o)
	}
	return b.String(), stats
}

// WriteFile 生成 ASS 并原子落盘：先写同目录隐藏临时文件，再 rename 到 path，
// mpv 经 --sub-file / sub-reload 读取时不可能看到半截文件。
// 父目录不存在时自动创建（0755）。失败路径上的临时文件会被清理，不留残渣。
func WriteFile(path string, comments []Comment, opts Options) (Stats, error) {
	content, stats := Convert(comments, opts)

	dir := filepath.Dir(path)
	// 0700：运行时目录里的东西只给本人看（与 config/store 同一条权限约定）。
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return stats, fmt.Errorf("danmaku: create parent dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".danmaku-*.ass.tmp")
	if err != nil {
		return stats, fmt.Errorf("danmaku: create temp file: %w", err)
	}
	tmpName := tmp.Name()
	// 失败路径统一走 discard：尽力回收临时文件；主错误原样上抛，不被回收动作遮蔽。
	discard := func(primary error) (Stats, error) {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return stats, primary
	}
	if _, err := tmp.WriteString(content); err != nil {
		return discard(fmt.Errorf("danmaku: write temp file: %w", err))
	}
	if err := tmp.Sync(); err != nil {
		return discard(fmt.Errorf("danmaku: sync temp file: %w", err))
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return stats, fmt.Errorf("danmaku: close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return stats, fmt.Errorf("danmaku: rename into place: %w", err)
	}
	return stats, nil
}
