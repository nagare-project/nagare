package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// 界面地址、开浏览器、截图目录（从 main.go 挪出来的小工具）。

// launchURL 拼带 token 的首启地址：前端读取后会存入 sessionStorage 并从地址栏抹掉。
func launchURL(port int, token string) string {
	return fmt.Sprintf("http://127.0.0.1:%d/?token=%s", port, token)
}

// baseURL 是不带 token 的界面地址，给托盘 / Dock 菜单显示与复制用。
func baseURL(port int) string {
	return fmt.Sprintf("http://127.0.0.1:%d/", port)
}

// aboutURL 直接开到设置页的「关于」分区（版本、数据目录、日志位置）。
func aboutURL(port int, token string) string {
	return fmt.Sprintf("http://127.0.0.1:%d/settings?token=%s#about", port, token)
}

// openBrowserLogged 开浏览器，失败只记日志（日志里不带 URL —— 它含 token）。
func openBrowserLogged(url string) {
	if browserDisabledForTest() {
		log.Print("（测试构建）不开浏览器：NAGARE_TEST_NO_BROWSER=1")
		return
	}
	if err := openBrowserFn(url); err != nil {
		log.Printf("自动打开浏览器失败（请手动访问 http://127.0.0.1:<端口>/ 并按配置文件里的 token 登录）：%v", err)
	}
}

// openBrowser 用系统默认浏览器打开 URL。
// 不变量：url 只能是我们自己拼出来的 http://127.0.0.1:<port>/ 地址
// —— exec.Command 不经 shell，但若将来把任何外部字符串传进来，要先防参数注入。
// Start 之后在后台 Wait：open/xdg-open 几毫秒就退出，不 Wait 会留僵尸进程
// —— nagare 是常驻进程，这个进程表残留会一直挂到退出。
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	// open/xdg-open 几毫秒就退出，不 Wait 会留僵尸进程；非零退出要说一声
	// （xdg-open 找不到浏览器就是这种），否则用户只会看到「什么都没发生」。
	go func() {
		if err := cmd.Wait(); err != nil {
			log.Printf("打开浏览器的命令返回错误（请手动访问 http://127.0.0.1:<端口>/）：%v", err)
		}
	}()
	return nil
}

// screenshotDir 是播放器里截图的存放位置：系统「图片」文件夹下的 nagare 子目录（第一次截图时才创建）。
// Linux 先看 XDG_PICTURES_DIR；找不到家目录时返回空串，截图交给 mpv 的默认位置。
func screenshotDir() string {
	if dir := os.Getenv("XDG_PICTURES_DIR"); dir != "" && runtime.GOOS == "linux" {
		return filepath.Join(dir, "nagare")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, "Pictures", "nagare")
}
