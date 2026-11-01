package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nagare-project/nagare/internal/store"
)

// 媒体库与作品关联的黄金用例。

func libraryGoldenCases() []goldenCase {
	return []goldenCase{
		{"library-empty", func(t *testing.T) []byte {
			env := newGoldenEnv(t, goldenOptions{})
			return env.call(t, http.StatusOK, http.MethodGet, "/api/library", "")
		}},
		{"library-populated", func(t *testing.T) []byte {
			env := newGoldenLibrary(t)
			return env.call(t, http.StatusOK, http.MethodGet, "/api/library", "")
		}},
		{"library-folder-add", func(t *testing.T) []byte {
			env := newGoldenEnv(t, goldenOptions{})
			root := filepath.Join(t.TempDir(), "anime")
			writeGoldenMedia(t, root, "葬送的芙莉莲/[Sub] Frieren - 01 [1080p].mkv", 1<<20)
			body := env.call(t, http.StatusOK, http.MethodPost, "/api/library/folders", `{"path":`+jsonString(t, root)+`}`)
			// 目录 ID 与添加时间由 store 生成（随机 / 当前时间），没有注入点：比对前换成固定值
			folder := env.store.Snapshot().Folders[0]
			env.scrub = []string{root, goldenMediaRoot, folder.ID, "f-0a1b2c3d"}
			body = env.scrubBytes(body)
			return []byte(replaceNumber(t, string(body), "addedAt", folder.AddedAt, goldenNow.UnixMilli()))
		}},
		{"library-rescan", func(t *testing.T) []byte {
			env := newGoldenLibrary(t)
			return env.call(t, http.StatusOK, http.MethodPost, "/api/library/rescan", "")
		}},
		{"library-folder-remove-not-found", func(t *testing.T) []byte {
			env := newGoldenEnv(t, goldenOptions{})
			return env.call(t, http.StatusNotFound, http.MethodDelete, "/api/library/folders/f-missing", "")
		}},
		{"library-association-manual", func(t *testing.T) []byte {
			env := newGoldenLibrary(t)
			key := env.clusterKey(t, "Bocchi")
			return env.call(t, http.StatusOK, http.MethodPut, "/api/library/association",
				`{"clusterKey":`+jsonString(t, key)+`,"mode":"manual","anilistId":130003}`)
		}},
		{"library-association-auto", func(t *testing.T) []byte {
			env := newGoldenLibrary(t)
			key := env.clusterKey(t, "Frieren")
			return env.call(t, http.StatusOK, http.MethodPut, "/api/library/association",
				`{"clusterKey":`+jsonString(t, key)+`,"mode":"auto"}`)
		}},
		{"library-association-dismiss-synced", func(t *testing.T) []byte {
			env := newGoldenLibrary(t)
			key := env.clusterKey(t, "Frieren")
			return env.call(t, http.StatusOK, http.MethodPost, "/api/library/association/dismiss-synced",
				`{"clusterKey":`+jsonString(t, key)+`,"anilistId":21}`)
		}},
		{"fs-dirs", func(t *testing.T) []byte {
			env := newGoldenEnv(t, goldenOptions{})
			root := filepath.Join(t.TempDir(), "anime")
			for _, dir := range []string{"葬送的芙莉莲", "Bocchi the Rock", ".hidden"} {
				require.NoError(t, os.MkdirAll(filepath.Join(root, dir), 0o755))
			}
			env.scrub = []string{root, goldenMediaRoot, filepath.Dir(root), filepath.Dir(goldenMediaRoot)}
			return env.call(t, http.StatusOK, http.MethodGet, "/api/fs/dirs?path="+root, "")
		}},
	}
}

