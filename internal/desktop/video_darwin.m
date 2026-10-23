//go:build darwin && cgo && libmpv

// 画面层：mpv 的 render API 画进 CAOpenGLLayer（同 IINA 的 ViewLayer、mpv 自己的 gl_layer.swift），
// 鼠标键盘由宿主转成 mpv 命令 —— render API 没有输入（client.h 193-196）。坐标照 mpv 的
// video/out/mac/view.swift（backing 像素、y 从上往下），键表与滚轮照 osdep/mac/input_helper.swift。
//
// 现在在主线程渲染：原型实测 1080p24 零丢帧。IINA 把渲染挪到了串行队列（主线程画会让它的侧栏
// 动画卡），我们没有那种动画；真要挪，照它 ViewLayer.update 的做法。主线程同时是渲染线程，
// 所以这里对 mpv 只用 *_async 调用（render.h 58-90）。
#import <QuartzCore/QuartzCore.h>
#import <OpenGL/OpenGL.h>
#import <OpenGL/gl3.h>
#include <dlfcn.h>
#include "desktop_internal_darwin.h"

// OpenGL 自 10.14 起标记废弃但仍可用；render API 只有 OpenGL（没有 Metal）。
#pragma clang diagnostic ignored "-Wdeprecated-declarations"

// 测试构建（desktoptest）把它置真：合成的鼠标事件进不了 WindowServer 的拖窗（它会等一个真的松开）。
BOOL gTestSyntheticDrag;
// 测试构建读的计数
long gFramesRendered, gWindowDrags;
// 测试构建（desktoptest，NAGARE_DESKTOP_TEST_SOFTWARE_GL）才会置的：1 = 没有可用的硬件 OpenGL 时退到苹果的
// 软件渲染器（GitHub 的 macOS runner 有时没有 GPU），2 = 直接用软件渲染器（在有显卡的机器上复现那条路）。
// 正式构建里恒为 0：用户的 Mac 都有显卡，软件渲染器只会让画面慢到不能看。
int gTestSoftwareGL;
BOOL gSoftwareGL; // 实际拿到的是软件渲染器

static mpv_render_context *gRender;
static CGLContextObj gCGL;
static CGLPixelFormatObj gPF;
static NSEvent *gDown;        // 最近一次左键按下（拖窗要从它开始）
static NSTimer *gCursorTimer; // 光标自动隐藏（render API 下 mpv 的 cursor-autohide 不起作用，由宿主做）

static const NSTimeInterval kCursorHideAfter = 1.0; // 同 mpv.conf 的 cursor-autohide=1000
static const CGFloat kDragThreshold = 3;            // 按下后移动超过这么多点才算拖窗

// ───────────────────────── OpenGL ─────────────────────────

static void *getProc(void *ctx, const char *name) {
  static void *lib;
  if (!lib) lib = dlopen("/System/Library/Frameworks/OpenGL.framework/OpenGL", RTLD_LAZY | RTLD_LOCAL);
  return lib ? dlsym(lib, name) : NULL;
}

// 像素格式与上下文要在图层第一次要它们之前就有：CAOpenGLLayer 只在第一次显示时问一次，那时拿到
// NULL 就再也不画了（实测：画面层建好、会话起来之后才建，mpv 一直报 render() not being called）。
static BOOL ensureGL(NSString **err) {
  if (gCGL) return YES;
  CGLPixelFormatAttribute attrs[] = {
      kCGLPFAOpenGLProfile, (CGLPixelFormatAttribute)kCGLOGLPVersion_3_2_Core,
      kCGLPFAAccelerated,   kCGLPFADoubleBuffer, kCGLPFAAllowOfflineRenderers, 0};
  // 只有测试构建会用到：苹果的软件渲染器（同 scripts/macos/libmpv/tests/embed_test.c 的退路）
  CGLPixelFormatAttribute software[] = {
      kCGLPFAOpenGLProfile, (CGLPixelFormatAttribute)kCGLOGLPVersion_3_2_Core,
      kCGLPFARendererID,    (CGLPixelFormatAttribute)kCGLRendererGenericFloatID, kCGLPFADoubleBuffer, 0};
  GLint n = 0;
  CGLError e = kCGLBadPixelFormat;
  if (gTestSoftwareGL != 2) e = CGLChoosePixelFormat(attrs, &gPF, &n);
  if ((e != kCGLNoError || !gPF) && gTestSoftwareGL) {
    gPF = NULL;
    e = CGLChoosePixelFormat(software, &gPF, &n);
    gSoftwareGL = e == kCGLNoError && gPF;
    if (gSoftwareGL) nagareLog(@"测试构建：没有可用的硬件 OpenGL，用软件渲染器");
  }
  if (e != kCGLNoError || !gPF) {
    if (err) *err = [NSString stringWithFormat:@"选不到 OpenGL 像素格式（%d）", e];
    return NO;
  }
  e = CGLCreateContext(gPF, NULL, &gCGL);
  if (e != kCGLNoError) {
    if (err) *err = [NSString stringWithFormat:@"建不了 OpenGL 上下文（%d）", e];
    gCGL = NULL;
    return NO;
  }
  return YES;
}

