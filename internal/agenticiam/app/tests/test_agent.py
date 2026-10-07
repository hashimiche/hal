"""The demo agent's API, against a fake IdP, Vault and MariaDB, for the six cases of ADR 0004."""

import json
import threading
import unittest
from urllib.error import HTTPError
from urllib.request import Request, urlopen

import fakes
from agentic_iam.agent import CANNOT_ACT, AgentAPI, AgentConfig, DemoAgent
from agentic_iam.env import ConfigError
from agentic_iam.fake_model import KeywordChatModel
from agentic_iam.web import make_server

# Every message the model saw, across the tests.
SEEN_BY_MODEL: list[str] = []


class RecordingModel(KeywordChatModel):
    def _generate(self, messages, stop=None, run_manager=None, **kwargs):
        SEEN_BY_MODEL.extend(str(m.content) for m in messages)
        return super()._generate(messages, stop, run_manager, **kwargs)


# (case, persona, prompt, task scope, decisions of the tool calls). None: no tool call at all.
CASES = [
    (1, "alice", "Q3 results", ["vault:finance-reports"], [("get_quarterly_results", "allowed")]),
    (2, "charlie", "Q3 results", ["vault:finance-reports"], None),
    (3, "bob", "Q3 results", ["vault:finance-reports"], [("get_quarterly_results", "refused")]),
    (4, "alice", "payroll", ["vault:payroll"], [("get_payroll", "refused")]),
    (5, "alice", "Q2 results", ["vault:finance-reports"],
     [("get_quarterly_results", "allowed"), ("get_forecasts", "refused")]),
    (6, "alice", "Q3 results and forecasts", ["vault:finance-reports", "vault:forecasts"],
     [("get_quarterly_results", "allowed"), ("get_forecasts", "allowed")]),
]


