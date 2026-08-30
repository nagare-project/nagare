// 文件名解析 —— 从 animego episodeParser.js 逐规则移植（决议 CQ2）。
//
// ⚠️ 行为以 testdata/parse.jsonl 为准（由 animego 的 JS 实现生成），
// 发现「JS 行为不合理」也不得单方面在这里修——先改 JS、再生语料、两边同步落地，
// 否则网站与 agent 会把同一个文件解成两个剧集。
//
// 三个跨语言语义坑（每处都有对应处理，改动前先读懂）：
//  1. JS 的 \s 匹配全角空格 U+3000 等 Unicode 空白，Go RE2 的 \s 只有 ASCII —— 统一用 jsWS 类；
//  2. Go RE2 没有 lookahead/lookbehind —— 相关正则改写为消耗型分组，见各处注释；
//  3. JS 的 .length 与 charCodeAt 按 UTF-16 code unit 计 —— 长度判断用 utf16Len。
package library

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"golang.org/x/text/unicode/norm"
)

// jsWS 复刻 JS 正则 \s 的字符集（ASCII 空白 + Unicode 空白 + BOM）。
const jsWS = `\t\n\v\f\r \x{00A0}\x{1680}\x{2000}-\x{200A}\x{2028}\x{2029}\x{202F}\x{205F}\x{3000}\x{FEFF}`

// utf16Len 按 UTF-16 code unit 计长度，对齐 JS 的 String.prototype.length。
func utf16Len(s string) int {
	return len(utf16.Encode([]rune(s)))
}

var (
	videoExtsRE    = regexp.MustCompile(`(?i)\.(mkv|mp4|avi|webm|flv|rmvb|mov|wmv|ts|m4v)$`)
	subtitleExtsRE = regexp.MustCompile(`(?i)\.(ass|ssa|srt|vtt)$`)
)

// resolutions 是会被误当集号的分辨率数字，任何集号解析命中它们都要跳过。
var resolutions = map[int]struct{}{360: {}, 480: {}, 720: {}, 1080: {}, 1440: {}, 2160: {}, 4320: {}}

// IsVideoFile 判断视频扩展名。
func IsVideoFile(fileName string) bool { return videoExtsRE.MatchString(fileName) }

// IsSubtitleFile 判断字幕扩展名。
func IsSubtitleFile(fileName string) bool { return subtitleExtsRE.MatchString(fileName) }

// SubtitleType 返回小写字幕扩展名（ass/ssa/srt/vtt），非字幕返回空串。
func SubtitleType(fileName string) string {
	m := subtitleExtsRE.FindStringSubmatch(fileName)
	if m == nil {
		return ""
	}
	return strings.ToLower(m[1])
}

// stripTechTokens 剥掉会被「最左数字兜底」偷走的编码/位深/分辨率 token。
var stripTechREs = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b\d{1,2}bit\b`),
	regexp.MustCompile(`(?i)\bHi10P?\b`),
	regexp.MustCompile(`(?i)\bx26[45]\b`),
	regexp.MustCompile(`(?i)\bH\.?26[45]\b`),
	regexp.MustCompile(`\b\d{3,4}[Pp]\b`),
}

func stripTechTokens(s string) string {
	for _, re := range stripTechREs {
		s = re.ReplaceAllString(s, "")
	}
	return s
}

// 集号解析的规则梯队，顺序即优先级（与 JS 一致，首个命中即返回）。
var episodePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bS\d+E(\d+)`),                                 // S01E03
	regexp.MustCompile(`(?i)\bEP?[` + jsWS + `]*(\d+)`),                    // EP03 / E03 / EP 03（\b 是回归案例：防 "de 2" 被偷）
	regexp.MustCompile(`第(\d+)[話话集]`),                                      // 第03話 / 第3集
	regexp.MustCompile(`[` + jsWS + `]-[` + jsWS + `](\d+)[` + jsWS + `]`), // " - 03 "
	regexp.MustCompile(`\[(\d+)(?:v\d+)?\]`),                               // [03] / [03v2]
}

