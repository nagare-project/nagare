//go:build darwin && cgo && libmpv

// 桌面窗口：一个 NSWindow（全尺寸内容 + 播放时透明的标题栏）里两层 —— 网页（WKWebView，从
// 127.0.0.1 载入）与画面（video_darwin.m）。播放时画面盖满窗口；mpv 的会话结束（q、返回、Esc、
// 最后一集播完、关窗）就换回网页，窗口不关。
//
// 与 Lua 界面之间的约定（user-data/nagare/*）见 docs/player-ui.md「嵌进 nagare 的桌面窗口」：
// 宿主发布 embedded / titlebar-inset / hidpi，读 capture（拖不拖窗）与 shown（红绿灯跟着淡出）。
// mpv 的窗口属性（fullscreen / window-maximized / window-minimized / ontop）在 render API 下只是
// 没人读的选项：这里观察它们、落到 NSWindow 上，用户在窗口上的操作再写回去。
//
// 线程：所有 AppKit 与 mpv 句柄的操作都在主线程。Go 那边经 nagareOnMain 切过来；mpv 的唤醒回调同样
// 切过来再取事件。会话结束时宿主收到 MPV_EVENT_SHUTDOWN：在主线程释放渲染上下文、换回网页，
// mpv_destroy 放到后台（见 teardownMpv）。nagare 这边的收尾（进度落盘、OnSessionEnd）不经过这里：
// 它跟着 IPC 连接的断开走（mpv.AttachEndpoint），与命令行下用户关掉 mpv 窗口落到同一个终态。
#import <WebKit/WebKit.h>
#include "desktop_internal_darwin.h"
#include "desktop_darwin.h"

// activateIgnoringOtherApps: 在 macOS 14 标记废弃，13 上没有替代。
#pragma clang diagnostic ignored "-Wdeprecated-declarations"

mpv_handle *gMpv;
NSWindow *gWin;
NSView *gVideo;
BOOL gPlaying;
BOOL gCapture;

static WKWebView *gWeb;
static NSString *gURL, *gOrigin;
static id gController;  // 窗口与网页的 delegate（delegate 是弱引用，自己持有）
// mpv 那边最近一次的窗口状态：与 NSWindow 比对，只在不一致时动手，免得来回打乒乓
static BOOL gMpvFullscreen, gMpvMaximized, gMpvMinimized;
static BOOL gFullscreenTransition;  // 全屏动画进行中：这期间不再 toggle
static BOOL gHideAfterFullscreen;   // 全屏时关窗：先退出全屏，退完再藏
static BOOL gShown = YES;           // Lua 的控制条露着没有
static BOOL gPaused;                // 给「正在播放」用
static NSString *gMediaTitle;

static const NSTimeInterval kMainWaitSeconds = 15;  // Go 等主线程把会话建好的上限
static const NSTimeInterval kTitlebarFade = 0.25;

void nagareLog(NSString *format, ...) {
  va_list ap;
  va_start(ap, format);
  NSString *s = [[NSString alloc] initWithFormat:format arguments:ap];
  va_end(ap);
  nagareDesktopLog((char *)s.UTF8String);
}

void nagareOnMain(dispatch_block_t block) {
  if ([NSThread isMainThread]) {
    block();
    return;
  }
  NSArray *modes = @[ NSDefaultRunLoopMode, NSModalPanelRunLoopMode, NSEventTrackingRunLoopMode,
                      (__bridge NSString *)kCFRunLoopCommonModes ];
  CFRunLoopPerformBlock(CFRunLoopGetMain(), (__bridge CFTypeRef)modes, block);
  CFRunLoopWakeUp(CFRunLoopGetMain());
}

void nagareCommand(NSArray<NSString *> *args) {
  if (!gMpv) return;
  const char *argv[args.count + 1];
  for (NSUInteger i = 0; i < args.count; i++) argv[i] = args[i].UTF8String;
  argv[args.count] = NULL;
  mpv_command_async(gMpv, 0, argv);
}

// ───────────────────────── 标题栏、缩放 ─────────────────────────