class AgentAPITest(unittest.TestCase):
    def setUp(self):
        self.lab = fakes.FakeLab()
        self.addCleanup(self.lab.close)
        config = AgentConfig.from_env({
            "TOKEN_ENDPOINT": self.lab.token_endpoint,
            "EXCHANGE_CLIENT_ID": fakes.EXCHANGE_CLIENT_ID,
            "EXCHANGE_CLIENT_SECRET": fakes.EXCHANGE_CLIENT_SECRET,
            "ACTOR_CLIENT_ID": fakes.ACTOR_CLIENT_ID,
            "ACTOR_CLIENT_SECRET": fakes.ACTOR_CLIENT_SECRET,
            "ACTOR_APP_PASSWORD": fakes.ACTOR_APP_PASSWORD,
            "VAULT_ADDR": self.lab.url,
        })
        agent = DemoAgent.from_config(config)
        agent.model = RecordingModel()
        agent.db = self.db = fakes.FakeDatabase(self.lab)

        server = make_server(AgentAPI, agent, port=0, host="127.0.0.1")
        threading.Thread(target=server.serve_forever, daemon=True).start()
        self.addCleanup(server.server_close)
        self.addCleanup(server.shutdown)
        self.url = f"http://127.0.0.1:{server.server_address[1]}"

    def post(self, path, body):
        request = Request(self.url + path, data=json.dumps(body).encode(),
                          headers={"Content-Type": "application/json"})
        try:
            with urlopen(request, timeout=10) as response:
                return response.status, json.loads(response.read())
        except HTTPError as err:
            with err:
                return err.code, json.loads(err.read())

    def task(self, persona, prompt):
        """What the BFF does: plan, then run with the persona's token and the planned scopes."""
        status, plan = self.post("/plan", {"prompt": prompt})
        self.assertEqual(status, 200)
        status, result = self.post("/run", {"prompt": prompt, "scopes": plan["scopes"],
                                            "subject_token": self.lab.persona_tokens[persona]})
        self.assertEqual(status, 200)
        return result

    def test_plan_derives_the_task_scope_from_the_prompt(self):
        for case, _, prompt, scopes, _ in CASES:
            with self.subTest(case=case):
                self.assertEqual(self.post("/plan", {"prompt": prompt}), (200, {"scopes": scopes, "answer": None}))
        self.assertEqual(self.lab.exchanges, [], "planning must not exchange tokens")
        self.assertEqual(self.lab.vault_tokens, [], "planning must read no data")

    def test_plan_answers_an_unknown_request_without_a_task(self):
        status, plan = self.post("/plan", {"prompt": "book me a flight"})
        self.assertEqual(status, 200)
        self.assertEqual(plan["scopes"], [])
        self.assertIn("quarterly results", plan["answer"])

    def test_the_six_cases(self):
        for case, persona, prompt, scopes, decisions in CASES:
            with self.subTest(case=case, persona=persona, prompt=prompt):
                trace = self.task(persona, prompt)["trace"]
                self.assertEqual(trace["task_scope"], scopes)
                if decisions is None:
                    self.assertFalse(trace["exchange"]["ok"])
                    self.assertEqual(trace["tool_calls"], [])
                    continue
                self.assertTrue(trace["exchange"]["ok"])
                self.assertEqual([(c["tool"], c["decision"]) for c in trace["tool_calls"]], decisions)
                self.assertEqual(trace["obo"]["sub"], persona)
                self.assertEqual(trace["obo"]["act"], {"sub": "finance-agent"})
                self.assertEqual(trace["obo"]["aud"], fakes.EXCHANGE_CLIENT_ID)
                self.assertEqual([d["path"] for d in trace["obo"]["authorization_details"]],
                                 [fakes.SCOPE_PATHS[s] for s in scopes])

    def test_allowed_calls_use_an_ephemeral_mariadb_user(self):
        result = self.task("alice", "Q3 results")
        call = result["trace"]["tool_calls"][0]
        self.assertEqual(call["vault_path"], "agentic-db/creds/quarterly-results")
        self.assertEqual(call["db_user"], self.db.users[0])
        self.assertEqual((call["lease_ttl"], call["rows"]), (300, 1))
        self.assertIn("Q3: revenue 13.4 M USD", result["answer"])

    def test_the_idp_refusal_is_a_clean_answer(self):
        result = self.task("charlie", "Q3 results")
        self.assertEqual(result["answer"], CANNOT_ACT)
        self.assertEqual(result["trace"]["exchange"],
                         {"ok": False, "error": "invalid_grant: Invalid token", "decided_by": "idp"})
        self.assertEqual(self.lab.vault_tokens, [])

    def test_the_injected_call_is_refused_by_the_task_scope(self):
        result = self.task("alice", "Q2 results")
        forecasts = result["trace"]["tool_calls"][1]
        self.assertFalse(forecasts["in_task_scope"])
        self.assertEqual(forecasts["decided_by"], "task_scope")
        self.assertIn("RAR_NO_MATCH", forecasts["vault_error"])
        self.assertIn("I could not read the forecasts: Vault refused the access", result["answer"])

    def test_other_refusals_name_no_decision_point(self):
        call = self.task("bob", "Q3 results")["trace"]["tool_calls"][0]
        self.assertEqual((call["vault_error"], call["decided_by"], call["db_user"]),
                         ("permission denied", None, None))

    def test_one_token_exchange_per_task(self):
        self.task("alice", "Q2 results")
        self.assertEqual(len(self.lab.exchanges), 1)
        self.assertEqual(self.lab.vault_tokens, self.lab.obo_tokens * 2)
        form = self.lab.exchanges[0]
        self.assertEqual(form["subject_token"], self.lab.persona_tokens["alice"])
        self.assertEqual(form["subject_token_type"], fakes.ACCESS_TOKEN_TYPE)
        self.assertEqual(form["actor_token_type"], fakes.ACCESS_TOKEN_TYPE)
        self.assertEqual(form["scope"], "openid vault:finance-reports")
        self.assertNotIn("audience", form)

    def test_no_token_or_password_leaves_the_demo_agent(self):
        results = [self.task(persona, prompt) for _, persona, prompt, _, _ in CASES]
        exposed = json.dumps(results) + "\n".join(SEEN_BY_MODEL)
        self.assertTrue(self.lab.obo_tokens and self.lab.leases)
        for secret in self.lab.secrets():
            self.assertNotIn(secret, exposed)

    def test_run_rejects_scopes_outside_the_lab(self):
        status, body = self.post("/run", {"prompt": "Q3 results", "scopes": ["vault:everything"],
                                          "subject_token": self.lab.persona_tokens["alice"]})
        self.assertEqual(status, 400)
        self.assertEqual(self.lab.exchanges, [])


class AgentConfigTest(unittest.TestCase):
    def test_reports_every_missing_variable_at_once(self):
        with self.assertRaises(ConfigError) as raised:
            AgentConfig.from_env({})
        self.assertEqual(str(raised.exception), "missing environment variables: EXCHANGE_CLIENT_ID, "
                         "EXCHANGE_CLIENT_SECRET, ACTOR_CLIENT_ID, ACTOR_CLIENT_SECRET, ACTOR_APP_PASSWORD")

    def test_defaults_are_the_lab_on_hal_net(self):
        config = AgentConfig.from_env({name: "x" for name in (
            "EXCHANGE_CLIENT_ID", "EXCHANGE_CLIENT_SECRET", "ACTOR_CLIENT_ID", "ACTOR_CLIENT_SECRET",
            "ACTOR_APP_PASSWORD")})
        self.assertEqual(config.token_endpoint, "http://authentik.localhost:9100/application/o/token/")
        self.assertEqual(config.vault_addr, "http://hal-vault:8200")
        self.assertEqual(config.creds_paths, fakes.CREDS_PATHS)
        self.assertEqual((config.db_host, config.db_port, config.db_name), ("hal-vault-mariadb", 3306, "acme"))
        self.assertEqual((config.port, config.actor.username), (8081, "finance-agent"))


if __name__ == "__main__":
    unittest.main()
