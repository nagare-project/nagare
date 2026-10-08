// Package releasetitle 解析磁力搜索结果的【发布标题】里与选集有关的两件事：
// 这条发布是不是合集，合集覆盖哪几集。
//
// 为什么不放进 internal/library：那条解析链是给单个文件名用的，并且与 animego 前端
// 共享一份语料（决议 CQ2），不能为发布标题改它的行为 —— 而在文件名解析链看来，
// 「[01-25全]」就是第 1 集。两个调用方（搜索结果的结构化字段、磁力播放的选集提示）
// 必须共用这一份判断：界面说「整季合集」、播放端却按第 1 集自动开播，正是这样来的。
package releasetitle

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// MaxEpisode 是认作集号的上限：再大的数字是年份、编码参数一类，不是集号。
const MaxEpisode = 999

// Range 是合集覆盖的集号区间（含两端）。编号是发布标题自己的编号：
// 跨季连续编号的第二季合集写作「13-24」，换算成作品集号是调用方的事。
type Range struct {
	Low  int `json:"low"`
	High int `json:"high"`
}

// rangePatterns 是集号区间的几种写法，每条的两个分组是起止集号。
//
// 来源插件（Nagare Source 的 btcrawler.episodeRange）按一条更宽松的写法决定合集能不能通过
// 集号筛选：只要两个 1–3 位数字被 - ~ 连着、前面是开头、空白或左括号就算。这里更严 ——
// 不带括号时连接号两边不能有空格：「Kono Subarashii 3 - 10」「Gintama 3 - 12」是第三季的
// 第 10、12 集，按区间读会把它挂到第 3–10 集每一集下面，还会丢掉插件解出的正确集号。
// 两边不一致的后果只是插件放行的那条按单集展示（插件自己给了集号），不会把单集错当合集。
var rangePatterns = []*regexp.Regexp{
	// 括号里的区间：[01-12]、[01 - 12 Fin]、【01-25全】、(01-12)、（01-12）、［1~13］、[01-12+SP]、[01-12v2]、
	// [01-28 END (v2)]。区间后面的版本号、完结字样、附加内容可以任意组合、任意顺序
	regexp.MustCompile(`(?i)[\[【［(（]\s*(\d{1,3})\s*[-~～]\s*(\d{1,3})(?:\s*(?:v\d+|[(（]\s*v\d+\s*[)）]|全|Fin|END|完|\+\s*[A-Za-z]+))*\s*[\]】］)）]`),
	// 带「话 / 集」的：第01-12话、01-12集
	regexp.MustCompile(`(?:第\s*)?(\d{1,3})\s*[-~～]\s*(\d{1,3})\s*[话話集]`),
	// EP01-12、E01-E12、S01E01-E12
	regexp.MustCompile(`(?i)\b(?:S\d{1,2})?EP?(\d{1,3})\s*[-~～]\s*(?:EP?)?(\d{1,3})\b`),
	// 不带括号的只认连写：01-12、01~13 全、1-12 Fin
	regexp.MustCompile(`(?i)(?:^|\s)(\d{1,3})[-~～](\d{1,3})(?:全|Fin|END|完)?(?:$|\s|[\[【(（])`),
}

// countedPrefixes 后面跟的数字区间是季数、分卷，不是集号（Season 1-3、Part 1-2、Vol 1-6）。
var countedPrefixes = []string{"season", "s", "part", "vol", "vol.", "volume", "disc", "cour"}

// totalPattern 认「全 12 集 / 全12话」：没写起止时，就是第 1 集到第 N 集。
// 不认它的话，文件名解析链会把这个 12 读成「第 12 集」，播放端随即自动开播第 12 集。
var totalPattern = regexp.MustCompile(`全\s*(\d{1,3})\s*[集话話]`)

// keywordPattern 是明说自己是合集、但写不出集号区间的字样。只收不会出现在单集标题里的词：
// BDRip、BD 这类单集发布同样会写，不能算。「全集中」（鬼灭之刃的台词）不是全集。
var keywordPattern = regexp.MustCompile(`(?i)合集|全集(?:[^中]|$)|\bBatch\b|BD-?BOX|\bComplete\b`)

// BatchRange 解出合集标题里的集号区间；写不出区间（或根本不是合集）时 ok 为 false。
func BatchRange(title string) (Range, bool) {
	for _, pattern := range rangePatterns {
		for _, m := range pattern.FindAllStringSubmatchIndex(title, -1) {
			if countedRange(title, m[2]) {
				continue
			}
			low, _ := strconv.Atoi(title[m[2]:m[3]])
			high, _ := strconv.Atoi(title[m[4]:m[5]])
			if low > 0 && high > low && high <= MaxEpisode {
				return Range{Low: low, High: high}, true
			}
		}
	}
	if m := totalPattern.FindStringSubmatch(title); m != nil {
		if total, _ := strconv.Atoi(m[1]); total > 1 && total <= MaxEpisode {
			return Range{Low: 1, High: total}, true
		}
	}
	return Range{}, false
}

// countedRange：从 start 起的区间前面紧挨着 Season / Part / Vol 一类的词。
func countedRange(title string, start int) bool {
	words := strings.FieldsFunc(title[:start], func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune("[【［(（", r)
	})
	if len(words) == 0 {
		return false
	}
	last := strings.ToLower(words[len(words)-1])
	for _, prefix := range countedPrefixes {
		if last == prefix {
			return true
		}
	}
	return false
}

// IsBatch 判断发布标题是不是合集（整季包、区间包、BD-BOX）。
func IsBatch(title string) bool {
	if _, ok := BatchRange(title); ok {
		return true
	}
	return keywordPattern.MatchString(title)
}
