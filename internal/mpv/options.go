package mpv

import (
	"fmt"
	"strings"
)

// 起播选项：一份清单，两种渲染。
//
//   - 命令行 mpv（Linux、浏览器模式、Windows 桌面窗口里的 mpv.exe --wid，以及今天所有平台）吃 argv：
//     LaunchSpec.Argv，与改造前的 buildArgs 逐字节一致（options_golden_test.go 钉着）。
//   - macOS 桌面窗口里的进程内 libmpv 逐条设选项：LaunchSpec.Libmpv。两边写法不同的只有列表 ——
//     argv 逐项追加（--glsl-shaders-append=…），而 mpv_set_option_string 不认 -append / -add 这类
//     后缀（原型实测 script-opts-append、glsl-shaders-append、http-header-fields-add 都返回
//     -5 option not found），只能整表设成 MPV_FORMAT_NODE。整表设也正好免去转义：列表项里的逗号、
//     冒号、分号、等号都原样保留。
//
// 这个包不碰 cgo，只给出数据；真正调 libmpv 的是桌面窗口那一层。

// OptionKind 是一条选项的值的形状，决定两种渲染各怎么写。
type OptionKind int

const (
	// OptionString 是单值：argv 写 --name=value；libmpv 用 mpv_set_option_string(name, value)。
	OptionString OptionKind = iota
	// OptionFlag 是开关，Value 为 "yes" / "no"：argv 写 --name / --no-name；libmpv 同 OptionString。
	OptionFlag
	// OptionList 是字符串列表（glsl-shaders、http-header-fields、sub-files）：argv 逐项追加；
	// libmpv 整表设成 MPV_FORMAT_NODE_ARRAY，每项一个 MPV_FORMAT_STRING。
	OptionList
	// OptionMap 是键值列表（script-opts），每项 "key=value"：argv 逐项追加；libmpv 整表设成
	// MPV_FORMAT_NODE_MAP，值是 MPV_FORMAT_STRING。只按第一个 = 切，值里还可以有 =。
	OptionMap
)

// Option 是一条 mpv 选项。Name 是 libmpv 认的名字（不带 "--"，也不带 -append 一类后缀）。
type Option struct {
	Name  string
	Kind  OptionKind
	Value string   // OptionString / OptionFlag
	Items []string // OptionList：每项原样；OptionMap：每项 "key=value"
	// PerFile 标出只属于第一个文件的选项（起播位置、标题、外挂字幕、请求头）：换文件时必须换掉，
	// 见 LaunchSpec 的说明。其余是整个 mpv 句柄（= 一次播放会话）共用的。
	PerFile bool

	// cliItem 是列表在 argv 里逐项追加时的写法（"glsl-shaders-append"、"sub-file"……）。
	cliItem string
	// afterRender：libmpv 下要等渲染上下文建好才能设（见 Libmpv）。
	afterRender bool
}

func stringOption(name, value string) Option {
	return Option{Name: name, Kind: OptionString, Value: value}
}

func flagOption(name string, on bool) Option {
	return Option{Name: name, Kind: OptionFlag, Value: yesNo(on)}
}

// listOption / mapOption 拷一份 items：清单是导出的，别让拿到它的人改到调用方的切片。
func listOption(name, cliItem string, items []string) Option {
	return Option{Name: name, Kind: OptionList, Items: append([]string(nil), items...), cliItem: cliItem}
}

func mapOption(name, cliItem string, items []string) Option {
	return Option{Name: name, Kind: OptionMap, Items: append([]string(nil), items...), cliItem: cliItem}
}

func (o Option) perFile() Option {
	o.PerFile = true
	return o
}

// afterRenderContext 标出 libmpv 下要等渲染上下文建好才能设的选项（见 LibmpvSpec.BeforeLoad）。
func (o Option) afterRenderContext() Option {
	o.afterRender = true
	return o
}

func (o Option) appendArgv(args []string) []string {
	switch o.Kind {
	case OptionFlag:
		if o.Value == "no" {
			return append(args, "--no-"+o.Name)
		}
		return append(args, "--"+o.Name)
	case OptionList, OptionMap:
		for _, item := range o.Items {
			args = append(args, "--"+o.cliItem+"="+item)
		}
		return args
	default:
		return append(args, "--"+o.Name+"="+o.Value)
	}
}

