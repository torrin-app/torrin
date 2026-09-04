import json
import os
import signal
import subprocess
import sys
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

BTN = ".content-protector-form-submit"
REVEAL_TIMEOUT = 120
_lock = threading.Lock()


def _reveal(url):
    from camoufox.sync_api import Camoufox

    with Camoufox(headless="virtual", humanize=True) as browser:
        page = browser.new_page(no_viewport=True)
        page.goto(url, wait_until="domcontentloaded", timeout=60000)
        for _ in range(35):
            page.wait_for_timeout(1000)
            disabled = page.evaluate(
                "() => { const b = document.querySelector('%s'); return b ? b.disabled : true; }" % BTN
            )
            if disabled is False:
                break
        try:
            page.eval_on_selector(BTN, "el => el.click()")
        except Exception:
            pass
        try:
            page.wait_for_load_state("networkidle", timeout=20000)
        except Exception:
            pass
        page.wait_for_timeout(3000)
        return page.content()


def reveal(url):
    proc = subprocess.Popen(
        [sys.executable, __file__, "--worker"],
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        start_new_session=True,
    )
    try:
        out, _ = proc.communicate(url.encode(), timeout=REVEAL_TIMEOUT)
    except subprocess.TimeoutExpired:
        _kill_tree(proc)
        raise RuntimeError("reveal timed out")
    if proc.returncode != 0:
        raise RuntimeError("reveal worker exited %s" % proc.returncode)
    resp = json.loads(out or b"{}")
    if resp.get("error"):
        raise RuntimeError(resp["error"])
    return resp["html"]


def _kill_tree(proc):
    try:
        os.killpg(os.getpgid(proc.pid), signal.SIGKILL)
    except ProcessLookupError:
        pass
    try:
        proc.wait(timeout=10)
    except Exception:
        pass


def _worker():
    url = sys.stdin.buffer.read().decode().strip()
    try:
        sys.stdout.write(json.dumps({"html": _reveal(url)}))
    except Exception as e:
        sys.stdout.write(json.dumps({"error": str(e)}))
    sys.stdout.flush()


class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        n = int(self.headers.get("Content-Length", 0))
        try:
            url = json.loads(self.rfile.read(n) or b"{}").get("url", "")
            if not url:
                return self._json(400, {"error": "url required"})
            with _lock:
                html = reveal(url)
            self._json(200, {"html": html})
        except Exception as e:
            self._json(500, {"error": str(e)})

    def do_GET(self):
        self._json(200, {"ok": True})

    def _json(self, code, obj):
        body = json.dumps(obj).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *a):
        pass


if __name__ == "__main__":
    if len(sys.argv) > 1 and sys.argv[1] == "--worker":
        _worker()
    else:
        ThreadingHTTPServer(("0.0.0.0", 8090), Handler).serve_forever()
