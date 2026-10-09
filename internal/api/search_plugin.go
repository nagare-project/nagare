package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/nagare-project/nagare/internal/httpserver"
	"github.com/nagare-project/nagare/internal/rules"
	"github.com/nagare-project/nagare/internal/sourceplugin"
)

// pluginTorrentTimeout 是磁力选集向插件要 BT 候选的上限。BT 来源是直接抓取，
// 正常几秒就回；这里只挡住某个来源挂死把整个选集拖住。
const pluginTorrentTimeout = 25 * time.Second

// pluginSourcePrefix 标记来自插件的条目，与本机规则的 id 空间分开。
const pluginSourcePrefix = "plugin:"

// pluginTorrentQuery 是磁力选集附带的作品身份；有集号才会去问插件。
type pluginTorrentQuery struct {
	Titles  []string
	Episode int
	// Absolute 是这一集在跨季连续编号下的编号（前作 12 集时，第二季第 3 集叫 15）；
	// 0 或与 Episode 相同表示只有一种编号。
	Absolute  int
	AnilistID int
	Year      int
}

// episodes 是要向插件发的集号请求。插件按 absolute（缺省按 number）精确筛单集，一次请求
// 只认一种编号：只发 absolute=15 会把按季编号的「第二季 03」整批筛掉，只发 3 又丢掉连续
// 编号的「15」—— 有连续编号时两种各问一次（number 都是作品集号），候选合并去重。
func (q pluginTorrentQuery) episodes() []sourceplugin.Episode {
	number := strconv.Itoa(q.Episode)
	out := []sourceplugin.Episode{{Number: number, Absolute: float64(q.Episode)}}
	if q.Absolute > 0 && q.Absolute != q.Episode {
		out = append(out, sourceplugin.Episode{Number: number, Absolute: float64(q.Absolute)})
	}
	return out
}

// appendPluginTorrents 用插件的 BT 来源补充磁力选集。只要 torrent（transports 允许列表），
// 插件不会为此启动任何浏览器嗅探；候选映射成与本机规则同形的条目，来源 id 加前缀区分。
// 任何失败只落到 Sources 状态里，不影响本机规则的结果。
func (s *SourcePluginService) appendPluginTorrents(ctx context.Context, view *SearchView, query pluginTorrentQuery) {
	s.streamPluginTorrents(ctx, query, func(item *SearchItemView, outcome *rules.Outcome) error {
		if item != nil {
			view.Items = append(view.Items, *item)
		}
		if outcome != nil {
			view.Sources = append(view.Sources, *outcome)
		}
		return nil
	})
}

// PluginSearchEvent 是 GET /api/search/plugin 的 NDJSON 事件：条目、来源结果、结束。
// 选集窗口先拿本机规则的结果（不到一秒），插件来源一条条追加——最慢的站点（Mikan 十几秒）
// 不再拖住整个列表。
type PluginSearchEvent struct {
	Event   string          `json:"event"`
	Item    *SearchItemView `json:"item,omitempty"`
	Outcome *rules.Outcome  `json:"outcome,omitempty"`
}

// streamPluginTorrents 逐条交出插件的 BT 候选；每个来源有了结论就交出它的结果状态。
// emit 返回错误（客户端断开）即停止。emit 不会被并发调用。
func (s *SourcePluginService) streamPluginTorrents(ctx context.Context, query pluginTorrentQuery, emit func(item *SearchItemView, outcome *rules.Outcome) error) {
	if s == nil || query.Episode < 1 || s.Status().Phase != "ready" {
		return
	}
	// 与本机规则同一套整理（去空白、不分大小写去重、最多四种写法）：插件的请求格式要求标题不重复，
	// 原名与英文名写法相同时不去重，整次请求会被插件拒掉；上限也挡住「写法 × 页」的抓取扇出。
	titles := rules.QueryVariants(query.Titles)
	if len(titles) == 0 {
		return
	}
	base := sourceplugin.ResolveRequest{
		Schema:      "nagare-resolve-request/v1",
		Subject:     sourceplugin.Subject{IDs: map[string]string{}, Titles: titles},
		Preferences: sourceplugin.Preferences{Transports: []string{"torrent"}},
	}
	if query.AnilistID > 0 {
		base.Subject.IDs["anilist"] = strconv.Itoa(query.AnilistID)
	}
	if query.Year > 0 {
		year := query.Year
		base.Subject.Year = &year
	}
	names := map[string]string{}
	if sources, err := s.runtime.Sources(ctx); err == nil {
		for _, source := range sources {
			names[source.ID] = source.Name
		}
	}

	requestContext, cancel := context.WithTimeout(ctx, pluginTorrentTimeout)
	defer cancel()
	episodes := query.episodes()
	merge := newPluginMerge(len(episodes), names, emit, cancel)
	errs := make([]error, len(episodes))
	var wg sync.WaitGroup
	for i, episode := range episodes {
		request := base // Subject 里的 map/切片只读，两个请求共用无妨
		request.Episode = episode
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = s.Candidates(requestContext, request, merge.handle)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		// 浏览器走了（ctx 取消）不算故障；其余的都要留痕 —— 两种编号只有一种失败时，界面上
		// 另一种的结果照常展示，原因只在这里
		if err != nil && ctx.Err() == nil {
			log.Printf("api: 向来源插件要 BT 候选失败（第 %d 个集号请求）：%v", i+1, err)
		}
	}
	merge.finish(ctx, errs)
}

// searchPlugin 是 GET /api/search/plugin：只问插件的 BT 来源，NDJSON 逐条返回。
func (h *Handler) searchPlugin(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	episode, err := strconv.Atoi(params.Get("episode"))
	if err != nil || episode < 1 {
		httpserver.WriteError(w, http.StatusBadRequest, "集号必须大于零")
		return
	}
	absolute, ok := optionalEpisodeParam(params.Get("absolute"))
	if episode > maxEpisodeNumber || !ok {
		httpserver.WriteError(w, http.StatusBadRequest, "集号超出范围")
		return
	}
	anilist, _ := strconv.Atoi(params.Get("anilist"))
	year, _ := strconv.Atoi(params.Get("year"))
	titles := append([]string{params.Get("q")}, params["title"]...)
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	encoder := json.NewEncoder(w)
	write := func(event PluginSearchEvent) error {
		if err := encoder.Encode(event); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	}
	query := pluginTorrentQuery{Titles: titles, Episode: episode, Absolute: absolute, AnilistID: anilist, Year: year}
	h.deps.SourcePlugin.streamPluginTorrents(r.Context(), query, func(item *SearchItemView, outcome *rules.Outcome) error {
		return write(PluginSearchEvent{Event: "item", Item: item, Outcome: outcome})
	})
	_ = write(PluginSearchEvent{Event: "done"})
}

// optionalEpisodeParam 解析可缺省的集号参数：缺席为 0；给了就必须是 1..maxEpisodeNumber。
func optionalEpisodeParam(raw string) (int, bool) {
	if raw == "" {
		return 0, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > maxEpisodeNumber {
		return 0, false
	}
	return n, true
}
