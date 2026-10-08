package api

import (
	"math"
	"strings"
	"unicode/utf8"

	"github.com/nagare-project/nagare/internal/torrentstream"
)

// POST /api/torrent/play 的请求形状与校验。

// 作品身份的边界：目录里一部作品带主标题 + 原名 + 英文名，四个留余量；
// 单个标题再长也不会超过 200 字，挡住把整段文本当标题塞进来。
const (
	maxIdentityTitles     = 4
	maxIdentityTitleRunes = 200
)

// maxEpisodeNumber 是接受的集号上限（长篇连载过千集，再大就不是集号了）。
const maxEpisodeNumber = 9999

type torrentPlayRequest struct {
	Magnet      string `json:"magnet"`
	TorrentURL  string `json:"torrentUrl"`
	Title       string `json:"title"`
	EpisodeHint int    `json:"episodeHint"`
	// AltEpisodeHint 是同一集在跨季连续编号下的编号（第二季第 3 集在合集里叫 15）。
	AltEpisodeHint int `json:"altEpisodeHint"`
	// FileIndex 是用户在选集弹窗里选定的文件。用指针：0 是合法下标，零值分不出
	// 「用户选了第 0 个」与「还没选」。
	FileIndex *int `json:"fileIndex"`
	// SuggestedFileIndex 是来源插件指明的文件：只是建议，指得不对（被认成花絮、越界）
	// 就照常按集号选或弹选集，不拿「选中的文件不在这条资源里」去回一个从没见过列表的用户。
	SuggestedFileIndex *int `json:"suggestedFileIndex"`
	// AnilistID 与 Titles 是用户在哪部目录作品里点的播放：只在本机用来校验弹幕匹配没有
	// 认成别的作品（与在线候选同一套，见 player.MatchHinted）。磁力本身从不发给 animego ——
	// 「拿作品 ID 换磁力」的接口永远不做（红线 2）。从搜索页播放时两者都缺席。
	AnilistID int      `json:"anilistId"`
	Titles    []string `json:"titles"`
}

// prepare 校验请求并翻成引擎的准备请求，连同整理过的作品标题；problem 非空时是给用户看的 400 原因。
func (req torrentPlayRequest) prepare() (prep torrentstream.PrepareRequest, titles []string, problem string) {
	if (strings.TrimSpace(req.Magnet) == "") == (strings.TrimSpace(req.TorrentURL) == "") {
		return prep, nil, "需要提供磁力链接或种子文件地址"
	}
	if !validEpisodeHint(req.EpisodeHint) || !validEpisodeHint(req.AltEpisodeHint) {
		return prep, nil, "集号超出范围"
	}
	titles, ok := matchTitles(req.AnilistID, req.Titles)
	if !ok {
		return prep, nil, "作品身份字段无效"
	}
	prep = torrentstream.PrepareRequest{
		Magnet:         req.Magnet,
		TorrentURL:     req.TorrentURL,
		Title:          req.Title,
		EpisodeHint:    req.EpisodeHint,
		AltEpisodeHint: req.AltEpisodeHint,
		FileIndex:      -1,
	}
	if req.FileIndex != nil {
		prep.FileIndex = *req.FileIndex
	}
	if suggested := req.SuggestedFileIndex; suggested != nil && *suggested >= 0 {
		index := *suggested
		prep.SuggestedFileIndex = &index
	}
	return prep, titles, ""
}

func validEpisodeHint(n int) bool { return n >= 0 && n <= maxEpisodeNumber }

// matchTitles 校验作品身份并整理标题：去空白、不分大小写去重、保序。anilistID 为 0 表示没有身份。
// 第二个返回值为 false 表示字段不合法（调用方回 400）。
func matchTitles(anilistID int, titles []string) ([]string, bool) {
	if anilistID < 0 || anilistID > math.MaxInt32 || len(titles) > maxIdentityTitles {
		return nil, false
	}
	kept := make([]string, 0, len(titles))
	for _, title := range titles {
		title = strings.TrimSpace(title)
		if utf8.RuneCountInString(title) > maxIdentityTitleRunes {
			return nil, false
		}
		if title != "" && !containsFold(kept, title) {
			kept = append(kept, title)
		}
	}
	return kept, true
}

func containsFold(list []string, value string) bool {
	for _, item := range list {
		if strings.EqualFold(item, value) {
			return true
		}
	}
	return false
}