static BOOL isFullscreen(void) {
  return (gWin.styleMask & NSWindowStyleMaskFullScreen) != 0;
}

// 红绿灯所在的那一层（NSTitlebarView）
static void setTitlebarAlpha(CGFloat alpha, BOOL animate) {
  NSView *bar = [gWin standardWindowButton:NSWindowCloseButton].superview;
  if (!bar) return;
  if (!animate) {
    bar.alphaValue = alpha;
    return;
  }
  [NSAnimationContext
      runAnimationGroup:^(NSAnimationContext *c) {
        c.duration = kTitlebarFade;
        bar.animator.alphaValue = alpha;
      }
      completionHandler:nil];
}

// 顶栏左边要让出的宽度（逻辑像素）：红绿灯最右边那个按钮的右缘。全屏时没有它们
static double titlebarInset(void) {
  if (isFullscreen()) return 0;
  NSButton *zoom = [gWin standardWindowButton:NSWindowZoomButton];
  if (!zoom) return 0;
  return ceil(NSMaxX([zoom convertRect:zoom.bounds toView:nil]));
}

// 异步设属性与异步命令的结果：mpv_set_property_async 对值不合法也先返回 0，真正的错误随后以
// MPV_EVENT_SET_PROPERTY_REPLY / COMMAND_REPLY 的 error 回来。每次调用带一个非零的 reply_userdata，
// 记下它对应的属性名，回来报错时写进日志（不写值：值里可能有请求头、地址）。
static NSMutableDictionary<NSNumber *, NSString *> *gPendingReplies;
static uint64_t gNextReply;

static uint64_t trackReply(const char *name) {
  if (!gPendingReplies) gPendingReplies = [NSMutableDictionary dictionary];
  uint64_t rid = ++gNextReply;
  gPendingReplies[@(rid)] = @(name);
  return rid;
}

static void untrackReply(uint64_t rid, int error) {
  if (rid == 0) return;
  NSString *name = gPendingReplies[@(rid)];
  if (!name) return;
  [gPendingReplies removeObjectForKey:@(rid)];
  if (error < 0) nagareLog(@"mpv 没接受 %@：%s", name, mpv_error_string(error));
}

// 异步设一个属性并跟踪结果（调用立即失败时当场记日志）
static int setPropertyAsync(mpv_handle *h, const char *name, mpv_format format, void *data) {
  uint64_t rid = trackReply(name);
  int r = mpv_set_property_async(h, rid, name, format, data);
  if (r < 0) untrackReply(rid, r);
  return r;
}

static void publishInset(void) {
  if (!gMpv) return;
  double v = titlebarInset();
  setPropertyAsync(gMpv, "user-data/nagare/titlebar-inset", MPV_FORMAT_DOUBLE, &v);
}

void nagarePublishHidpi(void) {
  if (!gMpv || !gWin) return;
  double v = gWin.backingScaleFactor;
  setPropertyAsync(gMpv, "user-data/nagare/hidpi", MPV_FORMAT_DOUBLE, &v);
}

static void setMpvFlag(const char *name, BOOL value) {
  if (!gMpv) return;
  int v = value ? 1 : 0;
  setPropertyAsync(gMpv, name, MPV_FORMAT_FLAG, &v);
}

// ───────────────────────── 两层切换 ─────────────────────────

static void showVideo(void) {
  gPlaying = YES;
  gWin.titlebarAppearsTransparent = YES;
  gWin.titleVisibility = NSWindowTitleHidden;
  gWeb.hidden = YES;
  gVideo.hidden = NO;
  [gWin makeFirstResponder:gVideo];
  nagareVideoReset();
  setTitlebarAlpha(1, NO);
}

