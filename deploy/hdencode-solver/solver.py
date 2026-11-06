import json
import os
import signal
import subprocess
import sys
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse

BTN = ".content-protector-form-submit"
REVEAL_TIMEOUT = 120
REVEAL_ATTEMPTS = 3
HOSTS = ("rapidgator", "nitroflare", "ddownload", "fikper")
_lock = threading.Lock()


def _proxy():
    p = os.environ.get("SOLVER_PROXY_URL", "")
    if not p:
        return None
    u = urlparse(p)
    if not u.hostname:
        return None
    out = {"server": "http://%s:%d" % (u.hostname, u.port or 8899)}
    if u.username:
        out["username"] = u.username
        out["password"] = u.password or ""
    return out


def _reveal(url):
    from camoufox.sync_api import Camoufox

    opts = {"headless": "virtual", "humanize": True}
    if proxy := _proxy():
        opts["proxy"] = proxy
        opts["geoip"] = True
    with Camoufox(**opts) as browser:
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
    # Run the browser in its own session/process group. A Camoufox/playwright
    # crash (e.g. the Firefox pageError bug) then dies with the worker instead of
    # leaking a zombie that spins the long-lived server; a hang is killed on timeout.
    # The Turnstile solve is probabilistic, so retry a fresh session when a run
    # reveals no hoster links.
    last = "reveal failed"
    for _ in range(REVEAL_ATTEMPTS):
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
            last = "reveal timed out"
            continue
        if proc.returncode != 0:
            last = "reveal worker exited %s" % proc.returncode
            continue
        resp = json.loads(out or b"{}")
        if resp.get("error"):
            last = resp["error"]
            continue
        html = resp.get("html", "")
        if not any(h in html.lower() for h in HOSTS):
            last = "no links revealed"
            continue
        return html
    raise RuntimeError(last)


def _fetch_raw(url):
    from camoufox.sync_api import Camoufox
    from urllib.parse import urlparse

    u = urlparse(url)
    origin = "%s://%s/" % (u.scheme, u.netloc)
    # No proxy: Cloudflare-gated feeds block the Go client's TLS fingerprint, not
    # its IP, so Camoufox's real browser clears them without the metered proxy.
    with Camoufox(headless="virtual", humanize=True) as browser:
        page = browser.new_page(no_viewport=True)
        # Load the HTML homepage first to clear Cloudflare. Navigating straight at
        # a feed URL makes Firefox DOWNLOAD it (page.goto raises "Download is
        # starting"), so instead fetch the feed from inside the page context,
        # which carries the cf_clearance cookie + the real browser fingerprint.
        page.goto(origin, wait_until="domcontentloaded", timeout=60000)
        page.wait_for_timeout(2000)
        return page.evaluate(
            "async (u) => { const r = await fetch(u, {credentials: 'include'}); return await r.text(); }",
            url,
        )


def fetch_url(url):
    last = "fetch failed"
    for _ in range(2):
        proc = subprocess.Popen(
            [sys.executable, __file__, "--fetch-worker"],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            start_new_session=True,
        )
        try:
            out, _ = proc.communicate(url.encode(), timeout=REVEAL_TIMEOUT)
        except subprocess.TimeoutExpired:
            _kill_tree(proc)
            last = "fetch timed out"
            continue
        if proc.returncode != 0:
            last = "fetch worker exited %s" % proc.returncode
            continue
        resp = json.loads(out or b"{}")
        if resp.get("error"):
            last = resp["error"]
            continue
        return resp.get("body", "")
    raise RuntimeError(last)


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


def _fetch_worker():
    url = sys.stdin.buffer.read().decode().strip()
    try:
        sys.stdout.write(json.dumps({"body": _fetch_raw(url)}))
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
            if self.path.rstrip("/") == "/fetch":
                with _lock:
                    body = fetch_url(url)
                return self._json(200, {"body": body})
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
    elif len(sys.argv) > 1 and sys.argv[1] == "--fetch-worker":
        _fetch_worker()
    else:
        ThreadingHTTPServer(("0.0.0.0", 8090), Handler).serve_forever()
