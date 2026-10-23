#!/usr/bin/env python3
"""macOS 桌面窗口的冒烟测试：窗口 + render API + 我们自建的 libmpv，在 CI 的 arm64 runner 上真跑一遍。

用的是带 desktoptest 遥控的测试构建（-tags "libmpv desktoptest"），布局照 Nagare.app：
    <app>/MacOS/nagare、<app>/MacOS/mpv、<app>/Frameworks/*.dylib（rpath @executable_path/../Frameworks）。

步骤（每一步都有时限；任何一步超时或不对就失败）：
  1. 用临时配置目录起 nagare（窗口模式），等本机 API 起来；
  2. 加一个媒体库目录（测试片段补到 1MB 以上，扫描器才收），取 fileId；
  3. POST /api/play —— 起播卡死（5002c52 修掉的那种主线程死锁）就在这里超时失败；
  4. 经 mpv 的 IPC：current-vo 是 libmpv，time-pos 在走；遥控的 /state 里窗口在播、渲染过帧；
  5. 暂停、拖进度条（/api/player/seek）到 12 秒，time-pos 落到目标附近；截一张窗口（只截本进程的窗口，不要屏幕录制权限）；
  6. ⌘Q（遥控经 NSApp sendEvent 发，与真按一样先过菜单）：进程在时限内退出、退出码 0，日志里收尾走完了，
     state.json 里记下了这一集的进度（在 12 秒附近 —— 不到 5 秒的位置 nagare 本来就不记）。

没有 GPU 的 runner：设 NAGARE_DESKTOP_TEST_SOFTWARE_GL=1，测试构建退到苹果的软件渲染器并给 mpv 设 gpu-sw=yes
（同 #54 的做法，只在测试构建里有；正式构建不认这个变量）。

测试片段：internal/desktop/testdata/smoke-40s.mp4（40 秒 H.264 + AAC，由一个全功能的 ffmpeg 生成：
  ffmpeg -fflags +bitexact -f lavfi -i testsrc2=s=192x108:r=24:d=40 -f lavfi -i sine=f=440:r=24000:d=40 \
    -c:v libx264 -preset veryfast -crf 46 -pix_fmt yuv420p -g 96 -c:a aac -b:a 16k -ac 1 -movflags +faststart smoke-40s.mp4）

用法：python3 -I -B desktop-smoke.py --app <目录> --media <测试片段> --work <空目录> [--timeout 秒]
token 与遥控密钥只从文件里读，不打印。
"""

import argparse
import glob
import http.client
import json
import os
import re
import shutil
import signal
import socket
import subprocess
import sys
import time
import urllib.parse
import urllib.request

CTL_ADDR = "127.0.0.1:18797"
MIN_VIDEO = 1024 * 1024 + 4096  # 扫描器丢掉 1MB 以下的视频（internal/library/scan.go 的 minVideoSize）
SEEK_TO = 12.0  # 不到 5 秒（player 的 resumeMinSec）不记进度；片段 40 秒，12 秒离「看完」也远


