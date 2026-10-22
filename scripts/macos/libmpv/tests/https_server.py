#!/usr/bin/env python3
"""verify.sh 的本机 HTTPS 服务端：只监听 127.0.0.1，提供 <目录> 下的文件。

用法：https_server.py <目录> <证书> <私钥> <端口写到这个文件>
随机端口，写进第四个参数指定的文件后开始服务，直到被杀掉。
"""

import functools
import http.server
import ssl
import sys


def main() -> int:
    root, cert, key, port_file = sys.argv[1:5]
    handler = functools.partial(http.server.SimpleHTTPRequestHandler, directory=root)
    httpd = http.server.ThreadingHTTPServer(("127.0.0.1", 0), handler)
    ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    ctx.load_cert_chain(cert, key)
    httpd.socket = ctx.wrap_socket(httpd.socket, server_side=True)
    with open(port_file, "w", encoding="ascii") as f:
        f.write(str(httpd.server_address[1]))
    httpd.serve_forever()
    return 0


if __name__ == "__main__":
    sys.exit(main())
