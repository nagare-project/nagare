package store

import (
	"slices"
	"strings"
)

// 磁力下载任务：用户点了「下载」的种子，完整下到下载目录，下完进本地媒体库。
//
// 与边下边播是两回事 —— 边下边播停播即删分片（决议 M3-4），这里存的是用户明确要留下来的东西，
// 所以任务要活过重启：没下完的下次启动接着下。旧版本读写时会丢掉这个字段（它不认识），
// 丢了的后果是没下完的任务不再续传、暂存目录留在盘上，不会损坏别的状态，所以不进 SchemaVersion。

// 下载任务的状态。
const (
	DownloadMetadata    = "metadata"    // 找分享者、等种子信息
	DownloadDownloading = "downloading" // 拿到种子信息，正在下载
	DownloadDone        = "done"        // 下完并挪进了下载目录
	DownloadFailed      = "failed"      // 不会自己恢复（私有种子、挪文件失败……），Error 写着原因
)

// Download 是一条下载任务。
type Download struct {
	// ID 是 infohash（小写十六进制），同一个种子只有一条任务。
	ID string `json:"id"`
	// Magnet / TorrentURL 二选一：续传时照原样再加一次（拿到过种子信息的还会存一份 .torrent 在暂存目录）。
	Magnet     string `json:"magnet,omitempty"`
	TorrentURL string `json:"torrentUrl,omitempty"`
	// Title 是发起时界面上的发布标题；Name 是种子信息里的名字（拿到种子信息前为空）。
	Title string `json:"title"`
	Name  string `json:"name,omitempty"`
	// Root 是发起时的下载目录：设置里改了目录，只影响之后的新任务。
	Root  string `json:"root"`
	State string `json:"state"`
	Error string `json:"error,omitempty"`
	// Size 是种子总大小（字节），拿到种子信息后才有。
	Size int64 `json:"size,omitempty"`
	// Paths 是下完挪进下载目录后的落点（种子顶层的每一项一个）。
	Paths       []string `json:"paths,omitempty"`
	AddedAt     int64    `json:"addedAt"`
	CompletedAt int64    `json:"completedAt,omitempty"`
}

func (d Download) clone() Download {
	d.Paths = append([]string(nil), d.Paths...)
	return d
}

// Active 报告任务还要不要接着下（找分享者或下载中）。
func (d Download) Active() bool {
	return d.State == DownloadMetadata || d.State == DownloadDownloading
}

// Downloads 返回全部下载任务的副本，按加入时间从新到旧。
func (s *Store) Downloads() []Download {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Download, 0, len(s.data.Downloads))
	for _, d := range s.data.Downloads {
		out = append(out, d.clone())
	}
	slices.SortStableFunc(out, func(a, b Download) int {
		if a.AddedAt != b.AddedAt {
			if a.AddedAt > b.AddedAt {
				return -1
			}
			return 1
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out
}

// Download 按 ID 读一条任务。
func (s *Store) Download(id string) (Download, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.data.Downloads[id]
	return d.clone(), ok
}

// PutDownload 新建或整条替换一条任务。
func (s *Store) PutDownload(d Download) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.Downloads == nil {
		s.data.Downloads = map[string]Download{}
	}
	s.data.Downloads[d.ID] = d.clone()
	return s.save()
}

// UpdateDownload 在锁内读改写一条任务；任务不存在时什么都不做并返回 false
// （用户删掉任务之后，后台迟到的进展不能把它写回来）。
func (s *Store) UpdateDownload(id string, mutate func(d *Download)) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.data.Downloads[id]
	if !ok {
		return false, nil
	}
	d = d.clone()
	mutate(&d)
	s.data.Downloads[id] = d
	return true, s.save()
}

// DeleteDownload 删掉一条任务记录；返回它原来在不在。
func (s *Store) DeleteDownload(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data.Downloads[id]; !ok {
		return false, nil
	}
	delete(s.data.Downloads, id)
	return true, s.save()
}
