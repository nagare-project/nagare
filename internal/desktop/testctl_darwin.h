//go:build darwin && cgo && libmpv && desktoptest

#ifndef NAGARE_DESKTOP_TESTCTL_DARWIN_H
#define NAGARE_DESKTOP_TESTCTL_DARWIN_H

// 测试构建的遥控（testctl_darwin.m）。任意线程可调，阻塞到主线程做完（最多 5 秒）。
void nagare_test_init(void);
int nagare_test_snap(const char *path);
void nagare_test_mouse(const char *kind, double x, double y, int clicks);
void nagare_test_key(int code, const char *chars, int mods, int up);
void nagare_test_js(const char *code);
void nagare_test_move_to_screen(int index);
char *nagare_test_state(void);

#endif