static void showWeb(void) {
  gPlaying = NO;
  gCapture = NO;
  gVideo.hidden = YES;
  gWeb.hidden = NO;
  gWin.titlebarAppearsTransparent = NO;
  gWin.titleVisibility = NSWindowTitleVisible;
  gWin.title = @"Nagare";
  gWin.level = NSNormalWindowLevel;  // 「窗口置顶」只管播放的时候
  setTitlebarAlpha(1, NO);
  [gWin makeFirstResponder:gWeb];
  nagareVideoReset();
  // 回到媒体库时退出全屏：全屏是看片时的状态
  if (isFullscreen() && !gFullscreenTransition) [gWin toggleFullScreen:nil];
}

// ───────────────────────── mpv 会话 ─────────────────────────

// 收掉会话。渲染上下文在主线程释放（必须在 mpv_destroy 之前，且只能在渲染线程）；mpv_destroy 挪到后台：
// 它要等核心整个退出才返回，核心卡在 I/O 上时（比如 /Volumes 下掉线的 SMB）会一直等 —— 在主线程等就是
// 窗口冻住。全局量先清空，之后迟到的唤醒对不上 gMpv == h，直接忽略。⌘Q 时进程在系统答复之后就退了，
// 后台这一步做没做完都无所谓（核心的收尾不碰我们的状态）。
static void teardownMpv(BOOL toWeb) {
  mpv_handle *h = gMpv;
  gMpv = NULL;
  [gPendingReplies removeAllObjects];
  nagareFreeRenderContext();
  nagareMediaKeysStop();
  if (h) {
    mpv_set_wakeup_callback(h, NULL, NULL);
    dispatch_async(dispatch_get_global_queue(QOS_CLASS_UTILITY, 0), ^{
      mpv_destroy(h);
    });
  }
  if (toWeb && gWin) showWeb();
}

static void syncFullscreen(void) {
  if (!gFullscreenTransition && isFullscreen() != gMpvFullscreen) [gWin toggleFullScreen:nil];
}

static BOOL flagOf(mpv_event_property *p) {
  return p->format == MPV_FORMAT_FLAG && p->data && *(int *)p->data != 0;
}

static void handleProperty(mpv_event_property *p) {
  NSString *name = @(p->name);
  if (p->format == MPV_FORMAT_NONE) return;  // 属性还没有值（比如 Lua 还没发布）
  BOOL flag = flagOf(p);
  if ([name isEqualToString:@"user-data/nagare/capture"]) {
    gCapture = flag;
  } else if ([name isEqualToString:@"user-data/nagare/shown"]) {
    gShown = flag;
    if (gPlaying && !isFullscreen()) setTitlebarAlpha(flag ? 1 : 0, YES);
  } else if ([name isEqualToString:@"fullscreen"]) {
    gMpvFullscreen = flag;
    syncFullscreen();
  } else if ([name isEqualToString:@"window-maximized"]) {
    gMpvMaximized = flag;
    if (!isFullscreen() && flag != gWin.isZoomed) [gWin zoom:nil];
  } else if ([name isEqualToString:@"window-minimized"]) {
    gMpvMinimized = flag;
    if (flag && !gWin.isMiniaturized) [gWin miniaturize:nil];
    if (!flag && gWin.isMiniaturized) [gWin deminiaturize:nil];
  } else if ([name isEqualToString:@"ontop"]) {
    gWin.level = flag ? NSFloatingWindowLevel : NSNormalWindowLevel;
  } else if ([name isEqualToString:@"pause"]) {
    gPaused = flag;
    nagareMediaKeysUpdate(gMediaTitle, gPaused);
  } else if ([name isEqualToString:@"media-title"] && p->format == MPV_FORMAT_STRING && p->data) {
    // 标题栏在播放时藏着；这个标题给「窗口」菜单、调度中心和「正在播放」看
    NSString *t = @(*(char **)p->data);
    if (t.length) {
      gWin.title = t;
      gMediaTitle = t;
      nagareMediaKeysUpdate(gMediaTitle, gPaused);
    }
  }
}

