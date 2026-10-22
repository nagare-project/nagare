#!/usr/bin/env python3
"""检查与登记 package.sh universal 产出的那一套文件（只用标准库，调用 otool / lipo / codesign）。

  macho.py check <产物目录> [--minos 13.0]
      静态检查，任何一条不过就以非零退出：
        * 目录里没有符号链接（nagare 的自更新拒绝符号链接）
        * 每个 Mach-O 都是 arm64 + x86_64 的 universal
        * 每个切片的最低系统版本（LC_BUILD_VERSION.minos）不高于 --minos
        * Frameworks/ 里的库装载名都是 @rpath/<文件名>，且没有任何 LC_RPATH
        * MacOS/mpv 只有一条 LC_RPATH：@executable_path/../Frameworks
        * 依赖要么是系统库（/usr/lib、/System/Library），要么是 @rpath/<Frameworks 里确有的文件>
        * ad-hoc 签名有效
        * 四个 mpv 头文件都在

  macho.py manifest <产物目录> --lock versions.lock --patches patches/ [--mpv-version 文件]
      写出 manifest.json 与 MANIFEST.txt：组件版本 / 许可证 / 源码哈希、补丁、工具链、
      每个文件的 sha256，每个 Mach-O 的架构、minos、装载名、依赖与 LC_RPATH。
"""

import argparse
import datetime
import hashlib
import json
import os
import subprocess
import sys
from typing import Iterator, List, Optional

ARCHS = ("arm64", "x86_64")
HEADERS = ("client.h", "render.h", "render_gl.h", "stream_cb.h")
MPV_RPATH = "@executable_path/../Frameworks"
MACHO_MAGICS = {
    b"\xca\xfe\xba\xbe", b"\xbe\xba\xfe\xca",  # fat
    b"\xcf\xfa\xed\xfe", b"\xfe\xed\xfa\xcf",  # 64 位
}


def run(*cmd: str) -> str:
    return subprocess.run(cmd, check=True, capture_output=True, text=True).stdout


def is_macho(path: str) -> bool:
    with open(path, "rb") as f:
        return f.read(4) in MACHO_MAGICS


def version_tuple(v: str) -> tuple:
    return tuple(int(x) for x in v.split("."))


def is_system(path: str) -> bool:
    return path.startswith("/usr/lib/") or path.startswith("/System/Library/")


def load_commands(path: str, arch: str) -> dict:
    """解析 otool -l：返回 minos、sdk、装载名、依赖、LC_RPATH。"""
    out = run("otool", "-arch", arch, "-l", path)
    info = {"minos": None, "sdk": None, "id": None, "deps": [], "rpaths": []}
    cmd = None
    for raw in out.splitlines():
        line = raw.strip()
        if line.startswith("cmd "):
            cmd = line.split()[1]
            continue
        parts = line.split()
        if not parts:
            continue
        if cmd == "LC_BUILD_VERSION" and parts[0] in ("minos", "sdk"):
            info[parts[0]] = parts[1]
        elif cmd == "LC_VERSION_MIN_MACOSX" and parts[0] == "version":
            info["minos"] = parts[1]
        elif cmd == "LC_VERSION_MIN_MACOSX" and parts[0] == "sdk":
            info["sdk"] = parts[1]
        elif cmd == "LC_ID_DYLIB" and parts[0] == "name":
            info["id"] = parts[1]
        elif cmd in ("LC_LOAD_DYLIB", "LC_LOAD_WEAK_DYLIB", "LC_REEXPORT_DYLIB",
                     "LC_LAZY_LOAD_DYLIB", "LC_LOAD_UPWARD_DYLIB") and parts[0] == "name":
            info["deps"].append(parts[1])
        elif cmd == "LC_RPATH" and parts[0] == "path":
            info["rpaths"].append(parts[1])
    return info


def walk_files(root: str) -> Iterator[str]:
    for d, dirs, files in os.walk(root):
        for name in sorted(dirs + files):
            yield os.path.join(d, name)


def machos(root: str) -> List[str]:
    found = []
    for sub in ("Frameworks", "MacOS"):
        base = os.path.join(root, sub)
        for name in sorted(os.listdir(base)):
            path = os.path.join(base, name)
            if os.path.isfile(path) and not os.path.islink(path) and is_macho(path):
                found.append(os.path.relpath(path, root))
    return found