// LaunchSpec 是一次起播的全部选项（NewLaunchSpec 从 LaunchOptions 生成）。
//
// 全局与这个文件的划分：将来的嵌入式启动器建一次 mpv 句柄（只带全局选项），再把第一个文件
// 交给它（同 Player.LoadNext 换下一集）。PerFile 的那几条 —— start、title / force-media-title、
// sub-files、http-header-fields —— 在 mpv 里也是全局选项，但只对这一个文件有意义：起播位置、
// 标题、对白字幕都是这一集的，请求头（签名、Cookie）是这一条流的。换文件时必须换掉，否则会
// 带到下一个文件上；LoadNext 复位前三样，请求头不复位（带请求头的在线来源没有上一集 / 下一集）。
type LaunchSpec struct {
	// Options 按 argv 的顺序排；全局与 PerFile 交错（argv 的顺序是钉死的）。
	Options []Option
	// Media 是第一个文件；空串 = 不带文件启动（argv 里是 --idle=yes）。
	Media string
}

// Argv 渲染成命令行 mpv 的参数。媒体路径永远在最后、跟在 "--" 后面（防御以 "-" 开头的文件名）。
func (s LaunchSpec) Argv() []string {
	args := make([]string, 0, len(s.Options)+2)
	for _, o := range s.Options {
		args = o.appendArgv(args)
	}
	if s.Media != "" {
		return append(args, "--", s.Media)
	}
	return append(args, "--idle=yes")
}

// KeyValue 是键值列表里的一项。
type KeyValue struct {
	Key, Value string
}

// LibmpvOption 是 libmpv 那一侧的一条选项。Kind 为 OptionString / OptionFlag 时值在 String；
// OptionList 时在 List（MPV_FORMAT_NODE_ARRAY）；OptionMap 时在 Map（MPV_FORMAT_NODE_MAP，保序）。
type LibmpvOption struct {
	Name   string
	Kind   OptionKind
	String string
	List   []string
	Map    []KeyValue
}

// Value 是这条选项的值的 Go 形态：string、[]string 或 map[string]string。经 JSON IPC 设属性
// （set_property）时直接用它 —— JSON 数组 / 对象在 mpv 那边就是 NODE_ARRAY / NODE_MAP，
// 与 mpv_set_option(MPV_FORMAT_NODE) 走的是同一条转换。
func (o LibmpvOption) Value() any {
	switch o.Kind {
	case OptionList:
		return append([]string{}, o.List...)
	case OptionMap:
		m := make(map[string]string, len(o.Map))
		for _, kv := range o.Map {
			m[kv.Key] = kv.Value
		}
		return m
	default:
		return o.String
	}
}

// LibmpvSpec 是 libmpv 的起播顺序：mpv_create → 设 Init → mpv_initialize → 建渲染上下文 →
// 连 IPC（mpv.Attach）→ 以属性设 BeforeLoad → loadfile Media。
type LibmpvSpec struct {
	// Init 在 mpv_initialize 之前设：全局选项，外加让 libmpv 与命令行 mpv 行为一致的几条
	// （libmpvProfile）。
	//
	// 优先级与命令行相反：mpv.conf 在 mpv_initialize 里才解析，会盖过这里先设的值；命令行 mpv 则是
	// argv 盖过 mpv.conf。所以界面配置（ui/mpv.conf）里不能出现与 Init 同名而取值不同的选项，
	// 否则那一项只在 macOS 上悄悄变样（TestUIConfigDoesNotOverrideLaunchOptions 守着）。
	Init []LibmpvOption
	// BeforeLoad 在渲染上下文建好之后、第一个 loadfile 之前以属性设：PerFile 的那几条，以及
	// force-window —— 它在渲染上下文建好之前设会让 VO 线程抢先初始化，而 vo_libmpv 没有渲染
	// 上下文就失败（IINA PlayerCore.swift 同样推迟到开文件时才设）。
	BeforeLoad []LibmpvOption
	// Media 是第一个文件（loadfile … replace）；空串 = 一直空闲。
	Media string
}

