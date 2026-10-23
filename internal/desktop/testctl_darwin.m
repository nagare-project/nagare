//go:build darwin && cgo && libmpv && desktoptest

// 只在测试构建里（-tags "libmpv desktoptest"）：给真机脚本用的遥控 —— 截自己的窗口
// （CGWindowListCreateImage 截本进程的窗口不要屏幕录制权限）、合成鼠标键盘、读窗口状态。
// 照 desktop-research 原型的 /ctl/*。正式构建里没有这个文件。
#import <WebKit/WebKit.h>
#include "desktop_internal_darwin.h"
#include "testctl_darwin.h"

extern BOOL gTestSyntheticDrag;
extern long gFramesRendered, gWindowDrags;
WKWebView *nagareWebView(void);

static void waitMain(dispatch_block_t b) {
  dispatch_semaphore_t done = dispatch_semaphore_create(0);
  nagareOnMain(^{
    b();
    dispatch_semaphore_signal(done);
  });
  dispatch_semaphore_wait(done, dispatch_time(DISPATCH_TIME_NOW, 5 * NSEC_PER_SEC));
}

void nagare_test_init(int softwareGL) {
  gTestSyntheticDrag = YES;
  gTestSoftwareGL = softwareGL;
}

int nagare_test_snap(const char *path) {
  NSString *p = @(path);
  __block int ok = 0;
  waitMain(^{
    if (!gWin) return;
    CGImageRef img = CGWindowListCreateImage(CGRectNull, kCGWindowListOptionIncludingWindow, (CGWindowID)gWin.windowNumber,
                                             kCGWindowImageBoundsIgnoreFraming | kCGWindowImageBestResolution);
    if (!img) return;
    NSBitmapImageRep *rep = [[NSBitmapImageRep alloc] initWithCGImage:img];
    ok = [[rep representationUsingType:NSBitmapImageFileTypePNG properties:@{}] writeToFile:p atomically:YES];
    CGImageRelease(img);
  });
  return ok;
}

// 坐标是内容视图左上角起的点（逻辑像素）。down / up / rdown / rup 走 [NSWindow sendEvent:]（经过窗口的
// 命中测试，标题栏那一条也算），move / drag / exit / wheel 直接交给画面视图（合成的移动事件不触发追踪区）。
void nagare_test_mouse(const char *kind, double x, double y, int clicks) {
  NSString *k = @(kind);
  waitMain(^{
    if (!gWin) return;
    NSView *root = gWin.contentView;
    NSPoint p = NSMakePoint(x, root.bounds.size.height - y);
    NSTimeInterval ts = [NSProcessInfo processInfo].systemUptime;
    NSEvent * (^mk)(NSEventType) = ^(NSEventType t) {
      return [NSEvent mouseEventWithType:t location:p modifierFlags:0 timestamp:ts windowNumber:gWin.windowNumber
                                 context:nil eventNumber:0 clickCount:clicks pressure:1];
    };
    if ([k isEqualToString:@"move"]) [gVideo mouseMoved:mk(NSEventTypeMouseMoved)];
    else if ([k isEqualToString:@"drag"]) [gVideo mouseDragged:mk(NSEventTypeLeftMouseDragged)];
    else if ([k isEqualToString:@"down"]) [gWin sendEvent:mk(NSEventTypeLeftMouseDown)];
    else if ([k isEqualToString:@"up"]) [gWin sendEvent:mk(NSEventTypeLeftMouseUp)];
    else if ([k isEqualToString:@"rdown"]) [gWin sendEvent:mk(NSEventTypeRightMouseDown)];
    else if ([k isEqualToString:@"rup"]) [gWin sendEvent:mk(NSEventTypeRightMouseUp)];
    else if ([k isEqualToString:@"enter"]) [gVideo mouseEntered:mk(NSEventTypeMouseMoved)];
    else if ([k isEqualToString:@"exit"]) [gVideo mouseExited:mk(NSEventTypeMouseMoved)];
    else if ([k hasPrefix:@"wheel"]) {
      CGEventRef ce = CGEventCreateScrollWheelEvent(NULL, kCGScrollEventUnitLine, 1, [k isEqualToString:@"wheelup"] ? 1 : -1);
      [gVideo scrollWheel:[NSEvent eventWithCGEvent:ce]];
      CFRelease(ce);
    }
  });
}