var episodeFallbackRE = regexp.MustCompile(`(?:^|\D)(\d{2,3})(?:\D|$)`)

// ParseEpisodeNumber 从文件名解析集号；解析失败返回 nil。
func ParseEpisodeNumber(filename string) *int {
	for _, re := range episodePatterns {
		m := re.FindStringSubmatch(filename)
		if m == nil {
			continue
		}
		num, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		if _, isRes := resolutions[num]; isRes {
			continue // 命中分辨率数字：跳到下一条规则（对齐 JS 的 continue 语义）
		}
		return &num
	}
	// 兜底：先剥编码 token 再找孤立的 2-3 位数字，否则 10bit → 10、x265 → 265。
	m := episodeFallbackRE.FindStringSubmatch(stripTechTokens(filename))
	if m != nil {
		num, err := strconv.Atoi(m[1])
		if err == nil {
			if _, isRes := resolutions[num]; !isRes {
				return &num
			}
		}
	}
	return nil
}

var (
	tagRE   = regexp.MustCompile(`(?i)^(\d{2,4}[Pp]?\b|HEVC|AVC|x26[45]|H\.?26[45]|AAC|FLAC|WEB-?DL|WebRip|BDRip|Blu-?[Rr]ay|CHS|CHT|JPN?|ENG?|BIG5|GB|S\d+E?\d*|\d{1,3}(?:v\d+)?|SP\d*|OVA|OAD|NCOP|NCED|Commentary|Audio[` + jsWS + `]+Commentary|[A-Z0-9 ]+\d{3,4}[Pp])$`)
	rangeRE = regexp.MustCompile(`^\d{1,3}[` + jsWS + `]*-[` + jsWS + `]*\d{1,3}$`)
	// 括号里出现任意画质/编码 token → 整个括号是 tag 不是标题。
	qualityHintsRE = regexp.MustCompile(`(?i)\b(HEVC|AVC|x26[45]|H\.?26[45]|AAC|FLAC|10bit|8bit|WEB-?DL|WebRip|BDRip|Blu-?[Rr]ay|HDR|DV|TrueHD|DTS)\b`)
)

var (
	standardStripREs = []*regexp.Regexp{
		regexp.MustCompile(`\[[^\]]*\]`),
		regexp.MustCompile(`\([^)]*\)`),
		regexp.MustCompile(`\b\d{3,4}[Pp]\b`),
		regexp.MustCompile(`(?i)\b(HEVC|AVC|x26[45]|H\.?26[45]|AAC|FLAC)\b`),
		regexp.MustCompile(`(?i)\b(WEB-?DL|WebRip|BDRip|Blu-?[Rr]ay)\b`),
	}
	dashTitleRE     = regexp.MustCompile(`^(.+?)[` + jsWS + `]+-[` + jsWS + `]+\d+`)
	epTitleRE       = regexp.MustCompile(`(?i)^(.+?)[` + jsWS + `]*EP?[` + jsWS + `]*\d+`)
	trailingNumRE   = regexp.MustCompile(`[` + jsWS + `]+\d+[` + jsWS + `]*$`)
	bracketRE       = regexp.MustCompile(`\[([^\]]+)\]`)
	brTrailDashRE   = regexp.MustCompile(`[` + jsWS + `]*-[` + jsWS + `]*\d+[` + jsWS + `]*$`)
	brTrailEpRE     = regexp.MustCompile(`(?i)[` + jsWS + `]*EP?[` + jsWS + `]*\d+[` + jsWS + `]*$`)
	anyBracketChar  = regexp.MustCompile(`[\[\]]`)
	anyLetterRE     = regexp.MustCompile(`\p{L}`)
	anyJSWhitespace = regexp.MustCompile(`[` + jsWS + `]`)
)

