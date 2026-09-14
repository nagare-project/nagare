package main

import (
	"errors"
	"log"
	"os"
	"path/filepath"

	"github.com/nagare-project/nagare/internal/tray"
)

// 「nagare 在后台运行」这件事用户看不见：界面在浏览器里，关掉标签页什么都不会提示。
// 三层提示（2026-09-14 用户定）：Dock / 托盘图标本身、首次启动一条系统通知、
// 界面顶部一条可关闭的提示条。这个文件管前两层的文案与「只在首次启动弹」的判定。

// noticeMarker 是「首次启动通知已经弹过」的标记文件名（放在配置目录）。
const noticeMarker = ".background-notice-shown"

// backgroundMode 汇总本次运行的后台形态：--no-tray 与 API-only（没有界面可开）都按无图标算。
func backgroundMode(noTray, apiOnly bool) tray.Mode {
	if noTray || apiOnly {
		return tray.ModeNone
	}
	return tray.CurrentMode()
}

// backgroundNotice 返回首次启动要弹的通知；已经弹过、或当前形态没有图标可指、
// 或标记写不进去（那就每次都弹，比永远不弹好）时按情况返回 nil / 通知。
func backgroundNotice(configDir string, mode tray.Mode) *tray.Notice {
	if mode == tray.ModeNone {
		return nil
	}
	marker := filepath.Join(configDir, noticeMarker)
	if _, err := os.Stat(marker); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		log.Printf("读取首次启动标记失败，按首次启动处理：%v", err)
	}
	if err := os.WriteFile(marker, []byte("nagare 首次启动提示已弹出\n"), 0o600); err != nil {
		log.Printf("写首次启动标记失败（下次启动会再提示一次）：%v", err)
	}
	return &tray.Notice{Title: "nagare 在后台运行中", Body: noticeBody(mode)}
}

// noticeBody 按形态告诉用户「去哪里找它、怎么退出」。
func noticeBody(mode tray.Mode) string {
	switch mode {
	case tray.ModeDock:
		return "关闭浏览器不会退出。点 Dock 图标可重新打开界面，⌘Q 或菜单栏图标可退出。"
	case tray.ModeMenuBar:
		return "关闭浏览器不会退出。从菜单栏右上角的「流」图标可重新打开界面或退出。"
	case tray.ModeTray:
		return "关闭浏览器不会退出。从系统托盘图标（可能折叠在 ^ 里）可重新打开界面或退出。"
	default:
		return "关闭浏览器不会退出。退出请到设置页，或在终端按 Ctrl+C。"
	}
}
