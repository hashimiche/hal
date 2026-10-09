"""Small OAuth 2.0 helpers, on the standard library only."""

import base64
import hashlib
import json
import secrets
from typing import Any


def jwt_claims(token: str) -> dict[str, Any]:
    """The payload of a JWT, decoded but NOT verified.

    It is only used to show claims, never to trust them: the party that
    verifies the OBO token is Vault.
    """
    try:
        payload = token.split(".")[1]
        claims = json.loads(base64.urlsafe_b64decode(payload + "=" * (-len(payload) % 4)))
    except (IndexError, ValueError):
        return {}
    return claims if isinstance(claims, dict) else {}


def pkce_pair() -> tuple[str, str]:
    """A PKCE code verifier and its S256 code challenge (RFC 7636)."""
    verifier = secrets.token_urlsafe(64)
    digest = hashlib.sha256(verifier.encode()).digest()
    return verifier, base64.urlsafe_b64encode(digest).rstrip(b"=").decode()


def error_text(body: Any) -> str:
    """An OAuth error response as one line, e.g. ``invalid_grant: Invalid token``."""
    if not isinstance(body, dict) or "error" not in body:
        return str(body)
    description = body.get("error_description")
    return f"{body['error']}: {description}" if description else str(body["error"])
