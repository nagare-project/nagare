// macOS Dock 应用层：在 fyne systray 的菜单栏图标之上，把进程提升为常规应用。
//
// systray 已经把自己装成了 NSApplication 的 delegate（SystrayAppDelegate，见其
// systray_darwin.m），菜单栏图标的全部行为都挂在那上面，不能换掉它。这里用一个
// 转发代理接管 NSApplicationDelegate：只处理「Dock 点击重开」「Dock 右键菜单」
// 「系统请求终止」三件事，其余消息（含 applicationWillTerminate: 等 systray 自己的
// 回调）原样转发给它。
//
// 终止路径答 NSTerminateLater：⌘Q / 注销 / 关机时系统在等我们，直接答 Now 会在
// 进度回写之前被 exit()，答 Cancel 会让注销 / 关机弹「被 nagare 取消」。Go 侧收尾
// 完成后调 nagare_dock_reply_terminate 才放行。
#import <Cocoa/Cocoa.h>
#include "dock_darwin.h"

// NSUserNotification 自 10.14 起标记废弃但 macOS 15 仍可用；替代品 UNUserNotificationCenter
// 要先向用户申请授权，首次启动多弹一个系统对话框，对一条「我在后台」的提示来说得不偿失。
// 哪天真被移除，nagare_notify 只会静默不弹。整个文件按下这条警告。
#pragma clang diagnostic ignored "-Wdeprecated-declarations"

@interface NagareAppDelegate : NSObject <NSApplicationDelegate, NSUserNotificationCenterDelegate>
@property(nonatomic, strong) id<NSApplicationDelegate> inner;
@property(nonatomic, strong) NSMenu *dockMenu;
@end

@implementation NagareAppDelegate

// ── 转发：不认识的 delegate 消息全部交给 systray 的 delegate ──

- (BOOL)respondsToSelector:(SEL)sel {
  return [super respondsToSelector:sel] || [self.inner respondsToSelector:sel];
}

- (id)forwardingTargetForSelector:(SEL)sel {
  if ([self.inner respondsToSelector:sel]) {
    return self.inner;
  }
  return [super forwardingTargetForSelector:sel];
}

// ── Dock ──

// 点 Dock 图标：没有窗口可以显示，等价于「打开界面」。
- (BOOL)applicationShouldHandleReopen:(NSApplication *)sender hasVisibleWindows:(BOOL)flag {
  nagareDockOpen();
  return NO;
}

- (NSMenu *)applicationDockMenu:(NSApplication *)sender {
  return self.dockMenu;
}

- (void)openInterface:(id)sender {
  nagareDockOpen();
}

- (void)copyAddress:(id)sender {
  nagareDockCopyAddress();
}

// ── 终止 ──

- (NSApplicationTerminateReply)applicationShouldTerminate:(NSApplication *)sender {
  nagareDockTerminate();
  return NSTerminateLater;
}

- (void)replyTerminate {
  [NSApp replyToApplicationShouldTerminate:YES];
}

// ── 通知 ──

// 默认只在应用不在前台时才展示通知；刚从 Finder 双击启动时 nagare 正是前台应用，
// 首次启动那条提示会被吞掉，所以强制展示。
- (BOOL)userNotificationCenter:(NSUserNotificationCenter *)center
     shouldPresentNotification:(NSUserNotification *)notification {
  return YES;
}

@end

// delegate 在 NSApplication 上是弱引用，必须自己持有。
static NagareAppDelegate *gDelegate;