class Smoke:
    def __init__(self, args):
        self.args = args
        self.work = os.path.abspath(args.work)
        self.cfg = os.path.join(self.work, "config")
        self.lib = os.path.join(self.work, "library")
        self.shots = os.path.join(self.work, "shots")
        self.log_path = os.path.join(self.work, "nagare.log")
        self.mpv_log = os.path.join(self.work, "mpv.log")
        self.secret_file = os.path.join(self.work, "test-secret")
        self.proc = None
        self.deadline = time.time() + args.timeout

    # ---------- 小工具 ----------
    def step(self, msg):
        print("==> " + msg, flush=True)

    def left(self, cap):
        return max(0.5, min(cap, self.deadline - time.time()))

    def wait(self, what, cond, cap, interval=0.25):
        end = time.time() + self.left(cap)
        last = None
        while time.time() < end:
            if self.proc and self.proc.poll() is not None:
                raise RuntimeError(f"等{what}时 nagare 退出了（退出码 {self.proc.returncode}）")
            try:
                last = cond()
                if last:
                    return last
            except Exception as e:  # noqa: BLE001 —— 等待期间的错误只记下最后一个
                last = e
            time.sleep(interval)
        raise RuntimeError(f"{cap:.0f} 秒内没有等到{what}（最后一次：{last!r}）")

    def conf(self):
        text = open(os.path.join(self.cfg, "config.toml"), encoding="utf-8").read()
        port = re.search(r"^port\s*=\s*(\d+)", text, re.M).group(1)
        token = re.search(r'^token\s*=\s*"([^"]+)"', text, re.M).group(1)
        return port, token

    def api(self, method, path, body=None, timeout=30):
        port, token = self.conf()
        req = urllib.request.Request(
            f"http://127.0.0.1:{port}{path}", method=method,
            data=json.dumps(body).encode() if body is not None else None,
            headers={"X-Nagare-Token": token, "Content-Type": "application/json"})
        with urllib.request.urlopen(req, timeout=timeout) as r:
            return json.loads(r.read().decode() or "null")

    def ctl(self, route, **q):
        secret = open(self.secret_file, encoding="utf-8").read().strip()
        query = "&".join(f"{k}={urllib.parse.quote(str(v))}" for k, v in q.items())
        req = urllib.request.Request(f"http://{CTL_ADDR}{route}" + (f"?{query}" if query else ""),
                                     headers={"X-Nagare-Test-Secret": secret})
        with urllib.request.urlopen(req, timeout=15) as r:
            return r.read().decode()

    def state(self):
        return json.loads(self.ctl("/state"))

    def ipc_socket(self):
        # 套接字放在运行时目录里；路径超过 Unix 套接字的长度上限时回落到 /tmp 下的私有目录（internal/mpv/mpv_unix.go）
        name = f"mpv-{self.proc.pid}-*.sock"
        socks = glob.glob(os.path.join(self.cfg, "runtime", name)) + glob.glob(f"/tmp/nagare-mpv-*/{name}")
        return max(socks, key=os.path.getmtime) if socks else None

    def mpv(self, *command):
        path = self.ipc_socket()
        if not path:
            raise RuntimeError("还没有 mpv 的 IPC 套接字")
        s = socket.socket(socket.AF_UNIX)
        s.settimeout(5)
        try:
            s.connect(path)
            s.sendall((json.dumps({"command": list(command)}) + "\n").encode())
            buf = b""
            while True:
                while b"\n" not in buf:
                    chunk = s.recv(65536)
                    if not chunk:
                        raise RuntimeError("IPC 断开")
                    buf += chunk
                line, buf = buf.split(b"\n", 1)
                msg = json.loads(line)
                if "event" not in msg:
                    if msg.get("error") != "success":
                        raise RuntimeError(f"mpv {command}：{msg.get('error')}")
                    return msg.get("data")
        finally:
            s.close()

    # ---------- 步骤 ----------
    def prepare(self):
        for d in (self.cfg, self.lib, self.shots):
            os.makedirs(d, exist_ok=True)
        # 测试片段补到 1MB 以上：mp4 末尾追加一个 free 盒子（合法的 MP4，播放器跳过它）
        clip = os.path.join(self.lib, "[Smoke] 冒烟测试 - 01 [1080P].mp4")
        shutil.copyfile(self.args.media, clip)
        with open(clip, "ab") as f:
            pad = MIN_VIDEO
            f.write(pad.to_bytes(4, "big") + b"free" + b"\0" * (pad - 8))
        return clip

    def launch(self):
        env = {
            "PATH": "/usr/bin:/bin:/usr/sbin:/sbin",
            "HOME": os.environ.get("HOME", self.work),
            "TMPDIR": os.environ.get("TMPDIR", "/tmp"),
            "NAGARE_CONFIG_DIR": self.cfg,
            "NAGARE_DESKTOP_TEST_ADDR": CTL_ADDR,
            "NAGARE_DESKTOP_TEST_SECRET_FILE": self.secret_file,
            "NAGARE_DESKTOP_MPV_LOG": self.mpv_log,
        }
        if os.environ.get("NAGARE_DESKTOP_TEST_SOFTWARE_GL"):
            env["NAGARE_DESKTOP_TEST_SOFTWARE_GL"] = os.environ["NAGARE_DESKTOP_TEST_SOFTWARE_GL"]
        exe = os.path.join(os.path.abspath(self.args.app), "MacOS", "nagare")
        self.step(f"起 nagare（窗口模式）：{exe}")
        self.proc = subprocess.Popen([exe, "--no-browser"], env=env, stdout=open(self.log_path, "wb"),
                                     stderr=subprocess.STDOUT, start_new_session=True)
        self.wait("本机 API", lambda: self.api("GET", "/api/health", timeout=3), 30)
        self.wait("测试遥控", lambda: self.state()["window"] is not None, 15)
        settings = self.api("GET", "/api/settings")["data"]["mpv"]
        print("    mpv 视图：" + json.dumps(settings, ensure_ascii=False))
        if not settings.get("embedded"):
            raise RuntimeError("设置里不是桌面窗口模式（embedded 不为真）—— 测试构建没带 libmpv 标签，或没有托盘事件循环？")

    def play(self, clip):
        self.step("加媒体库目录、取 fileId")
        self.api("POST", "/api/library/folders", {"path": self.lib}, timeout=60)
        file_id = self.wait("扫描出测试片段", lambda: self.find_file(os.path.basename(clip)), 30)
        self.step("POST /api/play（起播卡死会在这里超时）")
        res = self.api("POST", "/api/play", {"fileId": file_id}, timeout=self.left(40))
        if not res.get("success"):
            raise RuntimeError(f"起播失败：{res}")
        return file_id

    def find_file(self, name):
        lib = self.api("GET", "/api/library")["data"]
        for cl in lib.get("clusters") or []:
            for g in cl.get("groups") or []:
                for it in g.get("items") or []:
                    if name in it.get("fileId", ""):
                        return it["fileId"]
        return None

    def check_playback(self):
        self.step("经 IPC 看播放：current-vo / time-pos")
        vo = self.wait("current-vo", lambda: self.mpv("get_property", "current-vo"), 30)
        if vo != "libmpv":
            raise RuntimeError(f"current-vo 是 {vo!r}，应为 libmpv")
        t0 = self.wait("time-pos 开始走", lambda: (lambda t: t if t and t > 0.3 else None)(
            self.mpv("get_property", "time-pos")), 30)
        t1 = self.wait("time-pos 继续往前", lambda: (lambda t: t if t and t > t0 + 0.4 else None)(
            self.mpv("get_property", "time-pos")), 15)
        print(f"    current-vo={vo} time-pos {t0:.2f} → {t1:.2f}")
        st = self.wait("窗口在播且渲染过帧", lambda: (lambda s: s if s.get("playing") and s.get("frames", 0) > 0
                                                      else None)(self.state()), 15)
        print(f"    窗口：playing={st['playing']} frames={st['frames']} backingScale={st['backingScale']}")

    def check_seek(self):
        self.step(f"暂停、拖进度条到 {SEEK_TO:.0f} 秒")
        self.api("POST", "/api/player/pause", {"paused": True})
        self.wait("暂停", lambda: self.mpv("get_property", "pause") is True, 10)
        self.api("POST", "/api/player/seek", {"position": SEEK_TO})
        pos = self.wait(f"time-pos 到 {SEEK_TO:.0f} 秒附近",
                        lambda: (lambda t: t if t is not None and abs(t - SEEK_TO) < 1.0 else None)(
                            self.mpv("get_property", "time-pos")), 15)
        print(f"    seek 之后 time-pos={pos:.2f}")
        # 暂停中拖过去，画面也要重画一帧（render API 的更新回调 → 图层重画）；软件渲染器慢，多给点时间
        before = self.state().get("frames", 0)
        after = self.wait("拖动之后重画", lambda: (lambda n: n if n > before else None)(self.state().get("frames", 0)), 15)
        print(f"    拖动之后又画了 {after - before} 帧")
        time.sleep(0.8)
        shot = os.path.join(self.shots, "playing.png")
        print(f"    截窗口：{self.ctl('/snap', path=shot).strip()} → {shot}")

    def quit(self, file_id):
        self.step("⌘Q（经菜单的快捷键）")
        # 按下之后进程就开始收尾、随即退出：遥控可能来不及回这个请求，连接断开不算错
        for up in (0, 1):
            try:
                self.ctl("/key", code=12, chars="q", mods=8, up=up)
            except (OSError, http.client.HTTPException):
                break
        try:
            rc = self.proc.wait(timeout=self.left(40))
        except subprocess.TimeoutExpired:
            raise RuntimeError("⌘Q 之后 40 秒内没有退出（收尾卡住了？）")
        log = open(self.log_path, encoding="utf-8", errors="replace").read()
        if rc != 0:
            raise RuntimeError(f"退出码 {rc}")
        # ⌘Q 走的是 NSTerminateLater：收尾跑完、答复系统之后由系统结束进程（main 里那句「已退出」不会打）
        for mark in ("正在退出：停止播放并回写进度", "收尾完成，答复系统可以终止"):
            if mark not in log:
                raise RuntimeError(f"日志里没有「{mark}」：⌘Q 的收尾没走完")
        state = json.load(open(os.path.join(self.cfg, "state.json"), encoding="utf-8"))
        prog = (state.get("progress") or {}).get(file_id)
        if not prog:
            raise RuntimeError("state.json 里没有这一集的进度：退出时没有回写")
        if abs(prog.get("positionSec", 0) - SEEK_TO) > 1.5:
            raise RuntimeError(f"记下的进度不对：{prog}（应在 {SEEK_TO:.0f} 秒附近）")
        print(f"    退出码 0；记下的进度：{json.dumps(prog, ensure_ascii=False)}")

    def dump(self):
        for name, path in (("nagare 日志", self.log_path), ("mpv 日志", self.mpv_log)):
            if os.path.exists(path):
                lines = open(path, encoding="utf-8", errors="replace").read().splitlines()
                print(f"---- {name}（最后 60 行）----")
                for line in lines[-60:]:
                    print("    | " + line)

    def stop(self):
        if self.proc and self.proc.poll() is None:
            try:
                os.killpg(self.proc.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            self.proc.wait(timeout=10)

    def run(self):
        clip = self.prepare()
        try:
            self.launch()
            file_id = self.play(clip)
            self.check_playback()
            self.check_seek()
            self.quit(file_id)
        except Exception as e:  # noqa: BLE001 —— 失败时把现场打出来
            print(f"✗ 冒烟测试失败：{e}", flush=True)
            try:
                if self.proc and self.proc.poll() is None:
                    print("    遥控状态：" + json.dumps(self.state(), ensure_ascii=False))
                    self.ctl("/snap", path=os.path.join(self.shots, "failure.png"))
            except Exception:  # noqa: BLE001
                pass
            self.dump()
            return 1
        finally:
            self.stop()
        print("✓ 冒烟测试通过", flush=True)
        return 0


def main():
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--app", required=True, help="布局照 Nagare.app/Contents 的目录（MacOS/、Frameworks/）")
    p.add_argument("--media", required=True, help="一段 mp4 测试片段（会补到 1MB 以上）")
    p.add_argument("--work", required=True, help="空的工作目录（配置、媒体库、截图、日志）")
    p.add_argument("--timeout", type=float, default=240, help="整个测试的总时限（秒）")
    return Smoke(p.parse_args()).run()


if __name__ == "__main__":
    sys.exit(main())
