"""The chat: a web page and its backend-for-frontend (BFF).

The persona logs in to Authentik's hal-chat application with the authorization
code flow and PKCE. Their access token stays in this process: the browser only
holds an HttpOnly session cookie. For each prompt, the BFF asks the demo agent
for the task scope, asks the persona to consent to it, then hands the prompt,
the persona's access token and the consented scopes to the demo agent.
"""

import logging
import os
import secrets
import threading
import time
from collections.abc import Mapping
from dataclasses import dataclass, field
from http.cookies import CookieError, SimpleCookie
from pathlib import Path
from typing import Any
from urllib.parse import parse_qs, urlencode, urlsplit

from .env import Env
from .idp import Client
from .oauth import error_text, jwt_claims, pkce_pair
from .web import CallError, Handler, call_json, make_server, post_form, post_json, serve

log = logging.getLogger("agentic_iam")

SESSION_COOKIE = "hal_chat_session"
SESSION_TTL = 8 * 3600
LOGIN_SCOPES = "openid profile email"
TOKEN_MARGIN = 15  # seconds: an access token this close to expiry counts as expired
AGENT_TIMEOUT = 60

ASSETS = {
    "/": ("index.html", "text/html; charset=utf-8"),
    "/app.js": ("app.js", "text/javascript; charset=utf-8"),
    "/app.css": ("app.css", "text/css; charset=utf-8"),
}


@dataclass(frozen=True)
class ChatConfig:
    port: int
    public_url: str
    issuer: str
    client: Client
    agent_url: str

    @classmethod
    def from_env(cls, environ: Mapping[str, str] = os.environ) -> "ChatConfig":
        env = Env(environ)
        config = cls(
            port=env.port("LISTEN_PORT", 8080),
            public_url=env.require("PUBLIC_URL").rstrip("/"),
            issuer=env.get("OIDC_ISSUER", "http://authentik.localhost:9100/application/o/hal-chat/"),
            client=Client(env.require("OIDC_CLIENT_ID"), env.require("OIDC_CLIENT_SECRET")),
            agent_url=env.require("AGENT_URL").rstrip("/"),
        )
        env.check()
        return config


@dataclass
class Session:
    sid: str = field(repr=False)
    expires: float
    login: dict[str, str] | None = field(default=None, repr=False)  # state, nonce, PKCE verifier
    persona: str | None = None
    access_token: str | None = field(default=None, repr=False)
    token_expires: float = 0.0
    task: dict[str, Any] | None = None  # the planned task that awaits consent

    @property
    def logged_in(self) -> bool:
        return bool(self.persona and self.access_token and self.token_expires > time.time() + TOKEN_MARGIN)


class Sessions:
    """Server-side sessions, in memory: restarting the chat logs every persona out."""

    def __init__(self, ttl: float = SESSION_TTL):
        self._ttl = ttl
        self._lock = threading.Lock()
        self._items: dict[str, Session] = {}

    def create(self) -> Session:
        now = time.time()
        session = Session(sid=secrets.token_urlsafe(32), expires=now + self._ttl)
        with self._lock:
            self._items = {sid: s for sid, s in self._items.items() if s.expires > now}
            self._items[session.sid] = session
        return session

    def get(self, sid: str) -> Session | None:
        with self._lock:
            session = self._items.get(sid)
        return session if session and session.expires > time.time() else None

    def delete(self, sid: str) -> None:
        with self._lock:
            self._items.pop(sid, None)


class LoginError(Exception):
    """The login could not complete."""


