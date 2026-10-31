#!/usr/bin/env python3
"""检查与登记 iOS 的 libmpv 产物（只用标准库，调用 otool / lipo / nm / codesign）。

  xcf.py check <产物目录> --minos 15.0 --version 0.41.0
      Libmpv.xcframework 的静态检查，任何一条不过就以非零退出：
        * 目录里没有符号链接
        * xcframework 恰好两份：ios-arm64（真机）与 ios-arm64_x86_64-simulator（模拟器，两个架构）
        * 每个切片：平台（LC_BUILD_VERSION）对、minos 不高于 --minos、是动态库、装载名是
          @rpath/Libmpv.framework/Libmpv、没有 LC_RPATH、只依赖系统库、只导出 mpv_* 符号
        * framework 的 Info.plist（MinimumOSVersion = --minos、平台、版本）、四个头文件、module map
        * ad-hoc 签名有效
      另外列出每个切片引用的「需要声明理由的 API」（App 的 PrivacyInfo.xcprivacy 要覆盖它们）。

  xcf.py ff-superset <macOS recipes.sh> <iOS recipes.sh>
      iOS 的 FFmpeg 解码器 / 分离器 / 解析器清单必须是 macOS 版的超集（本地文件在 iOS 上要能播桌面能播的一切）。

  xcf.py gpl-files <mpv 的 Copyright> <mpv-sources.txt>
      mpv 在 Copyright 里列出的「GPL only」文件，一个都不能出现在实际编译的源文件里。

  xcf.py licenses --lock versions.lock --choices license-choices.txt --used <清单>
      <清单> 每行一个这次构建用到的源码名。每个都要在 license-choices.txt 里有一行，选用的许可证不能是 GPL；
      依据为 lock 的，选用的许可证必须是 versions.lock 那一列里的选项之一。

  xcf.py manifest <产物目录> --lock versions.lock --choices license-choices.txt --used <清单> --version 0.41.0
      写出 manifest.json 与 MANIFEST.txt：组件版本 / 选用的许可证 / 源码 sha256、工具链、每个文件的 sha256、
      每个切片的架构与 minos。
"""

import argparse
import datetime
import fnmatch
import hashlib
import json
import os
import plistlib
import re
import subprocess
import sys
from typing import Dict, List

FRAMEWORK = "Libmpv.framework"
BINARY = "Libmpv"
INSTALL_NAME = "@rpath/Libmpv.framework/Libmpv"
HEADERS = ("client.h", "render.h", "render_gl.h", "stream_cb.h")
# xcframework 里的两份：(目录名, SupportedPlatformVariant, 架构, LC_BUILD_VERSION 平台, Info.plist 平台)
SLICES = (
    ("ios-arm64", None, ("arm64",), "IOS", "iPhoneOS"),
    ("ios-arm64_x86_64-simulator", "simulator", ("arm64", "x86_64"), "IOSSIMULATOR", "iPhoneSimulator"),
)
PLATFORM_NAMES = {"2": "IOS", "IOS": "IOS", "7": "IOSSIMULATOR", "IOSSIMULATOR": "IOSSIMULATOR"}
# 宿主（Go 与 Swift）要用到的 API：少一个都说明链接或导出列表出了问题
REQUIRED_EXPORTS = (
    "_mpv_client_api_version", "_mpv_create", "_mpv_initialize", "_mpv_terminate_destroy",
    "_mpv_command", "_mpv_command_async", "_mpv_set_option_string", "_mpv_set_property",
    "_mpv_get_property", "_mpv_observe_property", "_mpv_wait_event", "_mpv_wakeup",
    "_mpv_set_wakeup_callback", "_mpv_request_log_messages", "_mpv_free",
    "_mpv_render_context_create", "_mpv_render_context_render", "_mpv_render_context_update",
    "_mpv_render_context_set_update_callback", "_mpv_render_context_report_swap",
    "_mpv_render_context_free",
)
# Apple「需要声明理由的 API」里会出现在 C 库里的那些符号（只列出来，不判失败）
REQUIRED_REASON_SYMBOLS = {
    "_stat": "文件时间戳", "_fstat": "文件时间戳", "_lstat": "文件时间戳", "_fstatat": "文件时间戳",
    "_getattrlist": "文件时间戳", "_getattrlistbulk": "文件时间戳", "_fgetattrlist": "文件时间戳",
    "_getattrlistat": "文件时间戳",
    "_mach_absolute_time": "系统启动时间",
    "_statfs": "磁盘空间", "_fstatfs": "磁盘空间", "_statvfs": "磁盘空间", "_fstatvfs": "磁盘空间",
}


