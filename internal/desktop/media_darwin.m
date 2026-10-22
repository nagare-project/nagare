//go:build darwin && cgo && libmpv

// 媒体键（F7 / F8 / F9、耳机线控、控制中心的「正在播放」）：libmpv 默认 input-media-keys=no，
// 而且 macOS 早就不把媒体键当普通按键发给前台应用了 —— 系统交给「正在播放」的那个应用的
// MPRemoteCommandCenter（mpv 自己的 osdep/mac/remote_command_center.swift 也是这么接的）。
// 播放期间注册，按下时转成 mpv 的键：上一曲 / 下一曲是 PREV / NEXT（input.conf 绑到 nagare 的
// 上一集 / 下一集），播放暂停是 PLAYPAUSE（mpv 默认绑定 cycle pause）。会话结束就撤掉。
#import <MediaPlayer/MediaPlayer.h>
#include "desktop_internal_darwin.h"

static NSMutableArray *gTargets;  // [命令, 目标] 成对存，撤销时用

static void bind(MPRemoteCommand *cmd, NSString *key) {
  id target = [cmd addTargetWithHandler:^MPRemoteCommandHandlerStatus(MPRemoteCommandEvent *e) {
    nagareOnMain(^{
      nagareCommand(@[ @"keypress", key ]);
    });
    return MPRemoteCommandHandlerStatusSuccess;
  }];
  cmd.enabled = YES;
  [gTargets addObject:@[ cmd, target ]];
}

void nagareMediaKeysStart(void) {
  if (gTargets) return;
  gTargets = [NSMutableArray array];
  MPRemoteCommandCenter *c = [MPRemoteCommandCenter sharedCommandCenter];
  bind(c.nextTrackCommand, @"NEXT");
  bind(c.previousTrackCommand, @"PREV");
  bind(c.togglePlayPauseCommand, @"PLAYPAUSE");
  bind(c.playCommand, @"PLAY");
  bind(c.pauseCommand, @"PAUSE");
  [MPNowPlayingInfoCenter defaultCenter].playbackState = MPNowPlayingPlaybackStatePlaying;
}

void nagareMediaKeysUpdate(NSString *title, BOOL paused) {
  if (!gTargets) return;
  MPNowPlayingInfoCenter *n = [MPNowPlayingInfoCenter defaultCenter];
  if (title.length) n.nowPlayingInfo = @{MPMediaItemPropertyTitle : title};
  n.playbackState = paused ? MPNowPlayingPlaybackStatePaused : MPNowPlayingPlaybackStatePlaying;
}

void nagareMediaKeysStop(void) {
  if (!gTargets) return;
  for (NSArray *pair in gTargets) [(MPRemoteCommand *)pair[0] removeTarget:pair[1]];
  gTargets = nil;
  MPNowPlayingInfoCenter *n = [MPNowPlayingInfoCenter defaultCenter];
  n.nowPlayingInfo = nil;
  n.playbackState = MPNowPlayingPlaybackStateStopped;
}
