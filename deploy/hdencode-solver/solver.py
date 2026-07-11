import json
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

from camoufox.sync_api import Camoufox

BTN = ".content-protector-form-submit"
_lock = threading.Lock()


def reveal(url):
    with Camoufox(headless=True, humanize=True) as browser:
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
            page.click(BTN, timeout=5000)
        except Exception:
            pass
        try:
            page.wait_for_load_state("networkidle", timeout=20000)
        except Exception:
            pass
        page.wait_for_timeout(3000)
        return page.content()


class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        n = int(self.headers.get("Content-Length", 0))
        try:
            url = json.loads(self.rfile.read(n) or b"{}").get("url", "")
            if not url:
                return self._json(400, {"error": "url required"})
            with _lock:  # one browser at a time (per-request browser is RAM-heavy)
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
    ThreadingHTTPServer(("0.0.0.0", 8090), Handler).serve_forever()
