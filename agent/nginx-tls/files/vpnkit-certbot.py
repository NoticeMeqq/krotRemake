#!/usr/bin/python3
"""Run Certbot with its Let's Encrypt HTTPS connections sourced from awg0."""

import http.server
import json
import os
import select
import socket
import subprocess
import sys
import threading


ACME_AUTHORITIES = {
    "acme-v02.api.letsencrypt.org:443",
    "acme-staging-v02.api.letsencrypt.org:443",
}


class AcmeConnect(http.server.BaseHTTPRequestHandler):
    timeout = 15

    def do_CONNECT(self):
        if self.path not in ACME_AUTHORITIES:
            self.send_error(403, "Only Let's Encrypt ACME endpoints are allowed")
            return

        host, _ = self.path.split(":")
        try:
            upstream = self.connect_ipv4(host)
        except OSError as exc:
            print(f"ACME via awg0: {host}: {exc}", file=sys.stderr)
            self.send_error(502, "ACME endpoint is unreachable via awg0")
            return

        with upstream:
            self.send_response_only(200, "Connection established")
            self.end_headers()
            try:
                self.relay(upstream)
            except OSError as exc:
                print(f"ACME via awg0: {host}: {exc}", file=sys.stderr)

    def connect_ipv4(self, host):
        last_error = OSError("no IPv4 address")
        for family, kind, proto, _, peer in socket.getaddrinfo(
            host, 443, socket.AF_INET, socket.SOCK_STREAM
        ):
            upstream = socket.socket(family, kind, proto)
            try:
                upstream.settimeout(8)
                upstream.setsockopt(socket.SOL_SOCKET, socket.SO_BINDTODEVICE, b"awg0\0")
                upstream.bind((self.server.source_ip, 0))
                upstream.connect(peer)
                return upstream
            except OSError as exc:
                upstream.close()
                last_error = exc
        raise last_error

    def relay(self, upstream):
        self.connection.settimeout(15)
        upstream.settimeout(15)
        peers = {self.connection: upstream, upstream: self.connection}
        while peers:
            readable, _, _ = select.select(list(peers), [], [], 60)
            if not readable:
                return
            for sender in readable:
                data = sender.recv(65536)
                if data:
                    peers[sender].sendall(data)
                else:
                    peers.pop(sender).shutdown(socket.SHUT_WR)

    def log_message(self, _format, *_args):
        pass


def tunnel_address():
    interfaces = json.loads(
        subprocess.check_output(["/usr/sbin/ip", "-j", "-4", "addr", "show", "dev", "awg0"])
    )
    for interface in interfaces:
        for address in interface.get("addr_info", []):
            if address["scope"] == "global":
                return address["local"]
    raise OSError("awg0 has no IPv4 address")


def main():
    try:
        source_ip = tunnel_address()
    except (OSError, subprocess.CalledProcessError) as exc:
        print(f"ACME via awg0: {exc}. Check cascade-awg0.", file=sys.stderr)
        return 1

    with http.server.ThreadingHTTPServer(("127.0.0.1", 0), AcmeConnect) as proxy:
        proxy.source_ip = source_ip
        thread = threading.Thread(target=proxy.serve_forever, daemon=True)
        thread.start()
        url = f"http://127.0.0.1:{proxy.server_port}"
        env = dict(os.environ, HTTPS_PROXY=url, https_proxy=url, NO_PROXY="", no_proxy="")
        try:
            return subprocess.call(["/usr/bin/certbot", *sys.argv[1:]], env=env)
        finally:
            proxy.shutdown()
            thread.join()


if __name__ == "__main__":
    sys.exit(main())
