"""The demo agent's side of the IdP: its own login, and one token exchange per task.

Both requests follow the spike result recorded in ADR 0004 (Authentik 2026.8.3).
"""

from dataclasses import dataclass, field

from .oauth import error_text
from .web import CallError, post_form

TOKEN_EXCHANGE_GRANT = "urn:ietf:params:oauth:grant-type:token-exchange"
ACCESS_TOKEN_TYPE = "urn:ietf:params:oauth:token-type:access_token"

# The OAuth errors by which the IdP refuses the delegation itself, as opposed
# to failing on a misconfiguration. Authentik refuses charlie with
# invalid_grant, from a policy binding on the vault-agentic application.
DELEGATION_REFUSALS = {"invalid_grant", "access_denied"}


class IdPError(Exception):
    """The IdP could not be used: unreachable, misconfigured, or it refused the demo agent's own login."""


class DelegationRefused(Exception):
    """The IdP refused to let the demo agent act for this persona: the IdP decision point."""


@dataclass(frozen=True)
class Client:
    client_id: str
    client_secret: str = field(repr=False)


@dataclass(frozen=True)
class ActorLogin:
    """How the demo agent logs in as itself: to hal-demo-agent, as the finance-agent Actor."""

    client: Client  # the hal-demo-agent application
    username: str
    app_password: str = field(repr=False)


class IdP:
    def __init__(self, token_endpoint: str, exchange_client: Client, actor: ActorLogin):
        self.token_endpoint = token_endpoint
        self.exchange_client = exchange_client
        self.actor = actor

    def exchange(self, subject_token: str, scopes: list[str]) -> str:
        """One RFC 8693 token exchange for one task: the persona's token in, the OBO token out."""
        form = {
            "grant_type": TOKEN_EXCHANGE_GRANT,
            "client_id": self.exchange_client.client_id,
            "client_secret": self.exchange_client.client_secret,
            "subject_token": subject_token,
            "subject_token_type": ACCESS_TOKEN_TYPE,
            "actor_token": self.actor_token(),
            "actor_token_type": ACCESS_TOKEN_TYPE,
            # No audience: the target of the exchange is vault-agentic itself.
            "scope": " ".join(["openid", *scopes]),
        }
        try:
            return _access_token(post_form(self.token_endpoint, form))
        except CallError as err:
            if isinstance(err.body, dict) and err.body.get("error") in DELEGATION_REFUSALS:
                raise DelegationRefused(error_text(err.body)) from None
            raise IdPError(f"token exchange failed: {err}") from None

    def actor_token(self) -> str:
        """The demo agent's own access token, sent as the exchange's actor_token.

        An Authentik Actor without a parent must present a JWT issued by a
        provider that vault-agentic federates. The demo agent gets it from
        hal-demo-agent with the client credentials grant and an app password.
        """
        form = {
            "grant_type": "client_credentials",
            "client_id": self.actor.client.client_id,
            "client_secret": self.actor.client.client_secret,
            "username": self.actor.username,
            "password": self.actor.app_password,
            "scope": "openid profile",
        }
        try:
            return _access_token(post_form(self.token_endpoint, form))
        except CallError as err:
            raise IdPError(f"the demo agent could not log in as itself: {err}") from None


def _access_token(answer: dict) -> str:
    token = answer.get("access_token")
    if not isinstance(token, str) or not token:
        raise IdPError("the token endpoint answered without an access_token")
    return token