class OIDC:
    """The BFF's OpenID Connect client for hal-chat, with endpoints discovered from the issuer."""

    def __init__(self, issuer: str, client: Client, redirect_uri: str):
        self.issuer = issuer
        self.client = client
        self.redirect_uri = redirect_uri
        self._metadata: dict[str, Any] | None = None

    def metadata(self) -> dict[str, Any]:
        # Discovered at first use: Authentik may still be starting when the chat starts.
        if self._metadata is None:
            url = self.issuer.rstrip("/") + "/.well-known/openid-configuration"
            try:
                self._metadata = call_json(url)
            except CallError as err:
                raise LoginError(f"OIDC discovery failed: {err}") from None
        return self._metadata

    def authorization_url(self, state: str, nonce: str, code_challenge: str) -> str:
        query = urlencode({
            "response_type": "code",
            "client_id": self.client.client_id,
            "redirect_uri": self.redirect_uri,
            "scope": LOGIN_SCOPES,
            "state": state,
            "nonce": nonce,
            "code_challenge": code_challenge,
            "code_challenge_method": "S256",
        })
        return f"{self.metadata()['authorization_endpoint']}?{query}"

    def redeem(self, code: str, verifier: str, nonce: str) -> tuple[dict[str, Any], dict[str, Any]]:
        """Redeem an authorization code: the token response and the ID token's claims."""
        try:
            tokens = post_form(self.metadata()["token_endpoint"], {
                "grant_type": "authorization_code",
                "code": code,
                "redirect_uri": self.redirect_uri,
                "code_verifier": verifier,
                "client_id": self.client.client_id,
                "client_secret": self.client.client_secret,
            })
        except CallError as err:
            raise LoginError(f"the IdP refused the authorization code: {error_text(err.body)}") from None

        # The ID token comes straight from the token endpoint, so its claims
        # are checked but not its signature (OpenID Connect Core 3.1.3.7,
        # which assumes TLS in production). It only names the persona here.
        claims = jwt_claims(tokens.get("id_token", ""))
        audience = claims.get("aud")
        if (claims.get("iss") != self.metadata().get("issuer")
                or self.client.client_id not in (audience if isinstance(audience, list) else [audience])
                or claims.get("nonce") != nonce
                or claims.get("exp", 0) < time.time()
                or not tokens.get("access_token")):
            raise LoginError("the ID token does not match this login")
        return tokens, claims

    def logout_url(self, return_to: str) -> str | None:
        """The IdP's logout, so that the next login can be another persona."""
        try:
            endpoint = self.metadata().get("end_session_endpoint")
        except LoginError:
            return None
        if not endpoint:
            return None
        return f"{endpoint}?" + urlencode({"client_id": self.client.client_id,
                                           "post_logout_redirect_uri": return_to})


class ChatApp:
    def __init__(self, config: ChatConfig, oidc: OIDC | None = None):
        self.config = config
        self.oidc = oidc or OIDC(config.issuer, config.client, config.public_url + "/callback")
        self.sessions = Sessions()
        static = Path(__file__).parent / "static"
        self.assets = {route: ((static / name).read_bytes(), kind) for route, (name, kind) in ASSETS.items()}

    def call_agent(self, path: str, body: dict[str, Any]) -> dict[str, Any]:
        return post_json(self.config.agent_url + path, body, timeout=AGENT_TIMEOUT)

    def cookie(self, sid: str) -> str:
        secure = "; Secure" if self.config.public_url.startswith("https://") else ""
        return f"{SESSION_COOKIE}={sid}; Path=/; Max-Age={SESSION_TTL}; HttpOnly; SameSite=Lax{secure}"

    @staticmethod
    def expired_cookie() -> str:
        return f"{SESSION_COOKIE}=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax"


