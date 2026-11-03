"""Configuration from environment variables, the only way both containers are configured."""

import os
from collections.abc import Mapping


class ConfigError(Exception):
    """The container was started without the configuration it needs."""


class Env:
    """Reads environment variables and reports every missing one at once."""

    def __init__(self, environ: Mapping[str, str] = os.environ):
        self._environ = environ
        self._missing: list[str] = []

    def require(self, name: str) -> str:
        value = self._environ.get(name, "").strip()
        if not value:
            self._missing.append(name)
        return value

    def get(self, name: str, default: str = "") -> str:
        return self._environ.get(name, "").strip() or default

    def port(self, name: str, default: int) -> int:
        value = self.get(name, str(default))
        if not value.isdigit():
            raise ConfigError(f"{name} must be a port number, got {value!r}")
        return int(value)

    def check(self) -> None:
        if self._missing:
            raise ConfigError("missing environment variables: " + ", ".join(self._missing))
