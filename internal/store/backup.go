package store

import (
	"encoding/json"
	"fmt"
	"os"
)

// backupBeforeUpgrade 在以另一个格式版本写回之前，把原来的文件留一份：
// <state>.v<原版本>.bak，0600。已有一份可用的备份就不覆盖 —— 降级再升级时，
// 最早那份才是用户真正的旧数据；空文件、断掉的链接、半截 JSON 不算可用，会被重写。
//
// 备份里去掉了 animego 会话凭证：它的用处是找回媒体库与进度，会话重新登录即可，
// 不该在退出登录之后还留着一份明文凭证。
func backupBeforeUpgrade(path string, raw []byte, from int) error {
	bak := fmt.Sprintf("%s.v%d.bak", path, from)
	if usableBackup(bak) {
		return nil
	}
	content, err := withoutSession(raw)
	if err != nil {
		return fmt.Errorf("整理状态备份 %s: %w", bak, err)
	}
	// 先写临时文件并刷盘，再改名：断电时要么没有备份（下次启动重来），要么是完整的一份
	tmp := bak + ".tmp"
	_ = os.Remove(tmp)
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("备份旧状态文件 %s: %w", bak, err)
	}
	if _, err := f.Write(content); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("写入状态备份 %s: %w", bak, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("刷盘状态备份 %s: %w", bak, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("关闭状态备份 %s: %w", bak, err)
	}
	if err := os.Rename(tmp, bak); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("备份旧状态文件 %s: %w", bak, err)
	}
	return nil
}

// usableBackup：是普通文件（不是链接）、非空、内容是完整的 JSON。
func usableBackup(path string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return false
	}
	raw, err := os.ReadFile(path)
	return err == nil && json.Valid(raw)
}

// withoutSession 去掉顶层的 animego 会话字段，其余原样保留。
func withoutSession(raw []byte) ([]byte, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	delete(doc, "animego")
	return json.MarshalIndent(doc, "", "  ")
}