def check(args: argparse.Namespace) -> int:
    root = args.dir
    minos_max = version_tuple(args.minos)
    errors = []

    def fail(msg: str) -> None:
        errors.append(msg)
        print("  ✗ " + msg)

    for path in walk_files(root):
        if os.path.islink(path):
            fail(f"符号链接：{os.path.relpath(path, root)} -> {os.readlink(path)}")

    frameworks = set(os.listdir(os.path.join(root, "Frameworks")))
    files = machos(root)
    if "Frameworks/libmpv.2.dylib" not in files:
        fail("缺少 Frameworks/libmpv.2.dylib")
    if "MacOS/mpv" not in files:
        fail("缺少 MacOS/mpv")
    for h in HEADERS:
        if not os.path.isfile(os.path.join(root, "include", "mpv", h)):
            fail(f"缺少头文件 include/mpv/{h}")

    for rel in files:
        path = os.path.join(root, rel)
        name = os.path.basename(rel)
        is_dylib = rel.startswith("Frameworks/")
        archs = sorted(run("lipo", "-archs", path).split())
        if archs != sorted(ARCHS):
            fail(f"{rel}：架构是 {' '.join(archs)}，应为 arm64 + x86_64")
        for arch in archs:
            lc = load_commands(path, arch)
            tag = f"{rel} [{arch}]"
            if lc["minos"] is None:
                fail(f"{tag}：读不到 LC_BUILD_VERSION / LC_VERSION_MIN_MACOSX")
            elif version_tuple(lc["minos"]) > minos_max:
                fail(f"{tag}：minos {lc['minos']} 高于 {args.minos}")
            if is_dylib:
                if lc["id"] != f"@rpath/{name}":
                    fail(f"{tag}：装载名是 {lc['id']}，应为 @rpath/{name}")
                if lc["rpaths"]:
                    fail(f"{tag}：不该有 LC_RPATH，实际 {lc['rpaths']}")
            elif lc["rpaths"] != [MPV_RPATH]:
                fail(f"{tag}：LC_RPATH 应只有 {MPV_RPATH}，实际 {lc['rpaths']}")
            for dep in lc["deps"]:
                if is_system(dep):
                    continue
                if dep.startswith("@rpath/") and dep[len("@rpath/"):] in frameworks:
                    continue
                fail(f"{tag}：依赖 {dep} 既不是系统库，也不在 Frameworks/ 里")
        sig = subprocess.run(["codesign", "--verify", "--strict", path], capture_output=True, text=True)
        if sig.returncode != 0:
            fail(f"{rel}：签名无效：{sig.stderr.strip()}")
        print(f"  ✓ {rel}（{' '.join(archs)}）")

    if errors:
        print(f"静态检查失败：{len(errors)} 项", file=sys.stderr)
        return 1
    print(f"静态检查通过：{len(files)} 个 Mach-O")
    return 0


def sha256(path: str) -> str:
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def read_text(path: str) -> Optional[str]:
    try:
        with open(path, encoding="utf-8") as f:
            return f.read()
    except FileNotFoundError:
        return None


def parse_lock(path: str) -> List[dict]:
    rows = []
    with open(path, encoding="utf-8") as f:
        for line in f:
            if not line.strip() or line.lstrip().startswith("#"):
                continue
            name, version, lic, file, digest, url = [c.strip() for c in line.split("|")]
            rows.append({"name": name, "version": version, "license": lic,
                         "file": file, "sha256": digest, "url": url})
    return rows


