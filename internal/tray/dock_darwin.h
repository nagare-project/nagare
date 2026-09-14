#ifndef NAGARE_DOCK_DARWIN_H
#define NAGARE_DOCK_DARWIN_H

#include <stdbool.h>

// Go 侧回调（dock_darwin.go 用 //export 提供）。
extern void nagareDockOpen(void);
extern void nagareDockCopyAddress(void);
extern void nagareDockTerminate(void);

// 把进程从「菜单栏应用」提升为「Dock 应用」：Dock 图标 + 应用菜单 + Dock 右键菜单，
// 并接管 NSApplicationDelegate 的重开 / 终止回调（其余仍转发给 systray 的 delegate）。
// 必须在主线程、systray 就绪之后调用。
void nagare_dock_setup(const char *appName, const char *address);

// 答复系统「可以终止了」；nagare_dock_setup 之后、applicationShouldTerminate 返回
// NSTerminateLater 之后才有意义。任意线程可调。
void nagare_dock_reply_terminate(void);

// 弹一条系统通知。任意线程可调；返回 false 表示这个进程没有 bundle，通知中心不认。
bool nagare_notify(const char *title, const char *body);

#endif