// 应用菜单（左上角）：
//   关于 Nagare            ← 系统标准面板：图标 + 版本 + 版权，全部来自 Info.plist
//   ──────────
//   http://127.0.0.1:8591/ ← 禁用行，只看
//   打开界面        ⌘O
//   复制地址        ⌘C
//   ──────────
//   退出 Nagare     ⌘Q
static NSMenu *buildAppMenu(NagareAppDelegate *d, NSString *appName, NSString *address) {
  NSMenu *appMenu = [[NSMenu alloc] initWithTitle:appName];
  NSMenuItem *about = [[NSMenuItem alloc] initWithTitle:[NSString stringWithFormat:@"关于 %@", appName]
                                                 action:@selector(orderFrontStandardAboutPanel:)
                                          keyEquivalent:@""];
  [appMenu addItem:about];
  [appMenu addItem:[NSMenuItem separatorItem]];
  NSMenuItem *addr = [[NSMenuItem alloc] initWithTitle:address action:nil keyEquivalent:@""];
  addr.enabled = NO;
  [appMenu addItem:addr];
  NSMenuItem *open = [[NSMenuItem alloc] initWithTitle:@"打开界面"
                                                action:@selector(openInterface:)
                                         keyEquivalent:@"o"];
  open.target = d;
  [appMenu addItem:open];
  NSMenuItem *copy = [[NSMenuItem alloc] initWithTitle:@"复制地址"
                                                action:@selector(copyAddress:)
                                         keyEquivalent:@"c"];
  copy.target = d;
  [appMenu addItem:copy];
  [appMenu addItem:[NSMenuItem separatorItem]];
  // terminate: 走 NSApplication → applicationShouldTerminate:，与注销 / 关机同一条路。
  NSMenuItem *quit = [[NSMenuItem alloc] initWithTitle:[NSString stringWithFormat:@"退出 %@", appName]
                                                action:@selector(terminate:)
                                         keyEquivalent:@"q"];
  [appMenu addItem:quit];
  return appMenu;
}

// Dock 右键菜单：地址（只看）+ 打开界面 + 复制地址。退出由 Dock 自己提供。
static NSMenu *buildDockMenu(NagareAppDelegate *d, NSString *address) {
  NSMenu *menu = [[NSMenu alloc] init];
  NSMenuItem *addr = [[NSMenuItem alloc] initWithTitle:address action:nil keyEquivalent:@""];
  addr.enabled = NO;
  [menu addItem:addr];
  NSMenuItem *open = [[NSMenuItem alloc] initWithTitle:@"打开界面"
                                                action:@selector(openInterface:)
                                         keyEquivalent:@""];
  open.target = d;
  [menu addItem:open];
  NSMenuItem *copy = [[NSMenuItem alloc] initWithTitle:@"复制地址"
                                                action:@selector(copyAddress:)
                                         keyEquivalent:@""];
  copy.target = d;
  [menu addItem:copy];
  return menu;
}

static void runOnMain(dispatch_block_t block) {
  if ([NSThread isMainThread]) {
    block();
  } else {
    dispatch_async(dispatch_get_main_queue(), block);
  }
}

void nagare_dock_setup(const char *appName, const char *address) {
  NSString *name = [NSString stringWithUTF8String:appName];
  NSString *addr = [NSString stringWithUTF8String:address];
  runOnMain(^{
    NagareAppDelegate *d = [[NagareAppDelegate alloc] init];
    d.inner = NSApp.delegate;
    d.dockMenu = buildDockMenu(d, addr);
    gDelegate = d;
    NSApp.delegate = d;

    NSMenu *mainMenu = [[NSMenu alloc] init];
    NSMenuItem *appItem = [[NSMenuItem alloc] init];
    [mainMenu addItem:appItem];
    appItem.submenu = buildAppMenu(d, name, addr);
    NSApp.mainMenu = mainMenu;

    // 常规应用：Dock 图标（来自 bundle 的 CFBundleIconFile）+ 左上角应用菜单。
    [NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
  });
}

void nagare_dock_reply_terminate(void) {
  NagareAppDelegate *d = gDelegate;
  if (d == nil) {
    return;
  }
  // NSTerminateLater 期间主线程跑在 NSModalPanelRunLoopMode 里，普通的
  // dispatch_async 到主队列不一定被服务；显式列出模式。
  [d performSelectorOnMainThread:@selector(replyTerminate)
                      withObject:nil
                   waitUntilDone:NO
                           modes:@[ NSModalPanelRunLoopMode, NSDefaultRunLoopMode, NSRunLoopCommonModes ]];
}

bool nagare_notify(const char *title, const char *body) {
  if ([[NSBundle mainBundle] bundleIdentifier] == nil) {
    // 裸二进制没有 bundle，通知中心不认这个进程。
    return false;
  }
  NSString *t = [NSString stringWithUTF8String:title];
  NSString *b = [NSString stringWithUTF8String:body];
  runOnMain(^{
    NSUserNotificationCenter *center = [NSUserNotificationCenter defaultUserNotificationCenter];
    if (gDelegate != nil) {
      center.delegate = gDelegate;
    }
    NSUserNotification *n = [[NSUserNotification alloc] init];
    n.title = t;
    n.informativeText = b;
    [center deliverNotification:n];
  });
  return true;
}
