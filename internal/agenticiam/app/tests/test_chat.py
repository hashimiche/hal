"""The chat's BFF, against a fake OIDC provider and a fake demo agent."""

import base64
import hashlib
import http.client
import json
import secrets
import threading
import time
import unittest
from urllib.parse import parse_qs, urlencode, urlsplit

import fakes
from agentic_iam.chat import ChatApp, ChatConfig, ChatHandler
from agentic_iam.web import make_server

CLIENT_ID = "hal-chat-client-id"
CLIENT_SECRET = "hal-chat-secret-" + secrets.token_urlsafe(8)


class FakeOIDC(fakes.FakeServer):
    """Authentik's hal-chat application: discovery and the authorization code grant with PKCE."""

    def __init__(self, redirect_uri):
        super().__init__()
        self.issuer = f"{self.url}/application/o/hal-chat/"
        self.redirect_uri = redirect_uri
        self.codes = {}
        self.issued = []

    def authorize(self, url):
        """What the browser and Authentik do: alice logs in, the browser gets a code."""
        query = {k: v[0] for k, v in parse_qs(urlsplit(url).query).items()}
        assert query["code_challenge_method"] == "S256" and query["redirect_uri"] == self.redirect_uri
        code = secrets.token_urlsafe(16)
        self.codes[code] = query
        return code, query["state"]

    def handle(self, method, path, headers, body):
        if path == "/application/o/hal-chat/.well-known/openid-configuration":
            return 200, {"issuer": self.issuer,
                         "authorization_endpoint": f"{self.url}/application/o/authorize/",
                         "token_endpoint": f"{self.url}/application/o/token/",
                         "end_session_endpoint": f"{self.url}/application/o/hal-chat/end-session/"}
        form = {k: v[0] for k, v in parse_qs(body.decode()).items()}
        request = self.codes.pop(form.get("code"), None)
        challenge = base64.urlsafe_b64encode(hashlib.sha256(form.get("code_verifier", "").encode()).digest())
        if (request is None or form.get("client_secret") != CLIENT_SECRET
                or challenge.rstrip(b"=").decode() != request["code_challenge"]):
            return 400, {"error": "invalid_grant"}
        tokens = {
            "access_token": f"alice-access-{secrets.token_urlsafe(8)}",
            "id_token": fakes.fake_jwt({"iss": self.issuer, "aud": CLIENT_ID, "sub": "a1b2",
                                        "preferred_username": "alice", "nonce": request["nonce"],
                                        "exp": int(time.time()) + 300}),
            "token_type": "Bearer",
            "expires_in": 300,
        }
        self.issued += [tokens["access_token"], tokens["id_token"]]
        return 200, tokens


class FakeAgent(fakes.FakeServer):
    def __init__(self):
        super().__init__()
        self.runs = []

    def handle(self, method, path, headers, body):
        if path == "/plan":
            return 200, {"scopes": ["vault:finance-reports"], "answer": None}
        self.runs.append(json.loads(body))
        return 200, {"answer": "The Q3 results: ...", "trace": {"task_scope": ["vault:finance-reports"]}}


class Browser:
    """A browser without JavaScript: it keeps the session cookie and records all it receives."""

    def __init__(self, base_url):
        self.netloc = urlsplit(base_url).netloc
        self.cookie = None
        self.received = []

    def request(self, method, path, body=None, content_type="application/json"):
        headers = {"Cookie": self.cookie} if self.cookie else {}
        data = None
        if body is not None:
            data, headers["Content-Type"] = json.dumps(body).encode(), content_type
        connection = http.client.HTTPConnection(self.netloc, timeout=10)
        connection.request(method, path, body=data, headers=headers)
        response = connection.getresponse()
        payload = response.read()
        connection.close()
        self.received.append(f"{response.headers}\n{payload.decode()}")
        if set_cookie := response.headers.get("Set-Cookie"):
            self.cookie = set_cookie.split(";")[0]
        return response, payload