@interface NagareMPVLayer : CAOpenGLLayer
@end

@implementation NagareMPVLayer
- (CGLPixelFormatObj)copyCGLPixelFormatForDisplayMask:(uint32_t)mask {
  ensureGL(NULL);
  return gPF ? CGLRetainPixelFormat(gPF) : [super copyCGLPixelFormatForDisplayMask:mask];
}
- (CGLContextObj)copyCGLContextForPixelFormat:(CGLPixelFormatObj)pf {
  ensureGL(NULL);
  return gCGL ? CGLRetainContext(gCGL) : [super copyCGLContextForPixelFormat:pf];
}
- (BOOL)canDrawInCGLContext:(CGLContextObj)ctx
                pixelFormat:(CGLPixelFormatObj)pf
               forLayerTime:(CFTimeInterval)t
                displayTime:(const CVTimeStamp *)ts {
  if (gRender) mpv_render_context_update(gRender);
  return YES;
}
- (void)drawInCGLContext:(CGLContextObj)ctx
             pixelFormat:(CGLPixelFormatObj)pf
            forLayerTime:(CFTimeInterval)t
             displayTime:(const CVTimeStamp *)ts {
  GLint fbo = 0, vp[4] = {0};
  glGetIntegerv(GL_DRAW_FRAMEBUFFER_BINDING, &fbo);
  glGetIntegerv(GL_VIEWPORT, vp);
  if (gRender) {
    mpv_opengl_fbo f = {.fbo = fbo, .w = vp[2], .h = vp[3], .internal_format = 0};
    int flip = 1;
    mpv_render_param p[] = {{MPV_RENDER_PARAM_OPENGL_FBO, &f}, {MPV_RENDER_PARAM_FLIP_Y, &flip}, {0}};
    mpv_render_context_render(gRender, p);
    gFramesRendered++;
  } else {
    glClearColor(0, 0, 0, 1);
    glClear(GL_COLOR_BUFFER_BIT);
  }
  [super drawInCGLContext:ctx pixelFormat:pf forLayerTime:t displayTime:ts];
  if (gRender) mpv_render_context_report_swap(gRender);
}
@end

// mpv 的任意线程调：切到主线程，Core Animation 随后调 drawInCGLContext
static void onRenderUpdate(void *ctx) {
  dispatch_async(dispatch_get_main_queue(), ^{
    [gVideo.layer setNeedsDisplay];
  });
}

BOOL nagareUsesSoftwareGL(void) {
  ensureGL(NULL);
  return gSoftwareGL;
}

BOOL nagareCreateRenderContext(mpv_handle *h, NSString **err) {
  if (!ensureGL(err)) return NO;
  CGLLockContext(gCGL);
  CGLSetCurrentContext(gCGL);
  mpv_opengl_init_params gl = {.get_proc_address = getProc};
  mpv_render_param params[] = {
      {MPV_RENDER_PARAM_API_TYPE, (void *)MPV_RENDER_API_TYPE_OPENGL},
      {MPV_RENDER_PARAM_OPENGL_INIT_PARAMS, &gl},
      {0}};
  int r = mpv_render_context_create(&gRender, h, params);
  CGLSetCurrentContext(NULL);
  CGLUnlockContext(gCGL);
  if (r < 0) {
    gRender = NULL;
    *err = [NSString stringWithFormat:@"建不了 mpv 渲染上下文：%s", mpv_error_string(r)];
    return NO;
  }
  mpv_render_context_set_update_callback(gRender, onRenderUpdate, NULL);
  return YES;
}

void nagareFreeRenderContext(void) {
  if (!gRender) return;
  CGLLockContext(gCGL);
  CGLSetCurrentContext(gCGL);
  mpv_render_context_free(gRender);
  CGLSetCurrentContext(NULL);
  CGLUnlockContext(gCGL);
  gRender = NULL;
  [gVideo.layer setNeedsDisplay];
}

