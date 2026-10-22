"""打 Windows 桌面窗口的测试包（给用户在真 Windows 上自己跑；CI 的 Windows 冒烟 job 也打一份当 artifact）。

用法：python build_kit.py <nagare.exe（-tags desktoptest 的构建）> <mpv 目录（dist/mpv-win）> \
          <WebView2 bootstrapper> <输出 zip>

包里（顶层目录 nagare-wintest/）：
  nagare/nagare.exe、nagare/mpv/…、许可证     测试构建 + 内置 mpv（布局与便携 zip 一样）
  media/clip.mkv                              测试片（脚本复制成三集）
  MicrosoftEdgeWebview2Setup.exe              没有 WebView2 运行时时用它装（与安装包内嵌的同一份）
  run-test.cmd / run-manual.cmd               双击入口
  run-wintest.ps1                             测试脚本（UTF-8 带 BOM：Windows PowerShell 5.1 不带 BOM 时按 ANSI 读，中文全乱）
  README-测试说明.txt、手动检查记录.txt         说明与手动检查的记录表（UTF-8 带 BOM、CRLF，记事本直接开）
zip 里的中文文件名打上 UTF-8 标记（Python 的 zipfile 遇到非 ASCII 名字会自动设），资源管理器解压不乱码。
"""
import os
import sys
import zipfile

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.abspath(os.path.join(HERE, "..", "..", ".."))
TOP = "nagare-wintest"


def text_for_windows(path: str, bom: bool = True) -> bytes:
    with open(path, encoding="utf-8") as f:
        s = f.read().replace("\r\n", "\n").replace("\n", "\r\n")
    data = s.encode("utf-8")
    return (b"\xef\xbb\xbf" + data) if bom else data


def main() -> None:
    if len(sys.argv) != 5:
        sys.exit(__doc__)
    exe, mpv_dir, bootstrapper, out = sys.argv[1:]
    for p in (exe, bootstrapper, os.path.join(mpv_dir, "mpv.exe")):
        if not os.path.isfile(p):
            sys.exit("缺少 " + p)
    if os.path.exists(out):
        os.remove(out)
    with zipfile.ZipFile(out, "w", zipfile.ZIP_DEFLATED, compresslevel=6) as z:
        z.write(exe, f"{TOP}/nagare/nagare.exe")
        for name in ("LICENSE", "THIRD_PARTY_NOTICES.md"):
            z.write(os.path.join(ROOT, name), f"{TOP}/nagare/{name}")
        for base, _, files in os.walk(mpv_dir):
            for fn in files:
                src = os.path.join(base, fn)
                rel = os.path.relpath(src, mpv_dir).replace(os.sep, "/")
                z.write(src, f"{TOP}/nagare/mpv/{rel}")
        z.write(os.path.join(HERE, "media", "clip.mkv"), f"{TOP}/media/clip.mkv")
        z.write(bootstrapper, f"{TOP}/MicrosoftEdgeWebview2Setup.exe")
        z.writestr(f"{TOP}/run-wintest.ps1", text_for_windows(os.path.join(HERE, "run-wintest.ps1")))
        for name in ("run-test.cmd", "run-manual.cmd"):
            z.writestr(f"{TOP}/{name}", text_for_windows(os.path.join(HERE, name), bom=False))
        for name in ("README-测试说明.txt", "手动检查记录.txt"):
            z.writestr(f"{TOP}/{name}", text_for_windows(os.path.join(HERE, name)))
    print(f"测试包：{out}（{os.path.getsize(out) // (1 << 20)} MB）")


if __name__ == "__main__":
    main()