def manifest(args: argparse.Namespace) -> int:
    root = args.dir
    data = {
        "generated_at": datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "deployment_target": args.minos,
        "source_commit": os.environ.get("GITHUB_SHA") or "",
        "workflow_run": "",
        "components": parse_lock(args.lock),
        "patches": [],
        "toolchain": {},
        "ffmpeg": {},
        "mpv": {},
        "files": [],
    }
    if os.environ.get("GITHUB_RUN_ID"):
        data["workflow_run"] = "{}/{}/actions/runs/{}".format(
            os.environ.get("GITHUB_SERVER_URL", "https://github.com"),
            os.environ.get("GITHUB_REPOSITORY", ""), os.environ["GITHUB_RUN_ID"])
    if os.path.isdir(args.patches):
        for comp in sorted(os.listdir(args.patches)):
            d = os.path.join(args.patches, comp)
            for p in sorted(os.listdir(d)) if os.path.isdir(d) else []:
                if p.endswith(".patch"):
                    data["patches"].append({"component": comp, "file": p,
                                            "sha256": sha256(os.path.join(d, p))})
    for arch in ARCHS:
        info = os.path.join(root, "info", arch)
        data["toolchain"][arch] = (read_text(os.path.join(info, "toolchain.txt")) or "").strip().splitlines()
        comps = read_text(os.path.join(info, "ffmpeg-components.txt"))
        data["ffmpeg"].setdefault("components", {})[arch] = comps.split() if comps else []
    info = os.path.join(root, "info", "arm64")
    cfg = read_text(os.path.join(info, "ffmpeg-configure-args.txt")) or ""
    data["ffmpeg"]["configure_args"] = cfg.splitlines()
    summary = read_text(os.path.join(info, "ffmpeg-configure-summary.txt")) or ""
    data["ffmpeg"]["license"] = next((l.split(":", 1)[1].strip() for l in summary.splitlines()
                                      if l.startswith("License:")), "")
    data["mpv"]["features"] = (read_text(os.path.join(info, "mpv-features.txt")) or "").strip().strip('"').split()
    if args.mpv_version:
        data["mpv"]["version_output"] = (read_text(args.mpv_version) or "").strip().splitlines()

    machs = set(machos(root))
    skip = {"manifest.json", "MANIFEST.txt"}
    for path in walk_files(root):
        rel = os.path.relpath(path, root)
        if os.path.isdir(path) or rel in skip:
            continue
        entry = {"path": rel, "size": os.path.getsize(path), "sha256": sha256(path)}
        if rel in machs:
            entry["lipo_info"] = run("lipo", "-info", path).strip()
            entry["archs"] = run("lipo", "-archs", path).split()
            entry["slices"] = {a: load_commands(path, a) for a in entry["archs"]}
        data["files"].append(entry)

    with open(os.path.join(root, "manifest.json"), "w", encoding="utf-8") as f:
        json.dump(data, f, ensure_ascii=False, indent=2)
        f.write("\n")
    with open(os.path.join(root, "MANIFEST.txt"), "w", encoding="utf-8") as f:
        f.write(render_text(data))
    return 0


def render_text(d: dict) -> str:
    out = []
    w = out.append
    w("nagare 的 macOS libmpv（universal：arm64 + x86_64）")
    w(f"生成时间 {d['generated_at']}  部署目标 macOS {d['deployment_target']}")
    if d["source_commit"]:
        w(f"源码提交 {d['source_commit']}")
    if d["workflow_run"]:
        w(f"构建记录 {d['workflow_run']}")
    w("")
    w("== 组件 ==")
    for c in d["components"]:
        w(f"{c['name']:<22} {c['version']:<12} {c['license']}")
        w(f"{'':<22} {c['url']}")
        w(f"{'':<22} sha256 {c['sha256']}")
    w("")
    w("== 补丁 ==")
    for p in d["patches"] or []:
        w(f"{p['component']}/{p['file']}  sha256 {p['sha256']}")
    if not d["patches"]:
        w("（无）")
    w("")
    w("== 工具链 ==")
    for arch, lines in d["toolchain"].items():
        w(f"[{arch}]")
        out.extend("  " + l for l in lines)
    w("")
    w(f"== FFmpeg ==\n许可证：{d['ffmpeg'].get('license', '')}")
    w("configure：" + " ".join(d["ffmpeg"].get("configure_args", [])))
    comps = d["ffmpeg"].get("components", {}).get("arm64", [])
    w(f"开启的组件（{len(comps)}）：" + " ".join(comps))
    w("")
    w("== mpv ==")
    out.extend(d["mpv"].get("version_output", []))
    w("特性：" + " ".join(d["mpv"].get("features", [])))
    w("")
    w("== Mach-O（lipo -info / minos / otool -L）==")
    for f in d["files"]:
        if "slices" not in f:
            continue
        w(f"{f['path']}  {f['size']} 字节")
        w(f"  {f['lipo_info']}")
        for arch, s in f["slices"].items():
            w(f"  [{arch}] minos {s['minos']} sdk {s['sdk']} id {s['id'] or '-'} rpath {s['rpaths'] or '-'}")
        first = next(iter(f["slices"].values()))
        for dep in first["deps"]:
            w(f"    {dep}")
    w("")
    w("== 全部文件（sha256）==")
    for f in d["files"]:
        w(f"{f['sha256']}  {f['size']:>10}  {f['path']}")
    return "\n".join(out) + "\n"


def main() -> int:
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = p.add_subparsers(dest="cmd", required=True)
    c = sub.add_parser("check")
    c.add_argument("dir")
    c.add_argument("--minos", default="13.0")
    m = sub.add_parser("manifest")
    m.add_argument("dir")
    m.add_argument("--lock", required=True)
    m.add_argument("--patches", required=True)
    m.add_argument("--minos", default="13.0")
    m.add_argument("--mpv-version")
    a = p.parse_args()
    return check(a) if a.cmd == "check" else manifest(a)


if __name__ == "__main__":
    sys.exit(main())
