// 临时参照组：复制自 animego go-api/internal/torrents（AGPL-3.0，同一作者）。M2 差分测试跑通后整包删除，勿在此新增功能。

// Package torrents — registry_test.go
//
// Covers the pluggable-source seam added in the source-registry
// refactor.  These tests are additive — the pre-existing aggregator /
// fetcher tests are untouched and still exercise behaviour parity.
//
//   - Registry preserves registration order via Sources()
//   - Register appends in order
//   - replaceByName swaps a source in place (position preserved)
//   - （参照组删减）原 TestRegistry_FanoutOrder 走的是 Aggregator.Fetch，
//     而 aggregator.go 依赖 internal/cache 未搬入；该测试验证的是聚合层的
//     合并顺序而非源适配器行为，故整段移除。staticFn / stubItem 两个辅助
//     函数原住 aggregator_test.go，随本文件一起搬来。
//   - compile-time: the three built-in source adapters satisfy Fetcher,
//     and the RSS sources deliberately do NOT satisfy Capable
package reference

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Compile-time: adapters satisfy Fetcher; Capable is opt-in.
// ---------------------------------------------------------------------------

var (
	_ Fetcher = gardenSource{}
	_ Fetcher = acgSource{}
	_ Fetcher = nyaaSource{}
	_ Fetcher = funcSource{}
)

func TestRSSSources_DoNotImplementCapable(t *testing.T) {
	t.Parallel()

	// RSS scrapes have no seeders / special budget — they must stay the
	// zero-Capabilities default so a future ranker doesn't treat them as
	// richer than they are.
	if _, ok := any(acgSource{}).(Capable); ok {
		t.Fatal("acgSource should NOT implement Capable")
	}
	if _, ok := any(nyaaSource{}).(Capable); ok {
		t.Fatal("nyaaSource should NOT implement Capable")
	}

	// CapabilitiesOf falls back to the zero value for a non-Capable source.
	assert.Equal(t, Capabilities{}, CapabilitiesOf(acgSource{}))
	assert.Equal(t, Capabilities{}, CapabilitiesOf(nyaaSource{}))
}

// ---------------------------------------------------------------------------
// Registry ordering primitives.
// ---------------------------------------------------------------------------

func TestRegistry_PreservesRegistrationOrder(t *testing.T) {
	t.Parallel()

	g := newFuncSource(SourceGarden, staticFn(nil, nil))
	a := newFuncSource(SourceAcg, staticFn(nil, nil))
	n := newFuncSource(SourceNyaa, staticFn(nil, nil))

	r := NewRegistry(g, a, n)
	got := r.Sources()
	require.Len(t, got, 3)
	assert.Equal(t, SourceGarden, got[0].Name())
	assert.Equal(t, SourceAcg, got[1].Name())
	assert.Equal(t, SourceNyaa, got[2].Name())

	// Register appends last.
	extra := newFuncSource(Source("extra"), staticFn(nil, nil))
	r.Register(extra)
	got = r.Sources()
	require.Len(t, got, 4)
	assert.Equal(t, Source("extra"), got[3].Name())

	// Sources() returns a copy — mutating it must not affect the registry.
	got[0] = extra
	assert.Equal(t, SourceGarden, r.Sources()[0].Name(),
		"Sources() must return a defensive copy")
}

func TestRegistry_ReplaceByName_PreservesPosition(t *testing.T) {
	t.Parallel()

	r := NewRegistry(
		newFuncSource(SourceGarden, staticFn(nil, nil)),
		newFuncSource(SourceAcg, staticFn(nil, nil)),
		newFuncSource(SourceNyaa, staticFn(nil, nil)),
	)

	// Replace the middle source; order must be unchanged and the swap
	// must report a hit.
	replacement := newFuncSource(SourceAcg, staticFn([]TorrentItem{stubItem(SourceAcg)}, nil))
	require.True(t, r.replaceByName(replacement))

	got := r.Sources()
	assert.Equal(t, SourceGarden, got[0].Name())
	assert.Equal(t, SourceAcg, got[1].Name(), "replaced source keeps its position")
	assert.Equal(t, SourceNyaa, got[2].Name())

	// A name not present is a no-op miss.
	assert.False(t, r.replaceByName(newFuncSource(Source("nope"), staticFn(nil, nil))))
	assert.Len(t, r.Sources(), 3)
}

// ---------------------------------------------------------------------------
// Helpers — 逐字取自 aggregator_test.go（aggregator 本体未搬入参照组）
// ---------------------------------------------------------------------------

// stubItem builds a single TorrentItem for a given source — tagged
// with the source name in the title so the merge-order assertion can
// inspect the result slice without depending on any other field.
func stubItem(src Source) TorrentItem {
	title := "stub-" + string(src)
	return TorrentItem{
		Title:  title,
		Magnet: "magnet:?xt=" + string(src),
		Size:   "1 KB",
		Source: src,
	}
}

// staticFn returns a fetchFn that always returns the given items and
// nil error.  Tracks invocation count via callCount.
func staticFn(items []TorrentItem, callCount *atomic.Int32) fetchFn {
	return func(_ context.Context, _ string) ([]TorrentItem, error) {
		if callCount != nil {
			callCount.Add(1)
		}
		return items, nil
	}
}