// tryStandardPath 走「剥标签后取正文」的标题策略。
func tryStandardPath(filename string) string {
	name := videoExtsRE.ReplaceAllString(filename, "")
	for _, re := range standardStripREs {
		name = re.ReplaceAllString(name, "")
	}
	name = strings.TrimSpace(name)

	if m := dashTitleRE.FindStringSubmatch(name); m != nil {
		return strings.TrimSpace(m[1])
	}
	if m := epTitleRE.FindStringSubmatch(name); m != nil {
		return strings.TrimSpace(m[1])
	}
	return strings.TrimSpace(trailingNumRE.ReplaceAllString(name, ""))
}

// tryBracketHeavy 走「满屏括号里挑最长的非 tag 括号」的标题策略。
func tryBracketHeavy(filename string) string {
	var brackets []string
	for _, m := range bracketRE.FindAllStringSubmatch(filename, -1) {
		brackets = append(brackets, m[1])
	}
	if len(brackets) < 3 {
		return ""
	}
	var candidates []string
	for _, b := range brackets {
		t := strings.TrimSpace(b)
		if t == "" || utf16Len(t) <= 3 {
			continue
		}
		if tagRE.MatchString(t) || rangeRE.MatchString(t) || qualityHintsRE.MatchString(t) {
			continue
		}
		candidates = append(candidates, t)
	}
	if len(candidates) == 0 {
		return ""
	}
	// 按 UTF-16 长度降序（稳定排序，对齐现代 JS 的稳定 sort）。
	sort.SliceStable(candidates, func(i, j int) bool {
		return utf16Len(candidates[i]) > utf16Len(candidates[j])
	})
	title := candidates[0]
	title = brTrailDashRE.ReplaceAllString(title, "")
	title = brTrailEpRE.ReplaceAllString(title, "")
	title = strings.ReplaceAll(title, "_", " ")
	return strings.TrimSpace(title)
}

// looksLikeTitle 拦掉残破括号、纯画质 token、无字母的候选。
func looksLikeTitle(candidate string) bool {
	if candidate == "" || utf16Len(candidate) < 4 {
		return false
	}
	if anyBracketChar.MatchString(candidate) {
		return false
	}
	if !anyLetterRE.MatchString(candidate) {
		return false
	}
	if tagRE.MatchString(candidate) {
		return false
	}
	if qualityHintsRE.MatchString(candidate) && !anyJSWhitespace.MatchString(candidate) {
		return false
	}
	return true
}

// ParseAnimeKeyword 从文件名解析番剧标题；失败返回空串。
// 对齐 JS：standard 结果没过 looksLikeTitle 时，先试 bracketHeavy，
// 仍失败则【依旧返回 standard】——这是原实现的行为，不是 bug。
func ParseAnimeKeyword(filename string) string {
	if filename == "" {
		return ""
	}
	standard := tryStandardPath(filename)
	if looksLikeTitle(standard) {
		return standard
	}
	if b := tryBracketHeavy(filename); b != "" {
		return b
	}
	return standard
}

// kindPattern 的声明顺序即匹配优先级（更具体的 BD 特典类排前，首个命中即返回）。
type kindPattern struct {
	kind string
	re   *regexp.Regexp
}

