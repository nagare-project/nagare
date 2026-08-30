// 标题归一化 —— 从 animego normalize.js 逐行移植（决议 CQ2）。
// 专为中文字幕组命名而写：【】剥离、·！？。、作分隔、NFKC 全角转半角。
// 行为以 testdata/parse.jsonl 为准，不得单方面「改进」。
package library

import (
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// noiseTokens：小写化后要剥掉的噪声（分辨率/编码/来源/音轨/字幕标记）。
var noiseTokens = map[string]struct{}{
	"1080p": {}, "720p": {}, "480p": {}, "2160p": {}, "4k": {},
	"x264": {}, "x265": {}, "h264": {}, "h265": {}, "hevc": {}, "avc": {}, "avc1": {},
	"bluray": {}, "blu-ray": {}, "bdrip": {}, "bdremux": {}, "webrip": {}, "web-dl": {}, "webdl": {},
	"hdtv": {}, "dvdrip": {}, "dvd": {},
	"aac": {}, "ac3": {}, "flac": {}, "mp3": {}, "dts": {}, "opus": {}, "vorbis": {},
	"srtx2": {}, "srtx1": {}, "ass": {}, "pgs": {}, "sub": {}, "sup": {},
	"10bit": {}, "8bit": {}, "hi10p": {}, "hi444": {}, "yuv420": {},
	"remux": {}, "amzn": {}, "nflx": {}, "hmax": {},
}

// epTokenRE 匹配集号类 token：S2、E01、EP12、01~03、ep01-end 等（输入已小写，无需 i 标志）。
var epTokenRE = regexp.MustCompile(`^(?:s\d+|ep?\d+|e\d+|e\d+-\d+|\d{1,3}(?:end)?)$`)

var (
	leadingTagsRE = regexp.MustCompile(`^(?:\[[^\]]*\]|\([^)]*\)|【[^】]*】|[` + jsWS + `])+`)
	sqBracketRE   = regexp.MustCompile(`\[[^\]]*\]`)
	parenRE       = regexp.MustCompile(`\([^)]*\)`)
	cnBracketRE   = regexp.MustCompile(`【[^】]*】`)
	// 分隔符：空白 + 常见半角标点 + 中文标点（·！？。、）。
	splitRE = regexp.MustCompile(`[` + jsWS + `\-_.,;:·\\/|+~！？。、]+`)
)

// NormalizeTokens 把标题归一化成小写半角 token 数组，剥掉噪声与集号 token。
// 空输入返回空切片（非 nil，保证 JSON 序列化为 []）。
func NormalizeTokens(title string) []string {
	tokens := []string{}
	if title == "" {
		return tokens
	}

	// NFKC：全角数字/字母 → ASCII，兼容分解（Ⅱ → II 也发生在这里）。
	s := norm.NFKC.String(title)
	s = leadingTagsRE.ReplaceAllString(s, "")
	s = sqBracketRE.ReplaceAllString(s, " ")
	s = parenRE.ReplaceAllString(s, " ")
	s = cnBracketRE.ReplaceAllString(s, " ")
	s = strings.ToLower(s)

	for _, tok := range splitRE.Split(s, -1) {
		t := strings.TrimSpace(tok)
		if t == "" {
			continue
		}
		if _, isNoise := noiseTokens[t]; isNoise {
			continue
		}
		if epTokenRE.MatchString(t) {
			continue
		}
		tokens = append(tokens, t)
	}
	return tokens
}
