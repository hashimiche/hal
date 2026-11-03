"""HTTP plumbing shared by the chat and the demo agent, on the standard library only."""

import json
import logging
import signal
import sys
from collections.abc import Callable
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any
from urllib.error import HTTPError, URLError
from urllib.parse import urlencode, urlsplit
from urllib.request import Request, urlopen

log = logging.getLogger("agentic_iam")

MAX_BODY = 64 * 1024
MAX_PROMPT = 500
TIMEOUT = 15

SECURITY_HEADERS = (
    ("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'"),
    ("X-Content-Type-Options", "nosniff"),
    ("Referrer-Policy", "no-referrer"),
    ("Cache-Control", "no-store"),
)


# --- Client side --------------------------------------------------------------


class CallError(Exception):
    """Another service answered with an error status, or could not be reached."""

    def __init__(self, url: str, status: int, body: Any):
        self.url = url
        self.status = status  # 0 when the service could not be reached
        self.body = body  # the decoded JSON body when there is one, else text
        super().__init__(f"{urlsplit(url).netloc}: HTTP {status or 'unreachable'}: {body}")


def call_json(url: str, *, data: bytes | None = None, headers: dict[str, str] | None = None,
              timeout: float = TIMEOUT) -> dict:
    request = Request(url, data=data, headers={"Accept": "application/json", **(headers or {})})
    try:
        with urlopen(request, timeout=timeout) as response:
            return json.loads(response.read() or b"{}")
    except HTTPError as err:
        with err:
            body = _decode(err.read())
        raise CallError(url, err.code, body) from None
    except (URLError, OSError) as err:
        raise CallError(url, 0, str(err)) from None


def post_form(url: str, form: dict[str, str], timeout: float = TIMEOUT) -> dict:
    return call_json(url, data=urlencode(form).encode(), timeout=timeout,
                     headers={"Content-Type": "application/x-www-form-urlencoded"})


def post_json(url: str, body: dict, timeout: float = TIMEOUT) -> dict:
    return call_json(url, data=json.dumps(body).encode(), timeout=timeout,
                     headers={"Content-Type": "application/json"})


def _decode(raw: bytes) -> Any:
    try:
        return json.loads(raw)
    except ValueError:
        return raw.decode(errors="replace").strip()


# --- Server side --------------------------------------------------------------


class Handler(BaseHTTPRequestHandler):
    """Base request handler: JSON in and out, security headers, quiet logs.

    The application object is reachable as ``self.server.app``.
    """

    server_version = "hal-agentic-iam"
    sys_version = ""

    def log_request(self, code: int | str = "-", size: int | str = "-") -> None:
        # The path only: a query string can carry an authorization code.
        log.info("%s %s %s", self.command, urlsplit(self.path).path, code)

    def log_message(self, format: str, *args: Any) -> None:
        log.info(format, *args)

    @property
    def route(self) -> str:
        return urlsplit(self.path).path

    def dispatch(self, routes: dict[str, Callable[[], None]]) -> None:
        action = routes.get(self.route)
        if action is None:
            self.send_json(404, {"error": "not found"})
            return
        try:
            action()
        except Exception:
            log.exception("%s %s failed", self.command, self.route)
            self.send_json(500, {"error": "internal error, see the container logs"})

    def read_json(self) -> dict | None:
        """The JSON object in the request body, or None after answering the error."""
        if self.headers.get_content_type() != "application/json":
            # Also a CSRF guard: a cross-site form cannot send this content type.
            self.send_json(415, {"error": "expected application/json"})
            return None
        length = self.headers.get("Content-Length") or "0"
        if not length.isdigit() or int(length) > MAX_BODY:
            self.send_json(413, {"error": "request too large"})
            return None
        try:
            body = json.loads(self.rfile.read(int(length)) or b"{}")
        except ValueError:
            body = None
        if not isinstance(body, dict):
            self.send_json(400, {"error": "expected a JSON object"})
            return None
        return body

    def prompt(self, body: dict) -> str | None:
        """The prompt in a request body, or None after answering 400."""
        prompt = body.get("prompt")
        if isinstance(prompt, str) and prompt.strip() and len(prompt) <= MAX_PROMPT:
            return prompt
        self.send_json(400, {"error": f"the prompt must be 1 to {MAX_PROMPT} characters"})
        return None

    def send_json(self, status: int, body: dict, headers: tuple = ()) -> None:
        self.send_bytes(status, json.dumps(body).encode(), "application/json", headers)

    def send_bytes(self, status: int, payload: bytes, content_type: str, headers: tuple = ()) -> None:
        self.send_response(status)
        for name, value in (*SECURITY_HEADERS, *headers):
            self.send_header(name, value)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def redirect(self, location: str, headers: tuple = ()) -> None:
        self.send_response(302)
        for name, value in (*SECURITY_HEADERS, ("Location", location), *headers):
            self.send_header(name, value)
        self.send_header("Content-Length", "0")
        self.end_headers()


def make_server(handler: type[Handler], app: Any, port: int, host: str = "0.0.0.0") -> ThreadingHTTPServer:
    server = ThreadingHTTPServer((host, port), handler)
    server.daemon_threads = True
    server.app = app
    return server


def serve(server: ThreadingHTTPServer) -> None:
    """Serve until the container is stopped."""
    # As PID 1 in a container, Python ignores SIGTERM unless it handles it.
    signal.signal(signal.SIGTERM, lambda *_: sys.exit(0))
    log.info("listening on port %d", server.server_address[1])
    try:
        server.serve_forever()
    finally:
        server.server_close()
