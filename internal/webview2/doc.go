// Package webview2 是 Windows 桌面窗口（internal/desktop）用的 WebView2 绑定：纯 Go、不要 cgo、不带任何 DLL。
//
// 来源：github.com/wailsapp/go-webview2 v1.0.23（commit e08e939，MIT，见同目录 LICENSE；webviewloader 是
// ISC，见 webviewloader/LICENSE）。只拷了要用的四个包，导入路径改成本仓库的：
//
//	internal/w32      → w32/
//	pkg/combridge     → combridge/
//	pkg/edge          → edge/（去掉 create_env_native.go 与测试）
//	webviewloader     → webviewloader/（去掉 native_* —— 那条路要从内存里装载 WebView2Loader.dll，用不着，
//	                    也不带 go-winloader 依赖；默认的纯 Go 装载器读注册表找已安装的运行时）
//
// 为什么拷而不是直接依赖：上游出错时直接结束进程（edge/chromium.go 的 errorCallback 无条件 os.Exit(1)，
// 还有 log.Fatal），而 nagare 同一个进程里跑着 HTTP 服务、磁力引擎与进度回写 —— WebView2 出一次错不能把
// 这些全带走，只能退回浏览器模式。上游的 README 也写明它不打算作为独立的包维护。
//
// 改动（都标了 "nagare patch"）：
//   - errorCallback 只报告，不 os.Exit；WebResourceRequested 里的 log.Fatal 换成报告；
//   - Embed（同步 + 嵌套消息循环 + 失败退出）换成异步的 Start + ReadyCallback；不再往页面注入 window.external；
//   - 控制器创建失败时立即返回（上游会接着解引用空指针）；不再关掉「跟随屏幕缩放」；
//   - 收到的网页消息不再原样回发；Navigate / Show / Hide / Focus 在控制器建好之前什么都不做；
//   - 几个没有 //go:build windows 的文件补上标签（否则别的平台上 go vet ./... 会失败）；
//   - uintptr → unsafe.Pointer 改成 vet 认可的写法；整个目录跑过 gofmt（只动空白与 import 顺序）；
//   - 删掉了拿 1 字节 Go bool 接 4 字节 BOOL 的那些 getter（ICoreWebView2Settings / ICoreWebViewSettings 的 GetIs*、
//     GetAre*，ICoreWebView2.GetContainsFullScreenElement）与把出参写到空指针上的 GetUserAgent：nagare 一个都不用，
//     留着就是踩内存的坑。要读这些值用 nagare_ext.go 里 int32 的写法；
//   - SetBackgroundColour 在控制器 / Controller2 拿不到时报错返回（上游接着解引用空指针）；
//   - edge/nagare_ext.go 是新加的：NavigationStarting（含 IsUserInitiated）、NewWindowRequested（含 IsUserInitiated）、
//     GetBrowserProcessID、Reload、Controller.Close、HasFullScreenElement、UserAgentString、RunScript；
//   - edge/nagare_contextmenu.go 是新加的：ICoreWebView2_11 的 ContextMenuRequested（虚表从 _3 一级一级接到 _11，
//     槽位有测试守着），用来只在输入框与选中的文字上留浏览器的右键菜单。
//
// 升级上游时：拿新版本的这四个目录覆盖，按上面的清单把改动重新打一遍（grep "nagare patch"）。
package webview2
