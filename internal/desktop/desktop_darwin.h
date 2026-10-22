//go:build darwin && cgo && libmpv

#ifndef NAGARE_DESKTOP_DARWIN_H
#define NAGARE_DESKTOP_DARWIN_H

// 桌面窗口的 C 接口（desktop_darwin.go 调用）。任意线程可调：内部一律切到主线程做
// （AppKit 与 render API 都要求主线程；切换时连 NSTerminateLater 期间的模态 run loop 也算进去）。

enum { NAGARE_OPT_STRING = 0, NAGARE_OPT_LIST = 1, NAGARE_OPT_MAP = 2 };

// 一条 mpv 选项：mpv.LibmpvOption 的 C 形态。开关（yes / no）也走 STRING。
typedef struct {
  const char *name;
  int kind;
  const char *value;    // STRING
  const char **items;   // LIST 的各项；MAP 的键
  const char **values;  // MAP 的值
  int count;            // items / values 的个数
} nagare_mpv_opt;

// Go 侧回调（desktop_darwin.go 用 //export 提供）：写进 nagare 的日志。不得含 token 与媒体地址。
extern void nagareDesktopLog(char *msg);

// 记下界面地址与允许的源；不建窗口。
void nagare_desktop_init(const char *url, const char *origin);
// 把窗口提到前面；第一次调用时建窗口并载入 init 给的地址。
void nagare_desktop_show(void);
// 在窗口的网页层载入 url 并把窗口提到前面。
void nagare_desktop_open(const char *url);

// 窗口所在屏幕的缩放（backingScaleFactor；还没有窗口就先建好）。阻塞到主线程做完（有上限）。
// 返回 0 成功；失败时 *scale 为 1，*err 是 malloc 出来的中文说明（调用方 free）。
int nagare_desktop_backing_scale(double *scale, char **err);
// 起一个 mpv 会话：建句柄、设 opts（mpv_initialize 之前的选项）、初始化、发布 user-data、建渲染上下文、
// 换到画面层。阻塞到主线程做完（有上限）。返回 0 成功；失败时 *err 是 malloc 出来的中文说明（调用方 free）。
int nagare_desktop_start(const nagare_mpv_opt *opts, int n, char **err);
// 以属性设 opts（渲染上下文建好之后、第一个 loadfile 之前），再 loadfile media。用法同上。
int nagare_desktop_load(const nagare_mpv_opt *opts, int n, const char *media, char **err);
// 起播半路失败：收掉这个会话并换回网页。
void nagare_desktop_abort(void);

// 网页层导航规则（nav_darwin.m）的 C 入口，给测试用：返回 0 放行、1 在窗口里开、2 交给默认浏览器、3 拦下。
int nagare_nav_decision(const char *url, int newWindow, int subframe, const char *origin);

#endif