static void drainEvents(mpv_handle *h) {
  while (gMpv && gMpv == h) {
    mpv_event *ev = mpv_wait_event(h, 0);
    switch (ev->event_id) {
      case MPV_EVENT_NONE:
        return;
      case MPV_EVENT_PROPERTY_CHANGE:
        handleProperty(ev->data);
        break;
      case MPV_EVENT_SET_PROPERTY_REPLY:
      case MPV_EVENT_COMMAND_REPLY:
        untrackReply(ev->reply_userdata, ev->error);
        break;
      case MPV_EVENT_SHUTDOWN:
        nagareLog(@"mpv 会话结束，换回网页");
        teardownMpv(YES);
        return;
      default:
        break;
    }
  }
}

// mpv 的任意线程调
static void onWakeup(void *ctx) {
  mpv_handle *h = ctx;
  nagareOnMain(^{
    drainEvents(h);
  });
}

// 一条选项交给 mpv：asProperty 为假时是 mpv_initialize 之前的 mpv_set_option*，为真时是异步设属性。
// 列表与键值列表整表设成 MPV_FORMAT_NODE（mpv 会拷一份，这里的临时数组随即释放）。
static int applyOption(mpv_handle *h, NSDictionary *o, BOOL asProperty) {
  const char *name = [o[@"name"] UTF8String];
  int kind = [o[@"kind"] intValue];
  if (kind == NAGARE_OPT_STRING) {
    const char *v = [o[@"value"] UTF8String];
    return asProperty ? setPropertyAsync(h, name, MPV_FORMAT_STRING, &v) : mpv_set_option_string(h, name, v);
  }
  NSArray<NSString *> *items = o[@"items"];
  NSArray<NSString *> *values = o[@"values"];
  NSUInteger n = items.count;
  mpv_node *nodes = calloc(n ? n : 1, sizeof(mpv_node));
  char **keys = kind == NAGARE_OPT_MAP ? calloc(n ? n : 1, sizeof(char *)) : NULL;
  for (NSUInteger i = 0; i < n; i++) {
    nodes[i].format = MPV_FORMAT_STRING;
    nodes[i].u.string = (char *)(kind == NAGARE_OPT_MAP ? values[i] : items[i]).UTF8String;
    if (keys) keys[i] = (char *)items[i].UTF8String;
  }
  mpv_node_list list = {.num = (int)n, .values = nodes, .keys = keys};
  mpv_node root = {.format = kind == NAGARE_OPT_MAP ? MPV_FORMAT_NODE_MAP : MPV_FORMAT_NODE_ARRAY, .u.list = &list};
  int r = asProperty ? setPropertyAsync(h, name, MPV_FORMAT_NODE, &root) : mpv_set_option(h, name, MPV_FORMAT_NODE, &root);
  free(nodes);
  free(keys);
  return r;
}

static void ensureWindow(void);

// 起一个会话（主线程）。成功返回 nil，失败返回说明。
static NSString *startSession(NSArray<NSDictionary *> *opts) {
  ensureWindow();
  if (gMpv) teardownMpv(NO);  // 上一个会话的 shutdown 还没取到：直接收掉
  mpv_handle *h = mpv_create();
  if (!h) return @"mpv_create 失败";
  // 窗口当前的状态先交给 mpv：否则它的初值（不全屏、不最大化）一被观察到，就会把窗口改回去
  mpv_set_option_string(h, "fullscreen", isFullscreen() ? "yes" : "no");
  mpv_set_option_string(h, "window-maximized", gWin.isZoomed ? "yes" : "no");
  for (NSDictionary *o in opts) {
    int r = applyOption(h, o, NO);
    if (r < 0) {
      mpv_destroy(h);
      return [NSString stringWithFormat:@"选项 %@ 设不上：%s", o[@"name"], mpv_error_string(r)];
    }
  }
  int r = mpv_initialize(h);
  if (r < 0) {
    mpv_destroy(h);
    return [NSString stringWithFormat:@"mpv 初始化失败：%s", mpv_error_string(r)];
  }
  gMpv = h;
  gMpvFullscreen = isFullscreen();
  gMpvMaximized = gWin.isZoomed;
  gMpvMinimized = NO;
  gCapture = NO;
  gShown = YES;
  const char *flags[] = {"fullscreen", "window-maximized", "window-minimized", "ontop", "pause",
                         "user-data/nagare/capture", "user-data/nagare/shown", NULL};
  for (int i = 0; flags[i]; i++) mpv_observe_property(h, 0, flags[i], MPV_FORMAT_FLAG);
  mpv_observe_property(h, 0, "media-title", MPV_FORMAT_STRING);
  mpv_set_wakeup_callback(h, onWakeup, h);
  // 渲染上下文还没建：这里还能用同步调用
  int yes = 1;
  mpv_set_property(h, "user-data/nagare/embedded", MPV_FORMAT_FLAG, &yes);
  publishInset();
  nagarePublishHidpi();
  NSString *err = nil;
  if (!nagareCreateRenderContext(h, &err)) {
    teardownMpv(YES);
    return err;
  }
  gPaused = NO;
  gMediaTitle = nil;
  nagareMediaKeysStart();
  showVideo();
  [gWin makeKeyAndOrderFront:nil];
  [NSApp activateIgnoringOtherApps:YES];
  return nil;
}

