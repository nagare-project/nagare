package api

import (
	"log"
	"path/filepath"
	"strings"
)

// IncludeDownloads 在一条下载完成后让它出现在媒体库里：下载目录已经是（或在）某个库目录里就重扫，
// 否则把它加成库目录（加的时候会扫一遍）。在下载器的后台 goroutine 里调。
//
// 它只碰媒体库，所以留在 api；磁力下载的端点在 api/sources（只有桌面版注册）。
func (s *LibraryService) IncludeDownloads(root string) {
	root = filepath.Clean(root)
	for _, folder := range s.st.Snapshot().Folders {
		if within(root, filepath.Clean(folder.Path)) {
			s.Rescan()
			return
		}
	}
	if _, _, err := s.AddFolder(root); err != nil {
		log.Printf("library: 把下载目录加进媒体库失败：%v", err)
		s.Rescan()
	}
}

// within 报告 path 是不是 base 本身或在 base 里面。
func within(path, base string) bool {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
