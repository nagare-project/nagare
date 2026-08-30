// dandanplay 文件匹配：发文件名 + 首 16MB hash（+ 可选关键词），换回
// anilistId、标题与「集号 → dandanplay episodeId」的映射（弹幕接口的钥匙）。
//
// 决议里明确「调 API，不移植」：服务端的三段瀑布（hash 直击 → 关键词拓宽 →
// 兜底）对客户端是黑盒，这里只关心请求形状与响应的三种 phase 多态。
package animego

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// MatchInput 是一次文件匹配的输入。
type MatchInput struct {
	// FileName 是不含路径的文件名，必填。
	FileName string
	// FileHash 是文件首 16MB 的 MD5（小写 hex）；大小写这里会再归一化一次。
	// 可空 —— 没 hash 时服务端只能靠文件名/关键词，命中率下降但不报错。
	FileHash string
	// FileSize 是完整文件字节数。
	FileSize int64
	// Episode 是解析出的集号。服务端要求 episodes 数组非空且 files[].episode
	// 对应，否则就算 hash 命中也返回 matched:false —— 所以它必须进请求。
	Episode int
	// Keyword 可选：番剧标题关键词，能拓宽 phase1 并触发 phase2。
	Keyword string
}

// EpisodeRef 是 episodeMap 里单集的引用。
type EpisodeRef struct {
	// DandanEpisodeID 就是弹幕接口 Comments 需要的 episodeId。
	DandanEpisodeID int64
	// Title 形如「第7话 xxx」。
	Title string
}

// MatchResult 是匹配结果。Matched 为 false 时其余字段均为零值 ——
// 完全 miss 是正常业务结果，不是错误。
type MatchResult struct {
	Matched bool
	// Source 是命中来源："dandanplay" 或 "animeCache"。
	Source string

	// 番剧元数据：优先取 siteAnime（站内库，字段最全），缺的再从 anime 补。
	// phase1 命中时只有 TitleNative/CoverImageURL，AnilistID 可能为 0 ——
	// 调用方要接受「匹配到了但没有 anilistId」的局面（此时进度同步不可用）。
	AnilistID     int
	TitleChinese  string
	TitleNative   string
	TitleRomaji   string
	CoverImageURL string
	TotalEpisodes int

	// EpisodeMap 是集号 → 单集引用（服务端的字符串键已转回 int）。
	EpisodeMap map[int]EpisodeRef
}

// matchFile 是请求里 files 数组的元素。
type matchFile struct {
	Episode  int    `json:"episode"`
	FileName string `json:"fileName"`
	FileHash string `json:"fileHash,omitempty"`
	FileSize int64  `json:"fileSize"`
}

// animeWire 是 anime / siteAnime 的宽松并集：phase1 只有标题+封面，
// phase2 是完整对象，phase3 是空对象 {}，siteAnime 可能为 null ——
// 全部字段 optional，多余字段忽略。
type animeWire struct {
	AnilistID     int    `json:"anilistId"`
	TitleChinese  string `json:"titleChinese"`
	TitleNative   string `json:"titleNative"`
	TitleRomaji   string `json:"titleRomaji"`
	CoverImageURL string `json:"coverImageUrl"`
	Episodes      int    `json:"episodes"`
}