// ───────────────────────── 键表 ─────────────────────────

// macOS 虚拟键码 → mpv 键名（osdep/mac/input_helper.swift 的 keymap；数字是 HIToolbox 的 kVK_*）。
// 其余键用 charactersIgnoringModifiers 的字符本身。
static NSString *specialKeyName(unsigned short code) {
  switch (code) {
    case 36: return @"ENTER";
    case 53: return @"ESC";
    case 51: return @"BS";
    case 48: return @"TAB";
    case 49: return @"SPACE";
    case 72: return @"VOLUME_UP";
    case 73: return @"VOLUME_DOWN";
    case 74: return @"MUTE";
    case 126: return @"UP";
    case 125: return @"DOWN";
    case 123: return @"LEFT";
    case 124: return @"RIGHT";
    case 114: return @"INS";  // kVK_Help：Mac 键盘上 Insert 的位置
    case 117: return @"DEL";
    case 115: return @"HOME";
    case 119: return @"END";
    case 116: return @"PGUP";
    case 121: return @"PGDWN";
    case 122: return @"F1";
    case 120: return @"F2";
    case 99: return @"F3";
    case 118: return @"F4";
    case 96: return @"F5";
    case 97: return @"F6";
    case 98: return @"F7";
    case 100: return @"F8";
    case 101: return @"F9";
    case 109: return @"F10";
    case 103: return @"F11";
    case 111: return @"F12";
    case 105: return @"F13";
    case 107: return @"F14";
    case 113: return @"F15";
    case 106: return @"F16";
    case 64: return @"F17";
    case 79: return @"F18";
    case 80: return @"F19";
    case 90: return @"F20";
    case 69: return @"KP_ADD";
    case 78: return @"KP_SUBTRACT";
    case 67: return @"KP_MULTIPLY";
    case 75: return @"KP_DIVIDE";
    case 76: return @"KP_ENTER";
    case 65: return @"KP_DEC";
    case 82: return @"KP0";
    case 83: return @"KP1";
    case 84: return @"KP2";
    case 85: return @"KP3";
    case 86: return @"KP4";
    case 87: return @"KP5";
    case 88: return @"KP6";
    case 89: return @"KP7";
    case 91: return @"KP8";
    case 92: return @"KP9";
    default: return nil;
  }
}

// 修饰键前缀。字符键带 Shift 也照写：mpv 解析键名时会把「Shift+n」规整成「N」（keycodes.c 的
// mp_normalize_keycode），与它自己处理 macOS 键盘时一样。
static NSString *modifierPrefix(NSEventModifierFlags m) {
  NSMutableString *s = [NSMutableString string];
  if (m & NSEventModifierFlagShift) [s appendString:@"Shift+"];
  if (m & NSEventModifierFlagControl) [s appendString:@"Ctrl+"];
  if (m & NSEventModifierFlagOption) [s appendString:@"Alt+"];
  if (m & NSEventModifierFlagCommand) [s appendString:@"Meta+"];
  return s;
}

static NSString *keyName(NSEvent *e) {
  NSString *base = specialKeyName(e.keyCode);
  if (!base) {
    NSString *chars = e.charactersIgnoringModifiers;
    if (chars.length == 0) return nil;  // 死键（组合输入的前半）：先不处理
    base = [chars substringWithRange:[chars rangeOfComposedCharacterSequenceAtIndex:0]];
    unichar c = [base characterAtIndex:0];
    // 控制字符与 AppKit 给功能键的私用区字符（NSUpArrowFunctionKey 之类）不是 mpv 认的键名
    if (c < 0x20 || (c >= 0xF700 && c <= 0xF8FF)) return nil;
  }
  return [modifierPrefix(e.modifierFlags) stringByAppendingString:base];
}

// 与 mpv v0.41.0 自己的 macOS 前端一致（osdep/mac/input_helper.swift 228-229：3 → MBTN_FORWARD、
// 4 → MBTN_BACK）。别的应用多半把 3 当「后退」，这里照 mpv：同一份 input.conf 在命令行 mpv 与桌面窗口里
// 意思一样。
static NSString *mouseButtonName(NSInteger button) {
  switch (button) {
    case 0: return @"MBTN_LEFT";
    case 1: return @"MBTN_RIGHT";
    case 2: return @"MBTN_MID";
    case 3: return @"MBTN_FORWARD";
    case 4: return @"MBTN_BACK";
    default: return button >= 5 && button <= 15 ? [NSString stringWithFormat:@"MBTN%ld", (long)(button + 4)] : nil;
  }
}

