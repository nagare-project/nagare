//go:build darwin && cgo && libmpv

// 网页层的导航规则（window_darwin.m 里导航策略与 target=_blank / window.open 两条路共用）：
//
//   - 窗口里只开我们自己的源（http://127.0.0.1:<端口>，与 Init 给的 origin 完全一致）；
//     target=_blank / window.open 指向它时，在这个窗口里开（不开第二个网页窗口）。
//   - http / https / mailto 交给系统默认浏览器 / 邮件应用。
//   - 其余一律拦下：data:、javascript:、blob:、about:、file:、各种应用协议……
//   - 内嵌框架（发现页的预告片 iframe）照网页自己的规矩走，不归这里管。
//
// 为什么这么严：target=_blank 要在窗口里开，只能由宿主自己 loadRequest —— 那是「嵌入方发起」的导航，
// 绕开了 WebKit 对「页面发起的顶层 data: / javascript: 导航」的拦截。评审实测旧规则（「不是外链就当自己的」）下
// data: 能整个换掉主框架、javascript: 能在应用的源里执行。
#include "desktop_internal_darwin.h"
#include "desktop_darwin.h"

static BOOL sameText(NSString *a, NSString *b) {
  return a && b && [a caseInsensitiveCompare:b] == NSOrderedSame;
}

static BOOL isOurs(NSURL *u, NSString *origin) {
  NSURL *o = [NSURL URLWithString:origin ?: @""];
  if (!u || !o.port || !u.port) return NO;
  return sameText(u.scheme, o.scheme) && sameText(u.host, o.host) && [u.port isEqualToNumber:o.port];
}

static BOOL opensOutside(NSURL *u) {
  NSString *s = u.scheme.lowercaseString;
  return s && [@[ @"http", @"https", @"mailto" ] containsObject:s];
}

NagareNav nagareNavDecision(NSURL *u, BOOL newWindow, BOOL subframe, NSString *origin) {
  if (subframe) return NagareNavAllow;
  if (isOurs(u, origin)) return newWindow ? NagareNavLoadHere : NagareNavAllow;
  if (opensOutside(u)) return NagareNavExternal;
  return NagareNavCancel;
}

int nagare_nav_decision(const char *url, int newWindow, int subframe, const char *origin) {
  NSString *s = [NSString stringWithUTF8String:url];
  NSString *o = [NSString stringWithUTF8String:origin];
  NSURL *u = s ? [NSURL URLWithString:s] : nil;
  if (!u) return NagareNavCancel;
  return nagareNavDecision(u, newWindow != 0, subframe != 0, o);
}