class ChatTest(unittest.TestCase):
    def setUp(self):
        server = make_server(ChatHandler, None, port=0, host="127.0.0.1")
        public_url = f"http://127.0.0.1:{server.server_address[1]}"
        self.oidc = FakeOIDC(public_url + "/callback")
        self.agent = FakeAgent()
        server.app = ChatApp(ChatConfig.from_env({
            "PUBLIC_URL": public_url,
            "OIDC_ISSUER": self.oidc.issuer,
            "OIDC_CLIENT_ID": CLIENT_ID,
            "OIDC_CLIENT_SECRET": CLIENT_SECRET,
            "AGENT_URL": self.agent.url,
        }))
        threading.Thread(target=server.serve_forever, daemon=True).start()
        for closer in (server.server_close, server.shutdown, self.oidc.close, self.agent.close):
            self.addCleanup(closer)
        self.browser = Browser(public_url)

    def login(self):
        response, _ = self.browser.request("GET", "/login")
        self.assertEqual(response.status, 302)
        self.assertIn("HttpOnly", response.headers["Set-Cookie"])
        self.assertIn("SameSite=Lax", response.headers["Set-Cookie"])
        return self.oidc.authorize(response.headers["Location"])

    def test_serves_the_page_under_a_strict_content_security_policy(self):
        for path, kind in (("/", "text/html"), ("/app.js", "text/javascript"), ("/app.css", "text/css")):
            with self.subTest(path=path):
                response, _ = self.browser.request("GET", path)
                self.assertEqual(response.status, 200)
                self.assertTrue(response.headers["Content-Type"].startswith(kind))
                self.assertIn("default-src 'self'", response.headers["Content-Security-Policy"])

    def test_the_persona_logs_in_and_runs_a_task_without_seeing_a_token(self):
        code, state = self.login()
        response, _ = self.browser.request("GET", "/callback?" + urlencode({"code": code, "state": state}))
        self.assertEqual((response.status, response.headers["Location"]), (302, "/"))

        response, payload = self.browser.request("GET", "/api/me")
        self.assertEqual(json.loads(payload), {"persona": "alice"})

        _, payload = self.browser.request("POST", "/api/plan", {"prompt": "Q3 results"})
        plan = json.loads(payload)
        self.assertEqual(plan["scopes"], ["vault:finance-reports"])

        response, payload = self.browser.request("POST", "/api/run", {"task_id": plan["task_id"]})
        self.assertEqual(response.status, 200)
        self.assertEqual(json.loads(payload)["answer"], "The Q3 results: ...")
        self.assertEqual(self.agent.runs, [{"prompt": "Q3 results", "scopes": ["vault:finance-reports"],
                                            "subject_token": self.oidc.issued[0]}])

        for token in self.oidc.issued:
            self.assertNotIn(token, "\n".join(self.browser.received))

    def test_consent_applies_to_the_planned_task_only(self):
        code, state = self.login()
        self.browser.request("GET", "/callback?" + urlencode({"code": code, "state": state}))
        response, _ = self.browser.request("POST", "/api/run", {"task_id": "not-planned"})
        self.assertEqual(response.status, 409)
        self.assertEqual(self.agent.runs, [])

    def test_the_callback_must_match_the_login_state(self):
        code, _ = self.login()
        response, _ = self.browser.request("GET", "/callback?" + urlencode({"code": code, "state": "forged"}))
        self.assertEqual(response.status, 400)
        response, _ = self.browser.request("GET", "/api/me")
        self.assertEqual(response.status, 401)

    def test_the_api_needs_a_login_and_json(self):
        response, _ = self.browser.request("POST", "/api/plan", {"prompt": "Q3 results"})
        self.assertEqual(response.status, 401)
        code, state = self.login()
        self.browser.request("GET", "/callback?" + urlencode({"code": code, "state": state}))
        response, _ = self.browser.request("POST", "/api/plan", {"prompt": "Q3 results"}, content_type="text/plain")
        self.assertEqual(response.status, 415)

    def test_logout_ends_the_session_and_the_idp_session(self):
        code, state = self.login()
        self.browser.request("GET", "/callback?" + urlencode({"code": code, "state": state}))
        session_cookie = self.browser.cookie
        response, _ = self.browser.request("GET", "/logout")
        self.assertTrue(response.headers["Location"].startswith(f"{self.oidc.url}/application/o/hal-chat/end-session/"))
        self.assertIn("Max-Age=0", response.headers["Set-Cookie"])
        self.browser.cookie = session_cookie
        response, _ = self.browser.request("GET", "/api/me")
        self.assertEqual(response.status, 401)


if __name__ == "__main__":
    unittest.main()