// ───────────────────────── 画面视图 ─────────────────────────

@interface NagareMPVView : NSView
@property(nonatomic, strong) NSTrackingArea *area;
@end

@implementation NagareMPVView

- (BOOL)isOpaque {
  return YES;
}
- (BOOL)acceptsFirstResponder {
  return YES;
}
- (BOOL)acceptsFirstMouse:(NSEvent *)e {
  return YES;
}
// 按下不交给窗口去拖：拖不拖由 Lua 的 capture 决定（mouseDragged）
- (BOOL)mouseDownCanMoveWindow {
  return NO;
}

- (CALayer *)makeBackingLayer {
  NagareMPVLayer *l = [NagareMPVLayer layer];
  l.asynchronous = NO;
  l.needsDisplayOnBoundsChange = YES;
  l.backgroundColor = NSColor.blackColor.CGColor;
  return l;
}

- (void)updateTrackingAreas {
  if (self.area) [self removeTrackingArea:self.area];
  self.area = [[NSTrackingArea alloc]
      initWithRect:self.bounds
           options:NSTrackingActiveAlways | NSTrackingMouseEnteredAndExited | NSTrackingMouseMoved |
                   NSTrackingInVisibleRect | NSTrackingEnabledDuringMouseDrag
             owner:self
          userInfo:nil];
  [self addTrackingArea:self.area];
  [super updateTrackingAreas];
}

- (void)syncScale {
  if (self.window) self.layer.contentsScale = self.window.backingScaleFactor;
}
- (void)viewDidMoveToWindow {
  [super viewDidMoveToWindow];
  [self syncScale];
}
- (void)viewDidChangeBackingProperties {
  [super viewDidChangeBackingProperties];
  [self syncScale];
  nagarePublishHidpi();
}

- (BOOL)pointerInside {
  NSPoint p = [self convertPoint:[self.window mouseLocationOutsideOfEventStream] fromView:nil];
  return [self mouse:p inRect:self.bounds];
}

// 光标动了就露出来，1 秒不动且还在画面上就藏起来
- (void)pokeCursor {
  [NSCursor setHiddenUntilMouseMoves:NO];
  [gCursorTimer invalidate];
  __weak NagareMPVView *weakSelf = self;
  gCursorTimer = [NSTimer scheduledTimerWithTimeInterval:kCursorHideAfter
                                                 repeats:NO
                                                   block:^(NSTimer *t) {
                                                     NagareMPVView *v = weakSelf;
                                                     if (v && gPlaying && !v.hidden && v.window.isKeyWindow && [v pointerInside]) {
                                                       [NSCursor setHiddenUntilMouseMoves:YES];
                                                     }
                                                   }];
}

// 位置换成 backing 像素、原点在左上（mpv 的 OSD 坐标）
- (void)sendPosition:(NSEvent *)e {
  NSPoint p = [self convertPoint:e.locationInWindow fromView:nil];
  NSPoint b = [self convertPointToBacking:p];
  NSSize bs = [self convertSizeToBacking:self.bounds.size];
  nagareCommand(@[ @"mouse", [@(lround(b.x)) stringValue], [@(lround(bs.height - b.y)) stringValue] ]);
  [self pokeCursor];
}

- (void)mouseEntered:(NSEvent *)e {
  nagareCommand(@[ @"keypress", @"MOUSE_ENTER" ]);
}
- (void)mouseExited:(NSEvent *)e {
  nagareCommand(@[ @"keypress", @"MOUSE_LEAVE" ]);
  [NSCursor setHiddenUntilMouseMoves:NO];
}
- (void)mouseMoved:(NSEvent *)e {
  [self sendPosition:e];
}
- (void)rightMouseDragged:(NSEvent *)e {
  [self sendPosition:e];
}
- (void)otherMouseDragged:(NSEvent *)e {
  [self sendPosition:e];
}

// 左键按下后拖动：光标下是 Lua 界面能点的东西（capture）就交给 mpv（拖进度条、拖音量）；
// 否则超过阈值就开始拖窗口 —— mpv 自己的拖窗在 render API 下没人接（VOCTRL_BEGIN_DRAGGING）。
- (void)mouseDragged:(NSEvent *)e {
  if (!gCapture && gDown) {
    NSPoint a = gDown.locationInWindow, b = e.locationInWindow;
    if (hypot(a.x - b.x, a.y - b.y) > kDragThreshold) {
      NSEvent *down = gDown;
      gDown = nil;
      nagareCommand(@[ @"keyup", @"MBTN_LEFT" ]);
      gWindowDrags++;
      if (!gTestSyntheticDrag) [self.window performWindowDragWithEvent:down];
      return;
    }
  }
  [self sendPosition:e];
}

