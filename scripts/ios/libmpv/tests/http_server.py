#!/usr/bin/env python3
"""verify.sh --simulator 的本机 HTTP 服务端：只监听 127.0.0.1，提供 <目录> 下的文件。

iOS 版经回环地址上的 http（Go 转出来的 /stream）播 NAS 里的片子，这里模拟那条路。
模拟器与宿主共用回环网络，所以模拟器里的 mpv 连得上。

用法：http_server.py <目录> <端口写到这个文件>
随机端口，写进第二个参数指定的文件后开始服务，直到被杀掉。
"""

import functools
import http.server
import sys


def main() -> int:
    root, port_file = sys.argv[1:3]
    handler = functools.partial(http.server.SimpleHTTPRequestHandler, directory=root)
    httpd = http.server.ThreadingHTTPServer(("127.0.0.1", 0), handler)
    with open(port_file, "w", encoding="ascii") as f:
        f.write(str(httpd.server_address[1]))
    httpd.serve_forever()
    return 0


if __name__ == "__main__":
    sys.exit(main())