// 键：经 [NSApp sendEvent:]，与真键一样先过菜单的快捷键（⌘W、⌘Q），再到第一响应者。
// mods：1 Shift、2 Control、4 Option、8 Command
void nagare_test_key(int code, const char *chars, int mods, int up) {
  NSString *c = @(chars);
  waitMain(^{
    if (!gWin) return;
    NSEventModifierFlags f = 0;
    if (mods & 1) f |= NSEventModifierFlagShift;
    if (mods & 2) f |= NSEventModifierFlagControl;
    if (mods & 4) f |= NSEventModifierFlagOption;
    if (mods & 8) f |= NSEventModifierFlagCommand;
    NSEvent *e = [NSEvent keyEventWithType:up ? NSEventTypeKeyUp : NSEventTypeKeyDown
                                  location:NSZeroPoint
                             modifierFlags:f
                                 timestamp:[NSProcessInfo processInfo].systemUptime
                              windowNumber:gWin.windowNumber
                                   context:nil
                                characters:c
               charactersIgnoringModifiers:c
                                 isARepeat:NO
                                   keyCode:(unsigned short)code];
    [NSApp sendEvent:e];
  });
}

void nagare_test_js(const char *code) {
  NSString *c = @(code);
  waitMain(^{
    [nagareWebView() evaluateJavaScript:c completionHandler:nil];
  });
}

void nagare_test_move_to_screen(int index) {
  waitMain(^{
    NSArray<NSScreen *> *screens = NSScreen.screens;
    if (!gWin || index < 0 || index >= (int)screens.count) return;
    NSRect v = screens[index].visibleFrame;
    [gWin setFrameOrigin:NSMakePoint(NSMinX(v) + 40, NSMaxY(v) - gWin.frame.size.height - 40)];
  });
}

char *nagare_test_state(void) {
  __block NSString *json = @"{}";
  waitMain(^{
    NSMutableDictionary *d = [NSMutableDictionary dictionary];
    d[@"window"] = @(gWin != nil);
    if (gWin) {
      NSView *bar = [gWin standardWindowButton:NSWindowCloseButton].superview;
      NSURL *u = nagareWebView().URL;
      NSMutableArray *screens = [NSMutableArray array];
      for (NSScreen *s in NSScreen.screens) {
        [screens addObject:@{@"frame" : NSStringFromRect(s.frame), @"scale" : @(s.backingScaleFactor)}];
      }
      [d addEntriesFromDictionary:@{
        @"visible" : @(gWin.isVisible),
        @"key" : @(gWin.isKeyWindow),
        @"playing" : @(gPlaying),
        @"mpv" : @(gMpv != NULL),
        @"fullscreen" : @((gWin.styleMask & NSWindowStyleMaskFullScreen) != 0),
        @"zoomed" : @(gWin.isZoomed),
        @"miniaturized" : @(gWin.isMiniaturized),
        @"level" : @(gWin.level),
        @"capture" : @(gCapture),
        @"titlebarAlpha" : @(bar ? bar.alphaValue : -1),
        @"backingScale" : @(gWin.backingScaleFactor),
        @"frame" : NSStringFromRect(gWin.frame),
        @"content" : NSStringFromSize(gWin.contentView.bounds.size),
        @"title" : gWin.title ?: @"",
        @"firstResponder" : NSStringFromClass([gWin.firstResponder class]) ?: @"nil",
        @"frames" : @(gFramesRendered),
        @"drags" : @(gWindowDrags),
        @"webPath" : u.path ?: @"",  // 不带查询串（首个地址里有 token）
        @"webScheme" : u.scheme ?: @"",
        @"webTitle" : nagareWebView().title ?: @"",
        @"webHidden" : @(nagareWebView().hidden),
        @"screens" : screens,
        @"activationPolicy" : @(NSApp.activationPolicy),
        @"appActive" : @(NSApp.isActive),
        @"appHidden" : @(NSApp.isHidden),
        @"uaAppName" : nagareWebView().configuration.applicationNameForUserAgent ?: @"",
        @"mainMenu" : [[NSApp.mainMenu.itemArray valueForKey:@"title"] componentsJoinedByString:@"|"] ?: @"",
      }];
    }
    NSData *data = [NSJSONSerialization dataWithJSONObject:d options:0 error:nil];
    json = [[NSString alloc] initWithData:data encoding:NSUTF8StringEncoding];
  });
  return strdup(json.UTF8String);
}