// 以属性设 BeforeLoad、再 loadfile（主线程，全是异步调用：渲染上下文已经在了）
static NSString *loadSession(NSArray<NSDictionary *> *opts, NSString *media) {
  if (!gMpv) return @"mpv 会话已经结束";
  for (NSDictionary *o in opts) {
    int r = applyOption(gMpv, o, YES);
    if (r < 0) return [NSString stringWithFormat:@"属性 %@ 设不上：%s", o[@"name"], mpv_error_string(r)];
  }
  if (media.length) {
    const char *argv[] = {"loadfile", media.UTF8String, "replace", NULL};
    uint64_t rid = trackReply("loadfile");
    int r = mpv_command_async(gMpv, rid, argv);
    if (r < 0) untrackReply(rid, r);
  }
  return nil;
}

// ───────────────────────── 窗口与网页的 delegate ─────────────────────────

@interface NagareWindowController : NSObject <NSWindowDelegate, WKNavigationDelegate, WKUIDelegate>
@end

// 按 nav_darwin.m 的规则处理一次导航；返回 WebKit 这一次该放行还是取消
static WKNavigationActionPolicy applyNav(NSURLRequest *req, BOOL newWindow, BOOL subframe) {
  NSURL *u = req.URL;
  switch (nagareNavDecision(u, newWindow, subframe, gOrigin)) {
    case NagareNavAllow:
      return WKNavigationActionPolicyAllow;
    case NagareNavLoadHere:  // target=_blank / window.open 指向我们自己：就在这个窗口里开
      [gWeb loadRequest:req];
      return WKNavigationActionPolicyCancel;
    case NagareNavExternal:
      [[NSWorkspace sharedWorkspace] openURL:u];
      return WKNavigationActionPolicyCancel;
    default:
      nagareLog(@"网页层拦下了一个 %@: 导航", u.scheme ?: @"(无协议)");  // 只记协议，地址里可能有东西
      return WKNavigationActionPolicyCancel;
  }
}

@implementation NagareWindowController

// 关窗口（红钮 / ⌘W）：播放中就停播（与命令行下关掉 mpv 窗口一样发 quit：进度落盘、OnSessionEnd
// 跟着 IPC 走），然后藏起窗口 —— nagare 照旧在后台，点 Dock 或托盘再打开。
- (BOOL)windowShouldClose:(NSWindow *)w {
  if (gMpv) {
    nagareLog(@"关窗口：停止播放");
    nagareCommand(@[ @"quit" ]);
  }
  if (isFullscreen()) {
    gHideAfterFullscreen = YES;
    if (!gFullscreenTransition) [gWin toggleFullScreen:nil];
  } else {
    [gWin orderOut:nil];
  }
  return NO;
}