var kindPatterns = []kindPattern{
	{"commentary", regexp.MustCompile(`(?i)(?:\bAudio[` + jsWS + `]+Commentary\b|\bCommentary\b|解[说說]|オーディオコメンタリー)`)},
	{"ncop", regexp.MustCompile(`(?i)\b(?:NC[` + jsWS + `]*OP\d*|Creditless[` + jsWS + `]+OP)\b`)},
	{"nced", regexp.MustCompile(`(?i)\b(?:NC[` + jsWS + `]*ED\d*|Creditless[` + jsWS + `]+ED)\b`)},
	{"menu", regexp.MustCompile(`(?i)(?:\bBD[` + jsWS + `]+Menu\b|\bDVD[` + jsWS + `]+Menu\b|\bmenu\b)`)},
	{"bonus", regexp.MustCompile(`(?i)(?:\bBonus\b|\bExtra\b|\bDisc[` + jsWS + `]+\d|特典)`)},
	{"trailer", regexp.MustCompile(`(?i)(?:\bTrailer\b|\bTeaser\b)`)},
	{"interview", regexp.MustCompile(`(?i)(?:\bInterview\b|\bCast[` + jsWS + `]+Talk\b|访谈|訪談)`)},
	{"wp", regexp.MustCompile(`(?i)(?:\bWP\d*\b|\bWeb[` + jsWS + `]+Preview\b)`)},
	{"cm", regexp.MustCompile(`(?i)\bCM[` + jsWS + `]*\d`)},
	{"movie", regexp.MustCompile(`(?i)(?:\bMovie\b|劇場版|剧场版)`)},
	{"sp", regexp.MustCompile(`(?i)\b(?:SP\d*|OAD\d*)\b`)},
	{"ova", regexp.MustCompile(`(?i)\bOVA\d*\b`)},
	{"pv", regexp.MustCompile(`(?i)(?:\bPV\d*\b|预告|預告)`)},
}

var anyDigitRE = regexp.MustCompile(`\d`)

// ParseEpisodeKind 从文件名推断集类型；含数字且无特征 token 时兜底 main，否则 unknown。
func ParseEpisodeKind(filename string) string {
	if filename == "" {
		return "unknown"
	}
	for _, kp := range kindPatterns {
		if kp.re.MatchString(filename) {
			return kp.kind
		}
	}
	if anyDigitRE.MatchString(filename) {
		return "main"
	}
	return "unknown"
}

var groupRE = regexp.MustCompile(`^\[([^\]]{1,30})\]`)

// resolutionRE：JS 原版用 lookbehind/lookahead 保证边界（下划线也算边界），
// RE2 不支持 —— 改写为消耗型边界分组，捕获组序号相应 +0（分辨率仍是组 1）。
var resolutionRE = regexp.MustCompile(`(?:^|[^A-Za-z0-9])(2160[Pp]|4[Kk]|1080[Pp]|720[Pp]|480[Pp])(?:[^A-Za-z0-9]|$)`)

var jsWSGlobalRE = regexp.MustCompile(`[` + jsWS + `]`)

// ─── 季号解析 ───

var cnNumDigit = map[rune]int{'零': 0, '一': 1, '二': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9}

// chineseToInt 把「四 / 十 / 十二 / 二十一」转成整数，失败返回 -1。
func chineseToInt(s string) int {
	r := []rune(s)
	if len(r) == 0 {
		return -1
	}
	if len(r) == 1 {
		if r[0] == '十' {
			return 10
		}
		if d, ok := cnNumDigit[r[0]]; ok {
			return d
		}
		return -1
	}
	// 十X → 10 + X
	if len(r) == 2 && r[0] == '十' {
		if d, ok := cnNumDigit[r[1]]; ok {
			return 10 + d
		}
		return -1
	}
	// X十 / X十Y → X*10 (+ Y)
	if len(r) >= 2 && r[1] == '十' {
		tens, okT := cnNumDigit[r[0]]
		ones, okO := 0, true
		if len(r) == 3 {
			ones, okO = cnNumDigit[r[2]], true
			if _, ok := cnNumDigit[r[2]]; !ok {
				okO = false
			}
		}
		if okT && okO {
			return tens*10 + ones
		}
	}
	return -1
}

var romanToInt = map[string]int{"I": 1, "II": 2, "III": 3, "IV": 4, "V": 5, "VI": 6, "VII": 7, "VIII": 8, "IX": 9, "X": 10}

