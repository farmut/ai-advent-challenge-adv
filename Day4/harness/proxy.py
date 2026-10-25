"""proxy.py <listen-port> <log.jsonl>: logging pass-through to LM Studio (127.0.0.1:1234).

Every POST is forwarded as is; one JSON line per request is appended to the log: request body,
status, time to first byte, total time and the `usage` block from the (streamed) response.
Only loopback is used; the proxy never talks to anything but LM Studio.
"""
import http.client
import http.server
import json
import sys
import threading
import time

PORT, LOG = int(sys.argv[1]), sys.argv[2]
UPSTREAM = ("127.0.0.1", 1234)
lock = threading.Lock()


class Handler(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *_):
        pass

    def _forward(self, method):
        body = self.rfile.read(int(self.headers.get("Content-Length") or 0))
        t0 = time.monotonic()
        conn = http.client.HTTPConnection(*UPSTREAM, timeout=1800)
        headers = {k: v for k, v in self.headers.items() if k.lower() not in ("host", "content-length", "accept-encoding")}
        conn.request(method, self.path, body=body, headers=headers)
        resp = conn.getresponse()
        self.send_response(resp.status)
        for k, v in resp.getheaders():
            if k.lower() not in ("content-length", "transfer-encoding", "connection"):
                self.send_header(k, v)
        self.send_header("Transfer-Encoding", "chunked")
        self.end_headers()
        first, tail, usage, finish = None, b"", None, None
        while True:
            chunk = resp.read1(65536)
            if not chunk:
                break
            if first is None:
                first = time.monotonic()
            self.wfile.write(b"%x\r\n%s\r\n" % (len(chunk), chunk))
            self.wfile.flush()
            tail = (tail + chunk)[-262144:]
            for line in chunk.split(b"\n"):
                line = line.strip()
                if line.startswith(b"data: {"):
                    try:
                        ev = json.loads(line[6:])
                    except ValueError:
                        continue
                    usage = ev.get("usage") or usage
                    for ch in ev.get("choices") or []:
                        finish = ch.get("finish_reason") or finish
        self.wfile.write(b"0\r\n\r\n")
        t1 = time.monotonic()
        if usage is None:
            try:
                usage = json.loads(tail).get("usage")
            except ValueError:
                pass
        try:
            req = json.loads(body) if body else None
        except ValueError:
            req = None
        rec = {"ts": time.time(), "method": method, "path": self.path, "status": resp.status,
               "ttfb_s": round((first or t1) - t0, 3), "total_s": round(t1 - t0, 3),
               "usage": usage, "finish_reason": finish, "request": req}
        with lock, open(LOG, "a", encoding="utf-8") as f:
            f.write(json.dumps(rec, ensure_ascii=False) + "\n")

    def do_POST(self):
        self._forward("POST")

    def do_GET(self):
        self._forward("GET")


http.server.ThreadingHTTPServer(("127.0.0.1", PORT), Handler).serve_forever()