- (void)windowWillEnterFullScreen:(NSNotification *)n {
  gFullscreenTransition = YES;
}
- (void)windowWillExitFullScreen:(NSNotification *)n {
  gFullscreenTransition = YES;
}
- (void)reportFullscreen {
  gFullscreenTransition = NO;
  BOOL is = isFullscreen();
  if (gMpv && gMpvFullscreen != is) {
    gMpvFullscreen = is;
    setMpvFlag("fullscreen", is);
  }
  publishInset();
  if (gPlaying) setTitlebarAlpha(gShown ? 1 : 0, NO);
  if (!is && gHideAfterFullscreen) {
    gHideAfterFullscreen = NO;
    [gWin orderOut:nil];
  }
}
- (void)windowDidEnterFullScreen:(NSNotification *)n {
  [self reportFullscreen];
}
- (void)windowDidExitFullScreen:(NSNotification *)n {
  [self reportFullscreen];
}
- (void)windowDidFailToEnterFullScreen:(NSWindow *)w {
  [self reportFullscreen];
}
- (void)windowDidFailToExitFullScreen:(NSWindow *)w {
  [self reportFullscreen];
}

// 缩放（双击标题栏、绿钮按住 ⌥、mpv 要求的 zoom:）在这里报给 mpv；不看 windowDidResize ——
// 缩放动画途中 isZoomed 一直是假，会和 mpv 来回打架
- (BOOL)windowShouldZoom:(NSWindow *)w toFrame:(NSRect)frame {
  BOOL target = !gWin.isZoomed;
  if (gMpv && gMpvMaximized != target) {
    gMpvMaximized = target;
    setMpvFlag("window-maximized", target);
  }
  return YES;
}
- (void)windowDidMiniaturize:(NSNotification *)n {
  if (gMpv && !gMpvMinimized) {
    gMpvMinimized = YES;
    setMpvFlag("window-minimized", YES);
  }
}
- (void)windowDidDeminiaturize:(NSNotification *)n {
  if (gMpv && gMpvMinimized) {
    gMpvMinimized = NO;
    setMpvFlag("window-minimized", NO);
  }
}
- (void)windowDidChangeBackingProperties:(NSNotification *)n {
  nagarePublishHidpi();
}
- (void)windowDidChangeScreen:(NSNotification *)n {
  nagarePublishHidpi();
}

// 网页的导航：规则见 nav_darwin.m（窗口里只开我们自己的源，外链交给默认浏览器，其余拦下）
- (void)webView:(WKWebView *)wv
    decidePolicyForNavigationAction:(WKNavigationAction *)a
                    decisionHandler:(void (^)(WKNavigationActionPolicy))decide {
  BOOL subframe = a.targetFrame && !a.targetFrame.isMainFrame;
  decide(applyNav(a.request, a.targetFrame == nil, subframe));
}

// window.open 与没先经过上面那条的 target=_blank：同一套规则，永远不开第二个网页窗口
- (WKWebView *)webView:(WKWebView *)wv
    createWebViewWithConfiguration:(WKWebViewConfiguration *)cfg
               forNavigationAction:(WKNavigationAction *)a
                    windowFeatures:(WKWindowFeatures *)f {
  applyNav(a.request, YES, NO);
  return nil;
}

- (void)webViewWebContentProcessDidTerminate:(WKWebView *)wv {
  nagareLog(@"网页进程意外退出，重新载入");
  [wv reload];
}

- (void)webView:(WKWebView *)wv didFailProvisionalNavigation:(WKNavigation *)nav withError:(NSError *)e {
  nagareLog(@"网页载入失败：%@（%ld）", e.localizedDescription, (long)e.code);
}

@end