def run(*cmd: str) -> str:
    return subprocess.run(cmd, check=True, capture_output=True, text=True).stdout


def version_tuple(v: str) -> tuple:
    return tuple(int(x) for x in v.split("."))


def sha256_of(path: str) -> str:
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def load_commands(path: str, arch: str) -> dict:
    """解析 otool -h / -l：文件类型、平台、minos、sdk、装载名、依赖、LC_RPATH。"""
    info = {"filetype": None, "platform": None, "minos": None, "sdk": None, "id": None,
            "deps": [], "rpaths": []}
    header = run("otool", "-arch", arch, "-h", path).split()
    # otool -h 的最后一行：magic cputype cpusubtype caps filetype ncmds sizeofcmds flags
    if len(header) >= 8:
        info["filetype"] = header[-4]
    cmd = None
    for raw in run("otool", "-arch", arch, "-l", path).splitlines():
        parts = raw.split()
        if not parts:
            continue
        if parts[0] == "cmd":
            cmd = parts[1]
            continue
        if cmd == "LC_BUILD_VERSION" and parts[0] in ("platform", "minos", "sdk"):
            info[parts[0]] = parts[1]
        elif cmd == "LC_ID_DYLIB" and parts[0] == "name":
            info["id"] = parts[1]
        elif cmd in ("LC_LOAD_DYLIB", "LC_LOAD_WEAK_DYLIB", "LC_REEXPORT_DYLIB",
                     "LC_LAZY_LOAD_DYLIB", "LC_LOAD_UPWARD_DYLIB") and parts[0] == "name":
            info["deps"].append(parts[1])
        elif cmd == "LC_RPATH" and parts[0] == "path":
            info["rpaths"].append(parts[1])
    return info


def symbols(path: str, arch: str, flag: str) -> List[str]:
    """nm 的符号名：flag = -gU（定义的外部符号）或 -u（未定义的）。"""
    out = run("nm", "-arch", arch, flag, "-j", path)
    return sorted({line.strip() for line in out.splitlines() if line.strip()})


def walk(root: str):
    for d, dirs, files in os.walk(root):
        for name in sorted(dirs + files):
            yield os.path.join(d, name)