// libmpvProfile 抵消 libmpv 自带的默认值（mpv etc/builtin.conf 的 [libmpv] 一段：config=no、
// input-default-bindings=no、osc=no、idle=yes……），让同一份清单在两边意思相同：
//   - config=yes：否则 --config-dir 指过去的 mpv.conf、input.conf、界面脚本一个都不加载。清单里
//     自己带了 config（NoConfig）就听清单的；
//   - input-default-bindings=yes：nagare 的 input.conf 只写了改动的键，其余靠 mpv 默认；
//   - osc=yes：命令行 mpv 默认就有。nagare 的界面加载后会把它关掉；界面配置没写出来（NoConfig）时
//     它是唯一的控制条。
var libmpvProfile = []Option{
	flagOption("config", true),
	flagOption("input-default-bindings", true),
	flagOption("osc", true),
}

// Libmpv 渲染成 libmpv 的形态（见 LibmpvSpec）。
//
// 与 argv 的差别都是 libmpv 的起播方式决定的：列表整表设；分 Init / BeforeLoad 两段设；不带文件时
// idle=yes、带文件时 idle=once（句柄先空闲着建好，loadfile 之后播完即退出 —— 与命令行 mpv 播完关窗
// 一致，会话照旧以 mpv 退出收尾）；libmpvProfile。值里不能有 NUL（C 字符串会被截断，argv 那边 exec
// 也会拒绝）。
func (s LaunchSpec) Libmpv() (LibmpvSpec, error) {
	if strings.IndexByte(s.Media, 0) >= 0 {
		return LibmpvSpec{}, fmt.Errorf("媒体路径里有 NUL 字符，交不给 mpv")
	}
	out := LibmpvSpec{Media: s.Media}
	present := map[string]bool{}
	for _, o := range s.Options {
		present[o.Name] = true
	}
	for _, o := range libmpvProfile {
		if !present[o.Name] {
			out.Init = append(out.Init, o.libmpv())
		}
	}
	for _, o := range s.Options {
		if err := o.validate(); err != nil {
			return LibmpvSpec{}, err
		}
		lo := o.libmpv()
		if o.PerFile || o.afterRender {
			out.BeforeLoad = append(out.BeforeLoad, lo)
		} else {
			out.Init = append(out.Init, lo)
		}
	}
	idle := "yes"
	if s.Media != "" {
		idle = "once"
	}
	out.Init = append(out.Init, stringOption("idle", idle).libmpv())
	return out, nil
}

func (o Option) libmpv() LibmpvOption {
	lo := LibmpvOption{Name: o.Name, Kind: o.Kind}
	switch o.Kind {
	case OptionList:
		lo.List = append([]string{}, o.Items...)
	case OptionMap:
		lo.Map = keyValues(o.Items)
	default:
		lo.String = o.Value
	}
	return lo
}

// keyValues 把 "key=value" 逐项切开。同一个键出现多次时与 argv 逐项 -append 的结果一致：旧的那项
// 去掉、新的排到最后（mpv 0.41 实测 a=1、b=2、a=3 得到 b=2,a=3）。调用前已经 validate 过。
func keyValues(items []string) []KeyValue {
	out := make([]KeyValue, 0, len(items))
	for _, item := range items {
		key, value, _ := strings.Cut(item, "=")
		kept := out[:0:0]
		for _, kv := range out {
			if kv.Key != key {
				kept = append(kept, kv)
			}
		}
		out = append(kept, KeyValue{Key: key, Value: value})
	}
	return out
}

func (o Option) validate() error {
	values := append([]string{o.Name, o.Value}, o.Items...)
	for _, v := range values {
		if strings.IndexByte(v, 0) >= 0 {
			return fmt.Errorf("mpv 选项 %s 的值里有 NUL 字符", o.Name)
		}
	}
	if o.Kind != OptionMap {
		return nil
	}
	for _, item := range o.Items {
		if key, _, ok := strings.Cut(item, "="); !ok || key == "" {
			return fmt.Errorf("mpv 选项 %s 的一项不是 key=value 的形式", o.Name)
		}
	}
	return nil
}

func yesNo(on bool) string {
	if on {
		return "yes"
	}
	return "no"
}