// 双击不用特别处理：两次按下间隔够短，mpv 自己合成 MBTN_LEFT_DBL（input.c 805-815）
- (void)mouseDown:(NSEvent *)e {
  [self sendPosition:e];
  gDown = e;
  nagareCommand(@[ @"keydown", @"MBTN_LEFT" ]);
}
- (void)mouseUp:(NSEvent *)e {
  [self sendPosition:e];
  if (gDown) nagareCommand(@[ @"keyup", @"MBTN_LEFT" ]);
  gDown = nil;
}
- (void)rightMouseDown:(NSEvent *)e {
  [self sendPosition:e];
  nagareCommand(@[ @"keydown", @"MBTN_RIGHT" ]);
}
- (void)rightMouseUp:(NSEvent *)e {
  [self sendPosition:e];
  nagareCommand(@[ @"keyup", @"MBTN_RIGHT" ]);
}
- (void)otherMouseDown:(NSEvent *)e {
  NSString *b = mouseButtonName(e.buttonNumber);
  if (!b) return;
  [self sendPosition:e];
  nagareCommand(@[ @"keydown", b ]);
}
- (void)otherMouseUp:(NSEvent *)e {
  NSString *b = mouseButtonName(e.buttonNumber);
  if (b) nagareCommand(@[ @"keyup", b ]);
}

// 滚轮：触控板这类精确滚动按 delta 缩放（keypress 的 scale，同 mpv 的 mp_input_put_wheel），
// 普通滚轮一格一次
- (void)scrollWheel:(NSEvent *)e {
  BOOL precise = e.hasPreciseScrollingDeltas;
  BOOL shift = (e.modifierFlags & NSEventModifierFlagShift) != 0;
  CGFloat dx = precise ? e.deltaX * 0.1 : (shift ? e.scrollingDeltaY : e.scrollingDeltaX);
  CGFloat dy = precise ? e.deltaY * 0.1 : (shift ? e.scrollingDeltaX : e.scrollingDeltaY);
  NSString *key = dy > 0 ? @"WHEEL_UP" : @"WHEEL_DOWN";
  CGFloat d = dy;
  if (fabs(dx) > fabs(dy)) {
    key = dx > 0 ? @"WHEEL_LEFT" : @"WHEEL_RIGHT";
    d = dx;
  }
  if (fabs(d) < 0.0005) return;
  NSString *scale = precise ? [NSString stringWithFormat:@"%.3f", fabs(d)] : @"1";
  nagareCommand(@[ @"keypress", [modifierPrefix(e.modifierFlags) stringByAppendingString:key], scale ]);
}

// 键：按住不放的重复不转（mpv 对 keydown 自己按 input-ar-* 重复）；松开时全部松开，
// 免得按下与松开之间换了修饰键、键名对不上、mpv 以为一直按着（mpv 自己也是这么做的）。
// ⌘ 开头的菜单快捷键（⌘W、⌘Q……）由菜单先拿走，到不了这里。
//
// 「全部松开」必须写成 keyup ""：mpv 0.41 的 keyup 不带参数时键名是 NULL、直接解引用，整个 mpv
// （进程内就是整个 nagare）段错误退出 —— 实测命令行 mpv 经 IPC 发 ["keyup"] 也一样崩。
- (void)keyDown:(NSEvent *)e {
  if (e.isARepeat) return;
  NSString *k = keyName(e);
  if (k) nagareCommand(@[ @"keydown", k ]);
}
- (void)keyUp:(NSEvent *)e {
  nagareCommand(@[ @"keyup", @"" ]);
}

@end

NSView *nagareMakeVideoView(NSRect frame) {
  NSString *err = nil;
  if (!ensureGL(&err)) nagareLog(@"%@（播放时再试）", err);
  NagareMPVView *v = [[NagareMPVView alloc] initWithFrame:frame];
  v.wantsLayer = YES;
  return v;
}

void nagareVideoReset(void) {
  gDown = nil;
  [gCursorTimer invalidate];
  gCursorTimer = nil;
  [NSCursor setHiddenUntilMouseMoves:NO];
}