def check(args: argparse.Namespace) -> int:
    root = args.dir
    xcf = os.path.join(root, "Libmpv.xcframework")
    minos_max = version_tuple(args.minos)
    errors: List[str] = []

    def fail(msg: str) -> None:
        errors.append(msg)
        print("  ✗ " + msg)

    for path in walk(root):
        if os.path.islink(path):
            fail(f"符号链接：{os.path.relpath(path, root)} -> {os.readlink(path)}")

    try:
        with open(os.path.join(xcf, "Info.plist"), "rb") as f:
            plist = plistlib.load(f)
    except (OSError, plistlib.InvalidFileException) as e:
        print(f"  ✗ 读不了 Libmpv.xcframework/Info.plist：{e}")
        return 1
    libs = plist.get("AvailableLibraries", [])
    got = sorted((lib.get("LibraryIdentifier"), lib.get("SupportedPlatform"),
                  lib.get("SupportedPlatformVariant"), tuple(sorted(lib.get("SupportedArchitectures", []))),
                  lib.get("LibraryPath"), lib.get("BinaryPath")) for lib in libs)
    want = sorted((ident, "ios", variant, tuple(sorted(archs)), FRAMEWORK, f"{FRAMEWORK}/{BINARY}")
                  for ident, variant, archs, _, _ in SLICES)
    if got != want:
        fail(f"xcframework 的切片不对：{got}，应为 {want}")
    else:
        print("  ✓ xcframework：ios-arm64 + ios-arm64_x86_64-simulator（真机 arm64、模拟器 arm64 + x86_64）")

    for ident, _, archs, platform, plist_platform in SLICES:
        fw = os.path.join(xcf, ident, FRAMEWORK)
        binary = os.path.join(fw, BINARY)
        before = len(errors)
        if not os.path.isfile(binary):
            fail(f"{ident}：缺少 {FRAMEWORK}/{BINARY}")
            continue
        got_archs = sorted(run("lipo", "-archs", binary).split())
        if got_archs != sorted(archs):
            fail(f"{ident}：架构是 {' '.join(got_archs)}，应为 {' '.join(archs)}")
        for arch in got_archs:
            tag = f"{ident} [{arch}]"
            lc = load_commands(binary, arch)
            if lc["filetype"] not in ("6", "DYLIB"):
                fail(f"{tag}：不是动态库（filetype {lc['filetype']}）")
            if PLATFORM_NAMES.get(lc["platform"] or "") != platform:
                fail(f"{tag}：平台是 {lc['platform']}，应为 {platform}")
            if lc["minos"] is None:
                fail(f"{tag}：读不到 LC_BUILD_VERSION")
            elif version_tuple(lc["minos"]) > minos_max:
                fail(f"{tag}：minos {lc['minos']} 高于 {args.minos}")
            if lc["id"] != INSTALL_NAME:
                fail(f"{tag}：装载名是 {lc['id']}，应为 {INSTALL_NAME}")
            if lc["rpaths"]:
                fail(f"{tag}：不该有 LC_RPATH，实际 {lc['rpaths']}")
            for dep in lc["deps"]:
                if not (dep.startswith("/usr/lib/") or dep.startswith("/System/Library/")):
                    fail(f"{tag}：依赖了非系统库 {dep}")
            exported = symbols(binary, arch, "-gU")
            stray = [s for s in exported if not s.startswith("_mpv_")]
            if stray:
                fail(f"{tag}：导出了 mpv_* 以外的符号 {len(stray)} 个，例如 {stray[:5]}")
            missing = [s for s in REQUIRED_EXPORTS if s not in exported]
            if missing:
                fail(f"{tag}：缺少导出 {missing}")
            if len(errors) == before:
                print(f"  ✓ {tag}：平台 {platform}、minos {lc['minos']}、sdk {lc['sdk']}、"
                      f"{len(exported)} 个 mpv_* 导出、依赖 {len(lc['deps'])} 个系统库")
            reasons = sorted({f"{s[1:]}（{REQUIRED_REASON_SYMBOLS[s]}）"
                              for s in symbols(binary, arch, "-u") if s in REQUIRED_REASON_SYMBOLS})
            if reasons:
                print(f"    需要声明理由的 API：{'、'.join(reasons)}")

        try:
            with open(os.path.join(fw, "Info.plist"), "rb") as f:
                info = plistlib.load(f)
        except (OSError, plistlib.InvalidFileException) as e:
            fail(f"{ident}：读不了 {FRAMEWORK}/Info.plist：{e}")
            info = {}
        expect = {
            "CFBundleExecutable": BINARY, "CFBundlePackageType": "FMWK",
            "MinimumOSVersion": args.minos, "CFBundleSupportedPlatforms": [plist_platform],
            "CFBundleShortVersionString": args.version,
        }
        for key, value in expect.items():
            if info.get(key) != value:
                fail(f"{ident}：Info.plist 的 {key} 是 {info.get(key)!r}，应为 {value!r}")
        if not info.get("CFBundleIdentifier") or not info.get("CFBundleVersion"):
            fail(f"{ident}：Info.plist 缺少 CFBundleIdentifier / CFBundleVersion")
        for h in HEADERS:
            if not os.path.isfile(os.path.join(fw, "Headers", "mpv", h)):
                fail(f"{ident}：缺少头文件 Headers/mpv/{h}")
        modmap = os.path.join(fw, "Modules", "module.modulemap")
        if not os.path.isfile(modmap) or "framework module Libmpv" not in open(modmap).read():
            fail(f"{ident}：缺少 Modules/module.modulemap（framework module Libmpv）")
        sig = subprocess.run(["codesign", "--verify", "--strict", fw], capture_output=True, text=True)
        if sig.returncode != 0:
            fail(f"{ident}：签名无效：{sig.stderr.strip()}")
        if len(errors) == before:
            print(f"  ✓ {ident}：Info.plist（MinimumOSVersion {args.minos}）、头文件、module map、签名")

    for h in HEADERS:
        if not os.path.isfile(os.path.join(root, "include", "mpv", h)):
            fail(f"缺少 include/mpv/{h}（给 Go 的 cgo 用）")

    if errors:
        print(f"静态检查失败：{len(errors)} 项", file=sys.stderr)
        return 1
    print("静态检查通过")
    return 0


def ff_lists(path: str) -> Dict[str, set]:
    """读 recipe_ffmpeg 里的 local FF_XXX="..." 清单。"""
    text = open(path, encoding="utf-8").read()
    body = text[text.index("recipe_ffmpeg()"):]
    out = {}
    for name, value in re.findall(r'local (FF_[A-Z]+)="([^"]*)"', body):
        out.setdefault(name, set(value.split()))
    return out


