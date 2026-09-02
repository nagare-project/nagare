package update

// 本文件负责 <CacheDir>/update.json 的读写。

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
)

const cacheFileName = "update.json"

// cacheFile 是 update.json 的内容。Enabled 用指针区分「从没写过」（默认开启）与显式 false。
type cacheFile struct {
	Enabled     *bool  `json:"enabled"`
	LastCheckAt int64  `json:"lastCheckAt"` // 上次成功检查的毫秒时间戳；0 = 从未
	Latest      string `json:"latest"`      // 不带 v 前缀；空 = 尚无发布
	URL         string `json:"url"`
}

func (f cacheFile) enabled() bool { return f.Enabled == nil || *f.Enabled }

// loadCache 读缓存。文件不存在按空处理；损坏或内容不合法时打一行日志并按空处理
// —— 不是硬错误，缓存丢了只是多查一次。盘上的值同样要过校验：界面会把 url 当链接渲染，
// 不信任任何非本进程刚写下的内容。
func loadCache(path, repo string) cacheFile {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cacheFile{}
	}
	if err != nil {
		log.Printf("update: 读取更新缓存失败，按空缓存处理：%v", err)
		return cacheFile{}
	}
	var f cacheFile
	if err := json.Unmarshal(raw, &f); err != nil {
		log.Printf("update: 更新缓存损坏，按空缓存处理：%v", err)
		return cacheFile{}
	}
	if f.LastCheckAt < 0 {
		f.LastCheckAt = 0
	}
	if f.Latest == "" {
		f.URL = ""
		return f
	}
	if _, err := validateRelease("update.cache", repo, "v"+f.Latest, f.URL); err != nil {
		// 只保留开关；版本与时间戳一起丢掉，否则会拿空结果冒充 24 小时内的有效检查。
		log.Printf("update: 更新缓存内容不合法，忽略其中的版本信息：%v", err)
		return cacheFile{Enabled: f.Enabled}
	}
	return f
}

// saveCache 原子写盘：先写 .tmp 再 rename，文件 0600、目录 0700（与 config.Save 同一套做法）。
// enabled 总是显式写出，读回时不依赖「缺省即开启」。
func saveCache(path string, f cacheFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("创建缓存目录: %w", err)
	}
	enabled := f.enabled()
	out := cacheFile{Enabled: &enabled, LastCheckAt: f.LastCheckAt, Latest: f.Latest, URL: f.URL}
	buf, err := json.Marshal(out)
	if err != nil {
		return fmt.Errorf("序列化更新缓存: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf, 0o600); err != nil {
		return fmt.Errorf("写入临时缓存: %w", err)
	}
	// WriteFile 对已存在的文件不会改权限，显式收紧一次。
	if err := os.Chmod(tmp, 0o600); err != nil {
		return fmt.Errorf("收紧缓存权限: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("替换缓存文件: %w", err)
	}
	return nil
}
