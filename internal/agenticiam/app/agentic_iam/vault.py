"""Vault, as the demo agent sees it: one read of database credentials per tool call."""

import re
from dataclasses import dataclass, field

from .web import CallError, call_json

# TODO(spike): open question 5 of ADR 0004, still blocked by the Vault license.
# Vault may or may not say which decision point refused a request. Map the
# markers of its error text to decision points here once the spike has seen
# real 403s. Until a marker matches, decided_by stays None and the
# transparency panel shows Vault's own words. The 2.1.x changelog names
# RAR_NO_MATCH, a refusal by the token's authorization_details.
DECISION_MARKERS = {
    "rar_no_match": "task_scope",
}


class VaultError(Exception):
    """Vault could not answer: unreachable, or an error that is not a refusal."""


class VaultRefused(Exception):
    """Vault refused the read (HTTP 403)."""

    def __init__(self, errors: list[str]):
        self.text = "; ".join(_clean(e) for e in errors) or "permission denied"
        super().__init__(self.text)

    @property
    def decided_by(self) -> str | None:
        """The decision point that refused, when Vault's error text says so."""
        text = self.text.lower()
        return next((point for marker, point in DECISION_MARKERS.items() if marker in text), None)


@dataclass(frozen=True)
class Lease:
    """Ephemeral MariaDB credentials issued by Vault's database secrets engine."""

    username: str
    password: str = field(repr=False)
    ttl: int = 0


class Vault:
    def __init__(self, addr: str):
        self.addr = addr.rstrip("/")

    def read_creds(self, path: str, obo_token: str) -> Lease:
        """Read ``<mount>/creds/<role>`` with the OBO JWT itself as the Vault token.

        This is a plain logical read: the KV helpers do not work with an OBO
        token, and this is a database mount anyway.
        """
        try:
            answer = call_json(f"{self.addr}/v1/{path}", headers={"X-Vault-Token": obo_token})
        except CallError as err:
            errors = err.body.get("errors") if isinstance(err.body, dict) else None
            if err.status == 403:
                raise VaultRefused([str(e) for e in errors or []]) from None
            raise VaultError(str(err)) from None
        data = answer.get("data") or {}
        if not data.get("username") or not data.get("password"):
            raise VaultError(f"no credentials in the answer for {path}")
        return Lease(data["username"], data["password"], int(answer.get("lease_duration") or 0))


def _clean(error: str) -> str:
    """Vault's multi-error text ("1 error occurred:\\n\\t* permission denied") as one line."""
    error = re.sub(r"^\d+ errors? occurred:\s*", "", error.strip())
    return re.sub(r"\s*\*\s+", " ", error).strip()