def ff_superset(args: argparse.Namespace) -> int:
    mac, ios = ff_lists(args.macos), ff_lists(args.ios)
    bad = 0
    for name in ("FF_DECODERS", "FF_DEMUXERS", "FF_PARSERS"):
        if name not in mac or name not in ios:
            print(f"  ✗ 读不出 {name}（recipes.sh 的写法变了？）")
            bad += 1
            continue
        missing = sorted(mac[name] - ios[name])
        if missing:
            print(f"  ✗ iOS 的 {name} 比 macOS 版少：{' '.join(missing)}")
            bad += 1
        else:
            print(f"  ✓ iOS 的 {name} 包含 macOS 版的全部 {len(mac[name])} 项")
    return 1 if bad else 0


def gpl_files(args: argparse.Namespace) -> int:
    text = open(args.copyright, encoding="utf-8").read()
    m = re.search(r"The following files are still GPL only.*?\n(.*?)\n\s*None of the cases", text, re.S)
    if not m:
        print("  ✗ 在 mpv 的 Copyright 里找不到「GPL only」清单（格式变了？）")
        return 1
    patterns = [line.split()[0] for line in m.group(1).splitlines() if line.strip()]
    if len(patterns) < 10:
        print(f"  ✗ 「GPL only」清单只读出 {len(patterns)} 项，格式可能变了：{patterns}")
        return 1
    sources = [line.strip() for line in open(args.sources, encoding="utf-8") if line.strip()]
    if not sources:
        print("  ✗ mpv-sources.txt 是空的")
        return 1
    hits = []
    for src in sources:
        for pat in patterns:
            if (pat.endswith("/") and src.startswith(pat)) or fnmatch.fnmatch(src, pat):
                hits.append(f"{src}（{pat}）")
    if hits:
        print(f"  ✗ 编进了 GPL only 的文件：{', '.join(hits)}")
        return 1
    print(f"  ✓ mpv 编了 {len(sources)} 个源文件，没有一个在 Copyright 的 {len(patterns)} 条「GPL only」清单里")
    return 0


def parse_lock(path: str) -> Dict[str, dict]:
    out = {}
    for line in open(path, encoding="utf-8"):
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        f = [x.strip() for x in line.split("|")]
        out[f[0]] = {"version": f[1], "license": f[2], "file": f[3], "sha256": f[4], "url": f[5]}
    return out


def parse_choices(path: str) -> Dict[str, dict]:
    out = {}
    for line in open(path, encoding="utf-8"):
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        f = [x.strip() for x in line.split("|")]
        if len(f) < 3:
            raise ValueError(f"license-choices.txt 这一行少了列：{line.strip()}")
        out[f[0]] = {"license": f[1], "basis": f[2], "note": f[3] if len(f) > 3 else ""}
    return out


def is_gpl(spdx: str) -> bool:
    # GPL / AGPL 都不行；LGPL 可以
    return re.search(r"(^|[^L])GPL", spdx) is not None


def read_used(path: str) -> List[str]:
    return [line.strip() for line in open(path, encoding="utf-8") if line.strip()]


def licenses(args: argparse.Namespace) -> int:
    lock, choices, used = parse_lock(args.lock), parse_choices(args.choices), read_used(args.used)
    bad = 0
    for name in used:
        if name not in lock:
            print(f"  ✗ {name} 不在 versions.lock 里")
            bad += 1
            continue
        c = choices.get(name)
        if c is None:
            print(f"  ✗ {name} 在 license-choices.txt 里没有一行（用了却没说按哪个许可证分发）")
            bad += 1
            continue
        if is_gpl(c["license"]):
            print(f"  ✗ {name} 选用的许可证是 GPL：{c['license']}")
            bad += 1
            continue
        lock_lic = lock[name]["license"]
        if c["basis"] == "lock":
            options = [x.strip() for x in lock_lic.split(" OR ")]
            if c["license"] != lock_lic and c["license"] not in options:
                print(f"  ✗ {name}：选用的 {c['license']} 不是 versions.lock 里 {lock_lic} 的选项")
                bad += 1
                continue
        elif c["basis"] != "config":
            print(f"  ✗ {name}：依据只能是 lock 或 config，实际 {c['basis']}")
            bad += 1
            continue
        print(f"  ✓ {name}：{c['license']}" + ("（由构建配置决定，见上面的 FFmpeg / mpv 检查）"
                                              if c["basis"] == "config" else ""))
    extra = sorted(set(choices) - set(used))
    if extra:
        print(f"  ✗ license-choices.txt 里有这次没用到的组件：{' '.join(extra)}")
        bad += 1
    return 1 if bad else 0


