//go:build darwin && cgo && libmpv

// window_darwin.m 与 video_darwin.m 之间共用的东西。下面的全局量只在主线程读写。
#ifndef NAGARE_DESKTOP_INTERNAL_DARWIN_H
#define NAGARE_DESKTOP_INTERNAL_DARWIN_H

#import <Cocoa/Cocoa.h>
#include <mpv/client.h>
#include <mpv/render.h>
#include <mpv/render_gl.h>

extern mpv_handle *gMpv;           // 当前会话的 mpv 句柄；没有会话时为 NULL
extern NSWindow *gWin;
extern NSView *gVideo;              // 画面层（MPVView）
extern BOOL gPlaying;               // 窗口正显示画面层
extern BOOL gCapture;               // Lua 说：光标下是界面上能点的东西，这一下按下归界面

// 写进 nagare 的日志（不得含 token 与媒体地址）。
void nagareLog(NSString *format, ...) NS_FORMAT_FUNCTION(1, 2);
// 在主线程执行 block：已在主线程就直接执行，否则排进主 run loop 的常用模式与模态模式
// （⌘Q 的 NSTerminateLater 期间主线程跑在 NSModalPanelRunLoopMode，普通的 dispatch_async 不一定被服务）。
void nagareOnMain(dispatch_block_t block);
// 发一条 mpv 命令（异步：主线程也是渲染线程，只能用 *_async）。
void nagareCommand(NSArray<NSString *> *args);

// ── video_darwin.m ──
// 画面层：CAOpenGLLayer 渲染 + 把鼠标键盘转成 mpv 的 mouse / keydown / keyup / keypress 命令。
NSView *nagareMakeVideoView(NSRect frame);
// 给 h 建 OpenGL 渲染上下文；失败返回 NO 并写 *err。
BOOL nagareCreateRenderContext(mpv_handle *h, NSString **err);
// 释放渲染上下文（必须在 mpv_destroy 之前）。没有就什么都不做。
void nagareFreeRenderContext(void);
// 换到 / 离开画面层时复位输入状态（按下的键、拖窗、光标隐藏）。
void nagareVideoReset(void);

// ── media_darwin.m ──
// 媒体键（MPRemoteCommandCenter）：会话开始时注册，结束时撤掉；标题与暂停状态给「正在播放」。
void nagareMediaKeysStart(void);
void nagareMediaKeysUpdate(NSString *title, BOOL paused);
void nagareMediaKeysStop(void);

// ── nav_darwin.m ──
// 网页层的导航怎么处理：放行、在这个窗口里开（target=_blank 指向我们自己）、交给默认浏览器、拦下。
// newWindow：target=_blank / window.open；subframe：内嵌框架里的导航。
typedef NS_ENUM(int, NagareNav) { NagareNavAllow = 0, NagareNavLoadHere = 1, NagareNavExternal = 2, NagareNavCancel = 3 };
NagareNav nagareNavDecision(NSURL *u, BOOL newWindow, BOOL subframe, NSString *origin);

// ── window_darwin.m ──
// 把当前的屏幕缩放发给界面（user-data/nagare/hidpi）；换屏、改缩放时调。
void nagarePublishHidpi(void);

#endif
