// Package logfile 管理 nagare 的日志文件：<configDir>/logs/nagare.log。
//
// 双击启动（Finder 里的 .app、-H windowsgui 的 exe）没有终端，这个文件是唯一的
// 诊断来源。启动时若超过 maxSize 就整体轮转成 nagare.log.1（覆盖上一份），
// 不做运行中切割 —— 常驻进程一次会话写不满 2MB，简单可靠优先。
//
// 约束：写进这里的日志不得含请求 URL 与 token（GET 的 query 可携带 token）。
package logfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	dirName  = "logs"
	fileName = "nagare.log"
	// maxSize 是触发轮转的阈值：超过即在启动时改名为 .1。
	maxSize = 2 << 20
	// rotatedSuffix 是上一份日志的后缀，只保留一代。
	rotatedSuffix = ".1"
)

// Path 返回日志文件的完整路径（不保证已创建）。
func Path(configDir string) string {
	return filepath.Join(configDir, dirName, fileName)
}

// Open 创建（或追加打开）日志文件：目录 0700，文件 0600（内容含本地路径等信息）。
// 已有文件超过 maxSize 时先轮转成 nagare.log.1 再新建。调用方负责 Close。
func Open(configDir string) (*os.File, error) {
	path := Path(configDir)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("创建日志目录 %s: %w", dir, err)
	}
	if err := rotateIfLarge(path); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("打开日志文件 %s: %w", path, err)
	}
	// OpenFile 对已存在的文件不改权限，显式收紧一次。
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("收紧日志权限 %s: %w", path, err)
	}
	return f, nil
}

// rotateIfLarge 超阈值时把 path 改名成 path.1（覆盖旧的）；不存在则什么都不做。
func rotateIfLarge(path string) error {
	st, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("检查日志大小 %s: %w", path, err)
	}
	if st.Size() <= maxSize {
		return nil
	}
	if err := os.Rename(path, path+rotatedSuffix); err != nil {
		return fmt.Errorf("轮转日志 %s: %w", path, err)
	}
	return nil
}