def manifest(args: argparse.Namespace) -> int:
    root = args.dir
    lock, choices, used = parse_lock(args.lock), parse_choices(args.choices), read_used(args.used)
    components = []
    for name in used:
        entry = dict(lock[name])
        entry["name"] = name
        entry["license_lock"] = entry.pop("license")
        entry["license"] = choices[name]["license"]
        components.append(entry)
    files, slices = [], []
    for path in walk(root):
        rel = os.path.relpath(path, root)
        if not os.path.isfile(path) or rel in ("manifest.json", "MANIFEST.txt"):
            continue
        files.append({"path": rel, "size": os.path.getsize(path), "sha256": sha256_of(path)})
    xcf = os.path.join(root, "Libmpv.xcframework")
    for ident, _, archs, _, _ in SLICES:
        binary = os.path.join(xcf, ident, FRAMEWORK, BINARY)
        for arch in archs:
            lc = load_commands(binary, arch)
            slices.append({"library": ident, "arch": arch, "platform": PLATFORM_NAMES.get(lc["platform"] or ""),
                           "minos": lc["minos"], "sdk": lc["sdk"], "size": os.path.getsize(binary),
                           "deps": lc["deps"]})
    toolchain = {}
    info = os.path.join(root, "info")
    for target in sorted(os.listdir(info)):
        tc = os.path.join(info, target, "toolchain.txt")
        if os.path.isfile(tc):
            toolchain[target] = open(tc, encoding="utf-8").read().strip().splitlines()
    commit = subprocess.run(["git", "rev-parse", "HEAD"], capture_output=True, text=True).stdout.strip()
    data = {
        "name": "libmpv-ios",
        "mpv_version": args.version,
        "license": "LGPL-2.1-or-later",
        "minimum_ios": args.minos,
        "source_commit": commit or "unknown",
        "generated": datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "components": components,
        "slices": slices,
        "toolchain": toolchain,
        "files": files,
    }
    with open(os.path.join(root, "manifest.json"), "w", encoding="utf-8") as f:
        json.dump(data, f, ensure_ascii=False, indent=2)
        f.write("\n")
    with open(os.path.join(root, "MANIFEST.txt"), "w", encoding="utf-8") as f:
        f.write(f"libmpv（iOS）{args.version}  整体许可证 LGPL-2.1-or-later  最低 iOS {args.minos}\n")
        f.write(f"来源提交 {data['source_commit']}  生成于 {data['generated']}\n\n组件：\n")
        for c in components:
            f.write(f"  {c['name']:<26} {c['version']:<12} {c['license']:<32} {c['sha256']}\n")
        f.write("\n切片：\n")
        for s in slices:
            f.write(f"  {s['library']:<28} {s['arch']:<7} {s['platform']:<13} minos {s['minos']}  sdk {s['sdk']}"
                    f"  {s['size']} 字节（整个 fat 文件）\n")
        f.write("\n文件：\n")
        for e in files:
            f.write(f"  {e['sha256']}  {e['size']:>10}  {e['path']}\n")
    print(f"清单：{len(components)} 个组件、{len(slices)} 个切片、{len(files)} 个文件")
    return 0


def main() -> int:
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = p.add_subparsers(dest="cmd", required=True)
    c = sub.add_parser("check")
    c.add_argument("dir")
    c.add_argument("--minos", required=True)
    c.add_argument("--version", required=True)
    s = sub.add_parser("ff-superset")
    s.add_argument("macos")
    s.add_argument("ios")
    g = sub.add_parser("gpl-files")
    g.add_argument("copyright")
    g.add_argument("sources")
    lic = sub.add_parser("licenses")
    lic.add_argument("--lock", required=True)
    lic.add_argument("--choices", required=True)
    lic.add_argument("--used", required=True)
    m = sub.add_parser("manifest")
    m.add_argument("dir")
    m.add_argument("--lock", required=True)
    m.add_argument("--choices", required=True)
    m.add_argument("--used", required=True)
    m.add_argument("--version", required=True)
    m.add_argument("--minos", required=True)
    args = p.parse_args()
    return {"check": check, "ff-superset": ff_superset, "gpl-files": gpl_files,
            "licenses": licenses, "manifest": manifest}[args.cmd](args)


if __name__ == "__main__":
    sys.exit(main())