static void ensureWindow(void) {
  if (gWin) return;
  // 托盘那边在桌面模式下已经升成常规应用（Dock 图标 + 应用菜单）；这里只兜底
  if (NSApp.activationPolicy != NSApplicationActivationPolicyRegular) {
    [NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
  }
  gController = [[NagareWindowController alloc] init];
  NSWindowStyleMask style = NSWindowStyleMaskTitled | NSWindowStyleMaskClosable | NSWindowStyleMaskMiniaturizable |
                            NSWindowStyleMaskResizable | NSWindowStyleMaskFullSizeContentView;
  gWin = [[NSWindow alloc] initWithContentRect:NSMakeRect(0, 0, 1280, 800)
                                     styleMask:style
                                       backing:NSBackingStoreBuffered
                                         defer:NO];
  gWin.title = @"Nagare";
  gWin.releasedWhenClosed = NO;
  gWin.collectionBehavior = NSWindowCollectionBehaviorFullScreenPrimary;
  gWin.tabbingMode = NSWindowTabbingModeDisallowed;
  gWin.delegate = gController;
  gWin.acceptsMouseMovedEvents = YES;
  gWin.movableByWindowBackground = NO;
  gWin.minSize = NSMakeSize(640, 400);
  gWin.backgroundColor = [NSColor colorWithSRGBRed:0.043 green:0.043 blue:0.043 alpha:1];  // 与网页的近黑底一致
  [gWin center];
  [gWin setFrameAutosaveName:@"NagareDesktopWindow"];

  NSView *root = [[NSView alloc] initWithFrame:[gWin contentRectForFrameRect:gWin.frame]];
  gWin.contentView = root;

  WKWebViewConfiguration *cfg = [[WKWebViewConfiguration alloc] init];
  cfg.mediaTypesRequiringUserActionForPlayback = WKAudiovisualMediaTypeNone;  // 发现页的预告片背景
  // 预告片的全屏按钮：elementFullscreenEnabled 是 macOS 12.3 的公开属性（构建机的 SDK 可能更旧，按运行时有没有来设）
  if ([cfg.preferences respondsToSelector:NSSelectorFromString(@"setElementFullscreenEnabled:")]) {
    [cfg.preferences setValue:@YES forKey:@"elementFullscreenEnabled"];
  }
  // 网页据此知道自己在 nagare 的窗口里（提示条说「关闭窗口」而不是「关闭标签页」）。只在 WebKit 默认的那段后面
  // 追加一个记号、不替换它：发现页的 YouTube 预告片会看它。约定见 docs/player-ui.md「宿主与网页」。
  NSString *appName = cfg.applicationNameForUserAgent;
  cfg.applicationNameForUserAgent = appName.length ? [appName stringByAppendingString:@" NagareDesktop"] : @"NagareDesktop";
  gWeb = [[WKWebView alloc] initWithFrame:root.bounds configuration:cfg];
  gWeb.navigationDelegate = gController;
  gWeb.UIDelegate = gController;
  gWeb.translatesAutoresizingMaskIntoConstraints = NO;
  [root addSubview:gWeb];
  // 网页放在标题栏下面（窗口是全尺寸内容的，画面层要盖到标题栏底下）
  NSLayoutGuide *guide = (NSLayoutGuide *)gWin.contentLayoutGuide;
  [NSLayoutConstraint activateConstraints:@[
    [gWeb.topAnchor constraintEqualToAnchor:guide.topAnchor],
    [gWeb.bottomAnchor constraintEqualToAnchor:root.bottomAnchor],
    [gWeb.leadingAnchor constraintEqualToAnchor:root.leadingAnchor],
    [gWeb.trailingAnchor constraintEqualToAnchor:root.trailingAnchor],
  ]];

  gVideo = nagareMakeVideoView(root.bounds);
  gVideo.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;
  gVideo.hidden = YES;
  [root addSubview:gVideo];

  if (gURL.length) [gWeb loadRequest:[NSURLRequest requestWithURL:[NSURL URLWithString:gURL]]];
  nagareLog(@"窗口已建好（屏幕缩放 %.1f）", gWin.backingScaleFactor);
}

// 给测试构建的遥控读网页层（testctl_darwin.m）
WKWebView *nagareWebView(void) {
  return gWeb;
}

static void bringToFront(void) {
  if (gWin.isMiniaturized) [gWin deminiaturize:nil];
  [gWin makeKeyAndOrderFront:nil];
  [NSApp activateIgnoringOtherApps:YES];
}

// ───────────────────────── C 接口 ─────────────────────────

// C 数组 → ObjC 对象（在调用方的线程上拷：Go 那边在函数返回后就释放这些内存）。
// 返回 nil 表示有字符串不是合法的 UTF-8。
static NSArray<NSDictionary *> *toArray(const nagare_mpv_opt *opts, int n) {
  NSMutableArray *out = [NSMutableArray arrayWithCapacity:n];
  for (int i = 0; i < n; i++) {
    const nagare_mpv_opt *o = &opts[i];
    NSString *name = o->name ? @(o->name) : nil;
    if (!name) return nil;
    NSMutableDictionary *d = [@{@"name" : name, @"kind" : @(o->kind)} mutableCopy];
    if (o->kind == NAGARE_OPT_STRING) {
      NSString *v = o->value ? [NSString stringWithUTF8String:o->value] : nil;
      if (!v) return nil;
      d[@"value"] = v;
    } else {
      NSMutableArray *items = [NSMutableArray array], *values = [NSMutableArray array];
      for (int j = 0; j < o->count; j++) {
        NSString *it = [NSString stringWithUTF8String:o->items[j]];
        NSString *va = o->kind == NAGARE_OPT_MAP ? [NSString stringWithUTF8String:o->values[j]] : @"";
        if (!it || !va) return nil;
        [items addObject:it];
        [values addObject:va];
      }
      d[@"items"] = items;
      d[@"values"] = values;
    }
    [out addObject:d];
  }
  return out;
}

// 在主线程跑 work 并等它做完，最多 kMainWaitSeconds。超时后 work 不会再跑（abandoned）。
static int runOnMainAndWait(NSString * (^work)(void), char **err) {
  dispatch_semaphore_t done = dispatch_semaphore_create(0);
  NSMutableDictionary *box = [NSMutableDictionary dictionary];
  nagareOnMain(^{
    @synchronized(box) {
      if (box[@"abandoned"]) return;
    }
    NSString *e = work();
    @synchronized(box) {
      if (e) box[@"error"] = e;
    }
    dispatch_semaphore_signal(done);
  });
  if (dispatch_semaphore_wait(done, dispatch_time(DISPATCH_TIME_NOW, (int64_t)(kMainWaitSeconds * NSEC_PER_SEC))) != 0) {
    @synchronized(box) {
      box[@"abandoned"] = @YES;
    }
    *err = strdup("主线程没有在限期内响应（界面卡住了？）");
    return -1;
  }
  NSString *e;
  @synchronized(box) {
    e = box[@"error"];
  }
  if (!e) return 0;
  *err = strdup(e.UTF8String);
  return -1;
}

void nagare_desktop_init(const char *url, const char *origin) {
  NSString *u = @(url), *o = @(origin);
  nagareOnMain(^{
    gURL = u;
    gOrigin = o;
  });
}

void nagare_desktop_show(void) {
  nagareOnMain(^{
    ensureWindow();
    bringToFront();
  });
}

void nagare_desktop_open(const char *url) {
  NSString *u = @(url);
  nagareOnMain(^{
    ensureWindow();
    NSURL *nu = [NSURL URLWithString:u];
    if (nu) [gWeb loadRequest:[NSURLRequest requestWithURL:nu]];
    bringToFront();
  });
}

int nagare_desktop_start(const nagare_mpv_opt *opts, int n, char **err) {
  NSArray *arr = toArray(opts, n);
  if (!arr) {
    *err = strdup("选项里有不是 UTF-8 的字符串");
    return -1;
  }
  return runOnMainAndWait(^NSString * {
    return startSession(arr);
  }, err);
}

int nagare_desktop_load(const nagare_mpv_opt *opts, int n, const char *media, char **err) {
  NSArray *arr = toArray(opts, n);
  NSString *m = [NSString stringWithUTF8String:media];
  if (!arr || !m) {
    *err = strdup("选项或媒体路径里有不是 UTF-8 的字符串");
    return -1;
  }
  return runOnMainAndWait(^NSString * {
    return loadSession(arr, m);
  }, err);
}

void nagare_desktop_abort(void) {
  nagareOnMain(^{
    teardownMpv(YES);
  });
}
