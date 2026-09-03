// 扫描丢弃的可见化：哪些东西没进库、为什么、用户能做什么。
//
// 为什么不走 internal/errors：那个包的注释自己写着「成功返回空结果类失败
// 不走 error 通道，由各业务层显式建模」——扫描丢弃正是这一类。仓内对
// 「可见的非错误降级」的活约定是同时给稳定码与中文文案
// （见 player.DanmakuInfo 的 {State,Reason}、rules 的健康四态）。
//
// 为什么码与文案都要：只给中文的话前端没法分组、计数，测试里也只能断言
// 中文串——本仓已经因为断言中文串把一句错话钉住过一次。
package library

// DropReason 是扫描丢弃的稳定分类码。前端按它分组，测试按它断言。
type DropReason string

const (
	// DropSymlink：符号链接一律不跟随（见 walkEntries 里的红线注释）。
	DropSymlink DropReason = "symlink"
	// DropTooSmall：视频小于 minVideoSize。
	DropTooSmall DropReason = "too-small"
	// DropStatFailed：拿不到文件元信息（权限、扫描中途被删）。
	DropStatFailed DropReason = "stat-failed"
	// DropTooDeep：超过 maxScanDepth 的子目录整棵不扫。按【目录】计一笔。
	DropTooDeep DropReason = "too-deep"
	// DropUnreadableDir：子目录读不了。同样按目录计一笔。
	DropUnreadableDir DropReason = "unreadable-dir"
)

// 刻意【不是】丢弃原因的两类，别再往这里加：
//   - 噪声名（._* / .DS_Store / Thumbs.db / desktop.ini）
//   - 非视频非字幕扩展名（notes.txt / cover.jpg）
//
// 它们本来就不是用户要的东西，报出来只会训练用户忽略这条横幅，
// 而横幅一旦被忽略，真正的软链丢弃也就跟着被忽略了。
//
// 另外没有 bundle-unpicked 这个码：ExFAT 包目录内部被跳过的条目，
// 跳过理由就是上面那三条文件级原因之一，报「包没挑中」是在讲机制，
// 不是讲用户能动手改的原因。

// maxDropSamples 是每个原因最多留几条路径。全量不留：一棵两千文件的软链树
// 只需要证明「是这一类」，前几条就够；剩下的只让内存和界面一起变长。
const maxDropSamples = 5

// dropCopy 是每个原因的用户文案与恢复动作。
//
// UserMsg 会被界面接在「N 项：」后面，所以写成能独立成句的短语，
// 别带主语也别以「是」开头（「1 项：是目录层级过深」读不通）。
// Recovery 必须是用户【真能做的一步操作】，不是对现象的复述。
var dropCopy = map[DropReason]struct{ UserMsg, Recovery string }{
	DropSymlink: {
		UserMsg:  "符号链接，nagare 不跟随",
		Recovery: "把链接指向的真实路径直接添加进库，或把软链换成硬链接",
	},
	DropTooDeep: {
		UserMsg:  "目录层级超过 3 层，整棵子树没有扫描",
		Recovery: "把这一层目录本身添加成库目录",
	},
	DropTooSmall: {
		UserMsg:  "视频文件小于 1MB",
		Recovery: "多半是没下完的片或采样文件；下完之后重新扫描",
	},
	DropUnreadableDir: {
		UserMsg:  "目录读不了",
		Recovery: "检查这个目录的读取权限",
	},
	DropStatFailed: {
		UserMsg:  "读不到文件信息",
		Recovery: "检查这个文件的权限，或确认它没有正在被移动/删除",
	},
}

// dropReasonOrder 固定输出顺序：越靠前越「用户改得动」。
// 用固定序而不是 map 迭代序，产出才是确定的（与扫描结果同一条要求）。
var dropReasonOrder = []DropReason{
	DropSymlink, DropTooDeep, DropTooSmall, DropUnreadableDir, DropStatFailed,
}

// DropGroup 是同一原因的丢弃汇总。
type DropGroup struct {
	Reason   DropReason
	Count    int
	UserMsg  string
	Recovery string
	// Samples 是相对库根的路径，最多 maxDropSamples 条。
	Samples []string
}

// DropSummary 是一次扫描丢掉的全部东西，按 dropReasonOrder 排列。
type DropSummary struct {
	Total  int
	Groups []DropGroup
}

// dropCollector 边扫边聚合。内存 O(原因数 × maxDropSamples)，与库大小无关——
// 收全量再聚合是白做的功：两千个文件的软链树要先造两千个结构体，
// 最后只留五个计数和几条路径。
type dropCollector struct {
	counts  map[DropReason]int
	samples map[DropReason][]string
}

func newDropCollector() *dropCollector {
	return &dropCollector{
		counts:  map[DropReason]int{},
		samples: map[DropReason][]string{},
	}
}

// add 记一条丢弃。relPath 为空只计数。
func (c *dropCollector) add(reason DropReason, relPath string) {
	c.counts[reason]++
	if relPath != "" && len(c.samples[reason]) < maxDropSamples {
		c.samples[reason] = append(c.samples[reason], relPath)
	}
}

// merge 把另一个收集器并进来。
//
// 存在的理由只有一个：ExFAT 包目录。包目录里被跳过的条目，只有在这个包
// 【被采纳】时才真的丢了；没采纳会走普通递归把每个条目重新判断一遍，
// 那时再记就是重复计数（用户会看到「3 项被跳过」而那 3 项其实都进库了）。
func (c *dropCollector) merge(other *dropCollector) {
	for reason, n := range other.counts {
		c.counts[reason] += n
	}
	for reason, paths := range other.samples {
		for _, p := range paths {
			if len(c.samples[reason]) >= maxDropSamples {
				break
			}
			c.samples[reason] = append(c.samples[reason], p)
		}
	}
}

func (c *dropCollector) summary() DropSummary {
	out := DropSummary{}
	for _, reason := range dropReasonOrder {
		n := c.counts[reason]
		if n == 0 {
			continue
		}
		text := dropCopy[reason]
		out.Total += n
		out.Groups = append(out.Groups, DropGroup{
			Reason:   reason,
			Count:    n,
			UserMsg:  text.UserMsg,
			Recovery: text.Recovery,
			Samples:  c.samples[reason],
		})
	}
	return out
}