// newGoldenLibrary 造一个扫过的媒体库：两部作品（一部手动认定、一部自动匹配）、一个没认出的分组、
// 一个失效的库目录、扫描跳过的小文件、进度、继续观看与后台识别的暂停原因。
func newGoldenLibrary(t *testing.T) *goldenEnv {
	t.Helper()
	root := filepath.Join(t.TempDir(), "anime")
	const mb = 1 << 20
	writeGoldenMedia(t, root, "葬送的芙莉莲/[Sub] Frieren - 01 [1080p].mkv", 3*mb)
	writeGoldenMedia(t, root, "葬送的芙莉莲/[Sub] Frieren - 02 [1080p].mkv", 3*mb)
	writeGoldenMedia(t, root, "葬送的芙莉莲/[Sub] Frieren - 01 [1080p].ass", 4096)
	writeGoldenMedia(t, root, "葬送的芙莉莲/[Sub] Frieren - NCOP [1080p].mkv", 2*mb)
	writeGoldenMedia(t, root, "Bocchi the Rock/[ANi] Bocchi the Rock! - 01 [1080P][Baha][WEB-DL][AAC AVC][CHT].mp4", 2*mb)
	writeGoldenMedia(t, root, "Bocchi the Rock/sample.mkv", 512)
	writeGoldenMedia(t, root, "家庭录像/2026 夏天.mp4", 2*mb)
	unplugged := filepath.Join(t.TempDir(), "unplugged")

	statePath := filepath.Join(t.TempDir(), "state.json")
	data := store.Data{SchemaVersion: store.SchemaVersion, Folders: []store.Folder{
		{ID: "f-0a1b2c3d", Path: root, AddedAt: goldenNow.Add(-48 * time.Hour).UnixMilli()},
		{ID: "f-5e6f7a8b", Path: unplugged, AddedAt: goldenNow.Add(-24 * time.Hour).UnixMilli()},
	}}
	raw, err := json.Marshal(data)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(statePath, raw, 0o600))

	env := newGoldenEnv(t, goldenOptions{statePath: statePath})
	env.scrub = []string{root, goldenMediaRoot, unplugged, "/media/unplugged"}
	env.lib.SetIdentify(nil, func() IdentifyStatus {
		return IdentifyStatus{Done: 1, Total: 3, Error: "限速中，作品识别稍后继续"}
	})
	env.lib.Rescan()

	frieren, bocchi, home := env.clusterKey(t, "Frieren"), env.clusterKey(t, "Bocchi"), env.clusterKey(t, "家庭录像")
	files := func(key string) []string {
		ids, ok := env.lib.ClusterFiles(key)
		require.True(t, ok, key)
		return ids
	}
	ms := func(d time.Duration) int64 { return goldenNow.Add(d).UnixMilli() }
	fr := files(frieren)
	require.NoError(t, env.store.SetProgress(fr[0], store.Progress{PositionSec: 1420, DurationSec: 1420, UpdatedAt: ms(-3 * time.Hour), Completed: true, Synced: true}))
	require.NoError(t, env.store.SetProgress(fr[1], store.Progress{PositionSec: 312.5, DurationSec: 1420, UpdatedAt: ms(-2 * time.Hour)}))
	for _, id := range files(bocchi) {
		require.NoError(t, env.store.SetBinding(id, store.Binding{
			AnilistID: goldenBocchiID, Episode: 1, Title: "孤独摇滚！", EpisodeTitle: "转动的星球",
			CoverURL: anilistCover("bx130003"), MatchedAt: ms(-time.Hour),
		}))
		require.NoError(t, env.store.SetProgress(id, store.Progress{PositionSec: 600, DurationSec: 1440, UpdatedAt: ms(-time.Hour)}))
	}
	require.NoError(t, env.store.UpdateAssociations(func(all map[string]store.Association) bool {
		all[frieren] = store.Association{
			Mode: store.AssociationManual, AnilistID: goldenFrierenID, Title: "葬送的芙莉莲",
			CoverURL: anilistCover("bx154587"), Members: fr, MemberCount: len(fr), SetAt: ms(-5 * time.Hour),
			SyncedElsewhere: []store.SyncedRecord{
				{AnilistID: 21, Title: "海贼王", Episodes: []int{1}},
				{AnilistID: 22, Episodes: []int{1, 2}},
			},
		}
		all[home] = store.Association{Mode: store.AssociationNone, SetAt: ms(-4 * time.Hour)}
		return true
	}))
	return env
}

// clusterKey 按标题里的一段文字找作品分组的 clusterKey。
func (e *goldenEnv) clusterKey(t *testing.T, titlePart string) string {
	t.Helper()
	for _, c := range e.lib.View().Clusters {
		for _, g := range c.Groups {
			for _, it := range g.Items {
				if containsFold(c.Title+" "+g.Label+" "+it.FileName, titlePart) {
					return c.ClusterKey
				}
			}
		}
	}
	t.Fatalf("媒体库里没有标题含「%s」的分组", titlePart)
	return ""
}
