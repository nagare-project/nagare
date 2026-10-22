package main

import (
	"fmt"
	"log"

	"github.com/nagare-project/nagare/internal/config"
	"github.com/nagare-project/nagare/internal/httpserver"
)

// 单实例：nagare 已经在跑时又启动了一次（终端里再敲一遍、双击了另一份二进制）。新进程不起第二个服务，
// 而是把已有的那个摆到用户面前，然后退出。

// surface 是第二次启动时怎么把已有实例摆到用户面前。
type surface int

const (
	// surfaceNone：--no-browser / API-only，什么都不弹，只退出。
	surfaceNone surface = iota
	// surfaceBrowser：--no-window，用户明说要浏览器。
	surfaceBrowser
	// surfaceWindow：先请已有实例把桌面窗口提到前面；它没有窗口（浏览器模式、没有窗口的平台、
	// 旧版本）或请求失败，再开浏览器。不看这个构建自己带不带窗口 —— 窗口是已有实例的。
	surfaceWindow
)

// secondLaunchSurface 由这次启动的开关决定 surface。
func secondLaunchSurface(f flags, apiOnly bool) surface {
	switch {
	case f.noBrowser || apiOnly:
		return surfaceNone
	case f.noWindow:
		return surfaceBrowser
	default:
		return surfaceWindow
	}
}

// 这几个变量是单实例判定的注入点：测试替换它们即可覆盖各种分支组合，
// 不需要真的监听端口或写标记文件。
var (
	hasInstance       = httpserver.HasInstance
	portOccupied      = httpserver.PortOccupied
	probePort         = httpserver.Probe
	requestShowWindow = httpserver.RequestShowWindow
	openBrowserFn     = openBrowser
)

// ensureSingleInstance 处理「启动了两次」：已在运行 → 按 how 把它摆到前面并返回 true（调用方退出）。
//
// 先用实例标记文件把门（它在 0700 的配置目录里，别的 OS 用户伪造不了），再做带 token 的
// 探测：没有标记就说明我方实例没起过，此时端口上的任何人都不是我们，直接按外来处理
// —— 绝不把 token 发给它。冒充者能照抄 /api/health 的公开信封，「响应对得上」证明不了身份。
// 请已有实例显示窗口同样带 token，所以只在探测确认是我方实例（ProbeRunning）之后才发。
func ensureSingleInstance(cfg *config.Config, configDir string, how surface) (alreadyRunning bool, err error) {
	if !hasInstance(configDir) {
		if portOccupied(cfg.Port) {
			return false, rotateToken(cfg, "端口 %d 上有其他程序（本机没有我方实例在跑），已轮换鉴权 token")
		}
		return false, nil
	}
	switch probePort(cfg.Port, cfg.Token) {
	case httpserver.ProbeRunning:
		log.Printf("nagare 已在运行（端口 %d），本实例退出", cfg.Port)
		surfaceRunning(cfg, how)
		return true, nil
	case httpserver.ProbeForeign:
		// 探测已把 token 交给了对端（它没能证明自己是我们）—— 视为已泄露，立即轮换。
		return false, rotateToken(cfg, "端口 %d 被其他程序占用，已轮换鉴权 token")
	}
	return false, nil
}

// surfaceRunning 把已在运行的实例摆到前面：能提窗口就提窗口，否则开浏览器（带 token 的首启地址）。
func surfaceRunning(cfg *config.Config, how surface) {
	if how == surfaceNone {
		return
	}
	if how == surfaceWindow {
		shown, err := requestShowWindow(cfg.Port, cfg.Token)
		if err != nil {
			log.Printf("没能请已有实例显示窗口，改开浏览器：%v", err)
		}
		if shown {
			log.Print("已把已有实例的窗口提到前面")
			return
		}
	}
	openBrowserLogged(launchURL(cfg.Port, cfg.Token))
}

// rotateToken 换一个新 token 并落盘；存不下去就没法安全启动（旧 token 可能已泄露
// 给端口上那个程序），返回错误让调用方终止。
func rotateToken(cfg *config.Config, format string) error {
	log.Printf(format, cfg.Port)
	cfg.Token = config.NewToken()
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("保存轮换后的配置失败：%w", err)
	}
	return nil
}