// Match 调 POST /api/dandanplay/match（公开端点，无需登录）。
// 注意服务端把格式错误的请求也答成 200 的 {"matched":false}，
// 所以「返回 nil 错误且 Matched=false」既可能是真 miss 也可能是请求被判无效。
func (c *Client) Match(ctx context.Context, in MatchInput) (MatchResult, error) {
	const op = "match"
	if in.FileName == "" {
		return MatchResult{}, &Error{Kind: ErrBadRequest, Op: op, Err: errors.New("fileName 不能为空")}
	}

	hash := strings.ToLower(in.FileHash)
	body, err := marshalJSON(op, struct {
		FileName string      `json:"fileName"`
		FileHash string      `json:"fileHash,omitempty"`
		FileSize int64       `json:"fileSize"`
		Keyword  string      `json:"keyword,omitempty"`
		Episodes []int       `json:"episodes"`
		Files    []matchFile `json:"files"`
	}{
		FileName: in.FileName,
		FileHash: hash,
		FileSize: in.FileSize,
		Keyword:  in.Keyword,
		// 大坑：episodes 必须非空、files[].episode 必须对应（见 MatchInput.Episode）。
		Episodes: []int{in.Episode},
		Files: []matchFile{{
			Episode:  in.Episode,
			FileName: in.FileName,
			FileHash: hash,
			FileSize: in.FileSize,
		}},
	})
	if err != nil {
		return MatchResult{}, err
	}

	res, err := c.send(ctx, op, http.MethodPost, "/api/dandanplay/match", body, nil)
	if err != nil {
		return MatchResult{}, err
	}
	// 服务端 20s 超时会给 500 {"error":"match timed out"} → ErrUnavailable。
	if err := classifyStatus(op, res, false); err != nil {
		return MatchResult{}, err
	}

	var wire struct {
		Matched    bool       `json:"matched"`
		Source     string     `json:"source"`
		Anime      *animeWire `json:"anime"`
		SiteAnime  *animeWire `json:"siteAnime"`
		EpisodeMap map[string]struct {
			DandanEpisodeID int64  `json:"dandanEpisodeId"`
			Title           string `json:"title"`
		} `json:"episodeMap"`
	}
	if err := decodeJSON(op, res.body, &wire); err != nil {
		return MatchResult{}, err
	}
	if !wire.Matched {
		return MatchResult{}, nil
	}
	if len(wire.EpisodeMap) == 0 {
		return MatchResult{}, &Error{Kind: ErrDecode, Op: op, Err: errors.New("matched=true 但响应缺少 episodeMap")}
	}

	epMap := make(map[int]EpisodeRef, len(wire.EpisodeMap))
	for key, v := range wire.EpisodeMap {
		ep, convErr := strconv.Atoi(key)
		if convErr != nil {
			return MatchResult{}, &Error{Kind: ErrDecode, Op: op, Err: fmt.Errorf("episodeMap 的键 %q 不是集号", key)}
		}
		if v.DandanEpisodeID <= 0 {
			return MatchResult{}, &Error{Kind: ErrDecode, Op: op, Err: fmt.Errorf("episodeMap[%s] 缺少 dandanEpisodeId", key)}
		}
		epMap[ep] = EpisodeRef{DandanEpisodeID: v.DandanEpisodeID, Title: v.Title}
	}

	merged := mergeAnime(wire.SiteAnime, wire.Anime)
	return MatchResult{
		Matched:       true,
		Source:        wire.Source,
		AnilistID:     merged.AnilistID,
		TitleChinese:  merged.TitleChinese,
		TitleNative:   merged.TitleNative,
		TitleRomaji:   merged.TitleRomaji,
		CoverImageURL: merged.CoverImageURL,
		TotalEpisodes: merged.Episodes,
		EpisodeMap:    epMap,
	}, nil
}

// mergeAnime 按字段合并：优先 siteAnime（站内库权威），缺的再从 anime 补。
// 这样 phase2「siteAnime 为 null、anilistId 在 anime 里」与 phase3
// 「anime 是空对象、数据全在 siteAnime」两种形状都能取到完整字段。
func mergeAnime(site, fallback *animeWire) animeWire {
	var out animeWire
	for _, src := range []*animeWire{site, fallback} {
		if src == nil {
			continue
		}
		if out.AnilistID == 0 {
			out.AnilistID = src.AnilistID
		}
		if out.TitleChinese == "" {
			out.TitleChinese = src.TitleChinese
		}
		if out.TitleNative == "" {
			out.TitleNative = src.TitleNative
		}
		if out.TitleRomaji == "" {
			out.TitleRomaji = src.TitleRomaji
		}
		if out.CoverImageURL == "" {
			out.CoverImageURL = src.CoverImageURL
		}
		if out.Episodes == 0 {
			out.Episodes = src.Episodes
		}
	}
	return out
}