class ChatHandler(Handler):
    @property
    def app(self) -> ChatApp:
        return self.server.app

    def do_GET(self) -> None:
        if self.route in self.app.assets:
            payload, kind = self.app.assets[self.route]
            self.send_bytes(200, payload, kind)
            return
        self.dispatch({"/login": self.login, "/callback": self.callback, "/logout": self.logout,
                       "/api/me": self.me, "/healthz": lambda: self.send_json(200, {"status": "ok"})})

    def do_POST(self) -> None:
        self.dispatch({"/api/plan": self.plan, "/api/run": self.run})

    # --- Login ----------------------------------------------------------------

    def login(self) -> None:
        if old := self.session():
            self.app.sessions.delete(old.sid)
        session = self.app.sessions.create()
        verifier, challenge = pkce_pair()
        session.login = {"state": secrets.token_urlsafe(32), "nonce": secrets.token_urlsafe(32),
                         "verifier": verifier}
        try:
            url = self.app.oidc.authorization_url(session.login["state"], session.login["nonce"], challenge)
        except LoginError as err:
            self.text(502, f"Cannot log in: {err}")
            return
        self.redirect(url, headers=(("Set-Cookie", self.app.cookie(session.sid)),))

    def callback(self) -> None:
        query = parse_qs(urlsplit(self.path).query)

        def param(name: str) -> str:
            return (query.get(name) or [""])[0]

        if param("error"):
            self.text(400, f"Login failed: {param('error')} {param('error_description')}".strip())
            return
        session = self.session()
        login = session.login if session else None
        if not session or not login or not secrets.compare_digest(param("state").encode(), login["state"].encode()):
            self.text(400, "This login does not match your session. Start again from the chat page.")
            return
        session.login = None
        try:
            tokens, claims = self.app.oidc.redeem(param("code"), login["verifier"], login["nonce"])
        except LoginError as err:
            self.text(502, f"Login failed: {err}")
            return
        session.persona = claims.get("preferred_username") or claims.get("sub")
        session.access_token = tokens["access_token"]
        session.token_expires = time.time() + int(tokens.get("expires_in") or 300)
        self.redirect("/")

    def logout(self) -> None:
        if session := self.session():
            self.app.sessions.delete(session.sid)
        location = self.app.oidc.logout_url(self.app.config.public_url + "/") or "/"
        self.redirect(location, headers=(("Set-Cookie", self.app.expired_cookie()),))

    # --- JSON API used by the page ------------------------------------------

    def me(self) -> None:
        if session := self.persona_session():
            self.send_json(200, {"persona": session.persona})

    def plan(self) -> None:
        """A prompt in, the task scope that needs the persona's consent out."""
        session = self.persona_session()
        body = self.read_json() if session else None
        if body is None or not (prompt := self.prompt(body)):
            return
        try:
            plan = self.app.call_agent("/plan", {"prompt": prompt})
        except CallError as err:
            log.warning("demo agent /plan: %s", err)
            self.send_json(502, {"error": "the demo agent is unavailable"})
            return
        scopes = plan.get("scopes") or []
        session.task = {"id": secrets.token_urlsafe(9), "prompt": prompt, "scopes": scopes} if scopes else None
        self.send_json(200, {"task_id": session.task["id"] if session.task else None,
                             "scopes": scopes, "answer": plan.get("answer")})

    def run(self) -> None:
        """The persona's consent in, the demo agent's answer and trace out."""
        session = self.persona_session()
        body = self.read_json() if session else None
        if body is None:
            return
        task = session.task
        if not task or body.get("task_id") != task["id"]:
            self.send_json(409, {"error": "no task awaits your consent"})
            return
        session.task = None
        try:
            result = self.app.call_agent("/run", {"prompt": task["prompt"],
                                                  "subject_token": session.access_token,
                                                  "scopes": task["scopes"]})
        except CallError as err:
            log.warning("demo agent /run: %s", err)
            self.send_json(502, {"error": "the demo agent is unavailable"})
            return
        self.send_json(200, {"answer": result.get("answer"), "trace": result.get("trace")})

    # --- Helpers ----------------------------------------------------------------

    def session(self) -> Session | None:
        try:
            morsel = SimpleCookie(self.headers.get("Cookie", "")).get(SESSION_COOKIE)
        except CookieError:
            return None
        return self.app.sessions.get(morsel.value) if morsel else None

    def persona_session(self) -> Session | None:
        """The logged-in persona's session, or None after answering 401."""
        session = self.session()
        if session and session.logged_in:
            return session
        self.send_json(401, {"error": "login_required"})
        return None

    def text(self, status: int, message: str) -> None:
        self.send_bytes(status, f"{message}\n\nBack to the chat: {self.app.config.public_url}/\n".encode(),
                        "text/plain; charset=utf-8")


def main() -> None:
    config = ChatConfig.from_env()
    serve(make_server(ChatHandler, ChatApp(config), config.port))