var seasonREs = []struct {
	re    *regexp.Regexp
	roman bool
	cn    bool
}{
	{re: regexp.MustCompile(`(?i)\bS(\d{1,2})E\d{1,3}\b`)},                    // S04E01
	{re: regexp.MustCompile(`(?i)\bSeason[` + jsWS + `]+(\d{1,2})\b`)},        // Season 4
	{re: regexp.MustCompile(`(?i)\b(\d{1,2})(?:st|nd|rd|th)\b`)},              // 4th / 1st Cour
	{re: regexp.MustCompile(`\bS(\d{1,2})(?:[^A-Za-z\d]|$)`)},                 // 裸 S2（原版负向 lookahead 改写）
	{re: regexp.MustCompile(`第[` + jsWS + `]*(\d{1,2})[` + jsWS + `]*[季期部]`)}, // 第4季
	{re: regexp.MustCompile(`第[` + jsWS + `]*((?:十)?[零一二三四五六七八九]|[一二三四五六七八九]?十(?:[零一二三四五六七八九])?)[` + jsWS + `]*[季期部]`), cn: true}, // 第四季
	// 罗马数字：原版 lookahead 改写为消耗型；仅明显的季尾位置。
	{re: regexp.MustCompile(`(?:^|[` + jsWS + `]|\[|【|\()(II|III|IV|V|VI|VII|VIII|IX|X)(?:[` + jsWS + `]*[\])】]|[` + jsWS + `]+-[` + jsWS + `]|$)`), roman: true},
}

// ParseSeason 从文件名推断季号（NFKC 后按优先级匹配）；未识别返回 nil。
func ParseSeason(filename string) *int {
	if filename == "" {
		return nil
	}
	s := norm.NFKC.String(filename)
	for _, rule := range seasonREs {
		m := rule.re.FindStringSubmatch(s)
		if m == nil {
			continue
		}
		switch {
		case rule.roman:
			if n, ok := romanToInt[m[1]]; ok {
				return &n
			}
		case rule.cn:
			if n := chineseToInt(m[1]); n >= 0 {
				return &n
			}
		default:
			if n, err := strconv.Atoi(m[1]); err == nil {
				return &n
			}
		}
	}
	return nil
}

var absoluteEpRE = regexp.MustCompile(`(?:總第|总第)[` + jsWS + `]*(\d{1,4})`)

// ParseAbsoluteEpisode 提取繁中/简中字幕组的「總第N/总第N」跨季总集号；无标记返回 nil。
func ParseAbsoluteEpisode(filename string) *int {
	if filename == "" {
		return nil
	}
	m := absoluteEpRE.FindStringSubmatch(norm.NFKC.String(filename))
	if m == nil {
		return nil
	}
	if n, err := strconv.Atoi(m[1]); err == nil {
		return &n
	}
	return nil
}

// EpisodeMeta 是单文件名的完整解析结果；nil 表示对应字段未识别。
type EpisodeMeta struct {
	Title      *string
	Number     *int
	Kind       string
	Group      *string
	Resolution *string
	Season     *int
	EpisodeAlt *int
}

// ParseEpisodeMeta 汇总全部解析器（与 JS parseEpisodeMeta 逐字段对齐）。
func ParseEpisodeMeta(filename string) EpisodeMeta {
	if filename == "" {
		return EpisodeMeta{Kind: "unknown"}
	}
	meta := EpisodeMeta{
		Number:     ParseEpisodeNumber(filename),
		Kind:       ParseEpisodeKind(filename),
		Season:     ParseSeason(filename),
		EpisodeAlt: ParseAbsoluteEpisode(filename),
	}
	if t := ParseAnimeKeyword(filename); t != "" {
		meta.Title = &t
	}
	if m := groupRE.FindStringSubmatch(filename); m != nil {
		g := strings.TrimSpace(m[1])
		meta.Group = &g
	}
	if m := resolutionRE.FindStringSubmatch(filename); m != nil {
		raw := strings.ToLower(jsWSGlobalRE.ReplaceAllString(m[1], ""))
		if raw == "4k" {
			raw = "2160p"
		}
		if !strings.HasSuffix(raw, "p") {
			raw += "p"
		}
		switch raw {
		case "480p", "720p", "1080p", "2160p":
			meta.Resolution = &raw
		}
	}
	return meta
}
