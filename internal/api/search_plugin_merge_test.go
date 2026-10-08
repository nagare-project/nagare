package api

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/rules"
	"github.com/nagare-project/nagare/internal/sourceplugin"
)

// recorder 收集合并器交出的条目与结论。
type recorder struct {
	items    []SearchItemView
	outcomes []rules.Outcome
	err      error
}

func (r *recorder) emit(item *SearchItemView, outcome *rules.Outcome) error {
	if r.err != nil {
		return r.err
	}
	if item != nil {
		r.items = append(r.items, *item)
	}
	if outcome != nil {
		r.outcomes = append(r.outcomes, *outcome)
	}
	return nil
}

func candidateEvent(source, infohash string) sourceplugin.Event {
	return sourceplugin.Event{Event: "candidate", Candidate: btCandidate(source, infohash, "[G] 某作品 - 03 [1080p]", 3)}
}

// 插件的候选流自己断了、又没有任何来源的结论：要说出来，别让界面以为「没有资源」；
// 浏览器已经走了（请求 ctx 取消）就不必再说。
func TestPluginMergeReportsBrokenStream(t *testing.T) {
	rec := &recorder{}
	newPluginMerge(1, nil, rec.emit, func() {}).finish(context.Background(), []error{errors.New("plugin candidate stream ended without done")})
	require.Len(t, rec.outcomes, 1)
	assert.Equal(t, pluginSourcePrefix+"bt", rec.outcomes[0].Source)
	assert.Equal(t, rules.StateFailed, rec.outcomes[0].State)
	assert.Contains(t, rec.outcomes[0].Reason, "来源插件候选流中断")

	// 整体超时：说清是超时
	rec = &recorder{}
	newPluginMerge(1, nil, rec.emit, func() {}).finish(context.Background(), []error{context.DeadlineExceeded})
	require.Len(t, rec.outcomes, 1)
	assert.Contains(t, rec.outcomes[0].Reason, "超时")

	rec = &recorder{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	newPluginMerge(1, nil, rec.emit, func() {}).finish(ctx, []error{errors.New("canceled")})
	assert.Empty(t, rec.outcomes)

	// 流正常结束、一个来源都没报：插件确实什么都没有，不报故障
	rec = &recorder{}
	newPluginMerge(2, nil, rec.emit, func() {}).finish(context.Background(), []error{nil, nil})
	assert.Empty(t, rec.outcomes)
}

// 客户端断开后：叫停其余请求，之后的事件一律不再交出。
func TestPluginMergeStopsAfterClientDisconnect(t *testing.T) {
	rec := &recorder{err: errors.New("broken pipe")}
	aborted := 0
	m := newPluginMerge(2, nil, rec.emit, func() { aborted++ })
	require.Error(t, m.handle(candidateEvent("garden", hashA)))
	assert.Equal(t, 1, aborted, "另一个请求要被叫停")

	rec.err = nil
	assert.Error(t, m.handle(candidateEvent("garden", hashB)), "断开之后的事件直接拒收")
	m.finish(context.Background(), []error{nil, nil})
	assert.Empty(t, rec.items)
	assert.Empty(t, rec.outcomes)
}

// 一个请求对某来源报了失败，另一个请求的流半路断了、没提到它：收尾时照它报的说，
// 而不是报「候选流中断」或者什么都不说。
func TestPluginMergeSettlesPartialFailuresAtFinish(t *testing.T) {
	rec := &recorder{}
	m := newPluginMerge(2, nil, rec.emit, func() {})
	require.NoError(t, m.handle(sourceplugin.Event{Event: "source_error", SourceID: "nyaa", Category: "search_failed", Message: "dns"}))
	assert.Empty(t, rec.outcomes, "另一个请求还可能有结果，先不下结论")
	require.NoError(t, m.handle(candidateEvent("garden", hashA)))
	require.NoError(t, m.handle(candidateEvent("garden", hashA)), "同一条发布第二次来：不重复交出")
	m.finish(context.Background(), []error{nil, errors.New("stream ended without done")})

	require.Len(t, rec.items, 1)
	byID := map[string]rules.Outcome{}
	for _, outcome := range rec.outcomes {
		byID[outcome.Source] = outcome
	}
	assert.Len(t, byID, 3)
	assert.Equal(t, rules.StateOK, byID["plugin:garden"].State)
	assert.Equal(t, 1, byID["plugin:garden"].Count)
	assert.Equal(t, rules.StateFailed, byID["plugin:nyaa"].State)
	assert.Equal(t, "search_failed", byID["plugin:nyaa"].Detail)
	// 半路断掉的那一个请求本身也要说出来：别的来源有结果，不代表结果是全的
	assert.Equal(t, rules.StateFailed, byID["plugin:bt"].State)
}

// 一种编号失败、另一种有结果：报有结果，但在细节里留下另一种失败的痕迹。
func TestPluginMergeKeepsPartialFailureDetail(t *testing.T) {
	rec := &recorder{}
	m := newPluginMerge(2, nil, rec.emit, func() {})
	require.NoError(t, m.handle(sourceplugin.Event{Event: "source_error", SourceID: "garden", Category: "search_timeout", Message: "slow"}))
	require.NoError(t, m.handle(candidateEvent("garden", hashA)))
	m.finish(context.Background(), []error{nil, nil})
	require.Len(t, rec.outcomes, 1)
	assert.Equal(t, rules.StateOK, rec.outcomes[0].State)
	assert.Empty(t, rec.outcomes[0].Reason, "有结果时界面不报故障")
	assert.Contains(t, rec.outcomes[0].Detail, "search_timeout")
}

// 两种编号各问一次时，插件给的文件建议可能指向另一种编号的那一集：不转交。只问一次时照常转交。
func TestPluginMergeDropsFileIndexUnderDualNumbering(t *testing.T) {
	index := 4
	event := func() sourceplugin.Event {
		e := candidateEvent("garden", hashA)
		e.Candidate.Transport.FileIndex = &index
		return e
	}
	rec := &recorder{}
	require.NoError(t, newPluginMerge(2, nil, rec.emit, func() {}).handle(event()))
	require.Len(t, rec.items, 1)
	assert.Nil(t, rec.items[0].FileIndex)

	rec = &recorder{}
	require.NoError(t, newPluginMerge(1, nil, rec.emit, func() {}).handle(event()))
	require.Len(t, rec.items, 1)
	require.NotNil(t, rec.items[0].FileIndex)
	assert.Equal(t, 4, *rec.items[0].FileIndex)
}

// 只给 .torrent 地址的发布也要能去重（没有 infohash 时按种子地址认）。
func TestReleaseLocator(t *testing.T) {
	assert.Equal(t, hashA, releaseLocator(SearchItemView{Item: rules.Item{Infohash: hashA, Magnet: "magnet:?xt=urn:btih:" + hashA}}))
	assert.Equal(t, "magnet:?xt=urn:btih:x", releaseLocator(SearchItemView{Item: rules.Item{Magnet: "magnet:?xt=urn:btih:x"}}))
	assert.Equal(t, "https://acg.example/1.torrent", releaseLocator(SearchItemView{TorrentURL: "https://acg.example/1.torrent"}))
}
