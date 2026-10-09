package releasetitle

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBatchRange(t *testing.T) {
	tests := []struct {
		title string
		want  Range
		ok    bool
	}{
		{"[诸神字幕组][排球少年!!][Haikyuu!!][BDRip][01-25全][简繁日文字幕][1080P][HEVC MKV]", Range{1, 25}, true},
		{"[VCB-Studio] Haikyuu!! [1-12 Fin][Ma10p_1080p]", Range{1, 12}, true},
		{"[ANi] 某科学的超电磁炮 第二季 [13~24][1080P]", Range{13, 24}, true},
		{"【喵萌奶茶屋】★01月新番★[某作品][01-12][1080p]", Range{1, 12}, true},
		{"[Group] 某作品 [01 - 12][1080p]", Range{1, 12}, true},
		{"[Group] 某作品 第二季 全12集 [1080P]", Range{1, 12}, true},
		{"[Group] 某作品 全 26 话 [BDRip]", Range{1, 26}, true},
		// 其他括号、版本号与附加内容后缀
		{"[SubsPlease] Some Show (01-12) (1080p) [Batch]", Range{1, 12}, true},
		{"[Group] 某作品（01-12）[1080P]", Range{1, 12}, true},
		{"[Group] 某作品［01-12］", Range{1, 12}, true},
		{"[Group] Some Show [01-12+SP][1080p]", Range{1, 12}, true},
		{"[Group] Some Show [01-12v2][1080p]", Range{1, 12}, true},
		// 完结字样之后再跟版本号（2026-10-09 真实结果：被当成了第 1 集，点播放会开第一季第 1 集）
		{"【悠哈璃羽字幕社】[葬送的芙莉莲_Sousou no Frieren][01-28 END（v2）][x264 1080p][CHT]", Range{1, 28}, true},
		{"[Group] Some Show [01-28 END (v2)][1080p]", Range{1, 28}, true},
		{"[Group] Some Show [01-12 v2 Fin][1080p]", Range{1, 12}, true},
		{"[Group] Some Show [1-12+Extra]", Range{1, 12}, true},
		// 带「话 / 集」与 EP 写法
		{"[Group] 某作品 第01-12话 [1080P]", Range{1, 12}, true},
		{"[Group] 某作品 01-12集", Range{1, 12}, true},
		{"[Group] Some Show EP01-12 [1080p]", Range{1, 12}, true},
		{"Some.Show.S01E01-E12.1080p.WEB-DL", Range{1, 12}, true},
		// 不带括号、连写的区间
		{"[Group] Some Show - 01-02 [1080p]", Range{1, 2}, true},
		{"某作品 01~13 合集", Range{1, 13}, true},
		// 不是区间：年份、分辨率、单集
		{"[Sub] Show - 05 [1920x1080] [2024-2025]", Range{}, false},
		{"[ANi] 我推的孩子 - 11 [1080P][Baha][WEB-DL][AAC AVC][CHT].mp4", Range{}, false},
		// 「标题里的季数 - 集号」是单集：第三季第 10 集不是第 3–10 集的合集
		{"[Lilith-Raws] Kono Subarashii Sekai ni Shukufuku wo! 3 - 10 [Baha][WebDL 1080p AVC AAC][CHT].mp4", Range{}, false},
		{"[ANi] Spy x Family 2 - 05 [1080P]", Range{}, false},
		{"[Group] Gintama 3 - 12 [720p]", Range{}, false},
		// 季数、分卷的区间不是集号
		{"[Group] Some Show Season 1-3 [1080p]", Range{}, false},
		{"[Group] Some Show Part 1-2 [BDRip]", Range{}, false},
		{"[Group] Some Show Vol 1-6 [BD]", Range{}, false},
		// 起止相同或倒置不是合集
		{"[Group] Show [05-05]", Range{}, false},
		{"[Group] Show [12-03]", Range{}, false},
		// 只有字样、没有区间
		{"幼女战记 第二季 全集合集", Range{}, false},
	}
	for _, tc := range tests {
		got, ok := BatchRange(tc.title)
		assert.Equal(t, tc.ok, ok, tc.title)
		assert.Equal(t, tc.want, got, tc.title)
	}
}

func TestIsBatch(t *testing.T) {
	batches := []string{
		"[诸神字幕组][排球少年!!][Haikyuu!!][BDRip][01-25全][简繁日文字幕][1080P][HEVC MKV]",
		"幼女战记 第二季 全集合集",
		"[Group] 某作品 [合集][1080P]",
		"[Group] Some Show (Batch) [1080p]",
		"[Group] Some Show BD-BOX [1080p]",
		"[Group] Some Show BDBOX",
		"[Group] Some Show Complete [1080p]",
		"[Group] 某作品 全12集",
		"[Group] 某作品 全集",
	}
	for _, title := range batches {
		assert.True(t, IsBatch(title), title)
	}
	singles := []string{
		"[DBD-Raws][摇曳露营△][03][1080P][BDRip][HEVC-10bit][FLAC].mkv",
		"[ANi] 我推的孩子 - 11 [1080P][Baha][WEB-DL][AAC AVC][CHT].mp4",
		"[Group] 鬼灭之刃 全集中 - 03 [1080P]",
		"[Group] Show - 05 Completed [1080p]",
		"[Lilith-Raws] Kono Subarashii Sekai ni Shukufuku wo! 3 - 10 [Baha][WebDL 1080p AVC AAC][CHT].mp4",
		"[Group] Some Show Season 1-3 - 05 [1080p]",
		"",
	}
	for _, title := range singles {
		assert.False(t, IsBatch(title), title)
	}
}
