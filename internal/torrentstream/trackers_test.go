package torrentstream

import (
	"fmt"
	"strings"
	"testing"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeTrackers(t *testing.T) {
	tests := []struct {
		name         string
		in           []string
		wantKept     []string
		wantRejected []string
	}{
		{
			name:     "空输入返回空切片而不是 nil",
			in:       nil,
			wantKept: []string{},
		},
		{
			name:     "四种协议都接受",
			in:       []string{"http://a.example/announce", "https://b.example/announce", "udp://c.example:1337/announce", "wss://d.example/announce"},
			wantKept: []string{"http://a.example/announce", "https://b.example/announce", "udp://c.example:1337/announce", "wss://d.example/announce"},
		},
		{
			name:     "去空白与空行",
			in:       []string{"  udp://a.example:80/announce  ", "", "   ", "\tws://b.example/announce"},
			wantKept: []string{"udp://a.example:80/announce", "ws://b.example/announce"},
		},
		{
			name:     "去重保序，只留第一次出现",
			in:       []string{"udp://a.example/announce", "udp://b.example/announce", "udp://a.example/announce"},
			wantKept: []string{"udp://a.example/announce", "udp://b.example/announce"},
		},
		{
			name:         "不支持的协议进 rejected",
			in:           []string{"udp://ok.example/announce", "file:///etc/passwd", "a.example/announce", "javascript:alert(1)", "udp://"},
			wantKept:     []string{"udp://ok.example/announce"},
			wantRejected: []string{"file:///etc/passwd", "a.example/announce", "javascript:alert(1)", "udp://"},
		},
		{
			name:     "协议大小写不敏感",
			in:       []string{"UDP://a.example/announce", "HTTPS://b.example/announce"},
			wantKept: []string{"UDP://a.example/announce", "HTTPS://b.example/announce"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kept, rejected := NormalizeTrackers(tc.in)
			require.NotNil(t, kept, "kept 永远非 nil")
			assert.Equal(t, tc.wantKept, kept)
			assert.Equal(t, tc.wantRejected, rejected)
		})
	}
}

func TestNormalizeTrackersEnforcesLimit(t *testing.T) {
	// 网上流传的「公开 tracker 大列表」动辄几千行，整份粘进来要被截断。
	in := make([]string, 0, maxTrackers+5)
	for i := 0; i < maxTrackers+5; i++ {
		in = append(in, fmt.Sprintf("udp://tracker%d.example:1337/announce", i))
	}
	kept, rejected := NormalizeTrackers(in)
	assert.Len(t, kept, maxTrackers)
	assert.Len(t, rejected, 5, "超出上限的进 rejected 而不是被静默吞掉")
	assert.Equal(t, in[maxTrackers], rejected[0], "保序：被拒的是排在后面的那些")
}

func boolPtr(b bool) *bool { return &b }

func TestTrackersForSkipsPrivateTorrents(t *testing.T) {
	configured := []string{"udp://tracker.example:1337/announce", "not-a-url"}

	tests := []struct {
		name string
		info *metainfo.Info
		want []string
	}{
		{"公开种子补 tracker", &metainfo.Info{}, []string{"udp://tracker.example:1337/announce"}},
		{"private=false 视为公开", &metainfo.Info{Private: boolPtr(false)}, []string{"udp://tracker.example:1337/announce"}},
		{"private=true 一条都不补", &metainfo.Info{Private: boolPtr(true)}, nil},
		{"拿不到 info 时按公开处理", nil, []string{"udp://tracker.example:1337/announce"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, trackersFor(tc.info, configured))
		})
	}
}

func TestTrackersForEmptyConfig(t *testing.T) {
	// 默认空：不补任何 tracker，也不硬编码任何地址。
	assert.Nil(t, trackersFor(&metainfo.Info{}, nil))
	assert.Nil(t, trackersFor(&metainfo.Info{}, []string{}))
	assert.Nil(t, trackersFor(&metainfo.Info{}, []string{"  ", "ftp://x.example/announce"}))
}

func TestNoHardcodedTrackers(t *testing.T) {
	// 红线：本体零内置 tracker。默认配置里一条地址都不能有。
	cfg := Config{}.normalized()
	assert.Empty(t, cfg.Trackers)
	for _, scheme := range supportedTrackerSchemes {
		assert.True(t, strings.HasSuffix(scheme, "://"), "协议白名单只该是 scheme 前缀，不该是具体地址")
	}
}
