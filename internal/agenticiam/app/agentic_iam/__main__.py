"""Entry point: ``python -m agentic_iam chat`` or ``python -m agentic_iam agent``."""

import argparse
import logging
import sys

from .env import ConfigError


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        prog="agentic_iam",
        description="HAL Agentic IAM lab. Configured by environment variables only (see README.md).",
    )
    parser.add_argument(
        "command",
        choices=["chat", "agent"],
        help="chat: the web page and its BFF. agent: the demo agent's internal API.",
    )
    args = parser.parse_args(argv)
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")

    if args.command == "chat":
        from .chat import main as run
    else:
        from .agent import main as run
    try:
        run()
    except ConfigError as err:
        print(f"agentic_iam {args.command}: {err}", file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main())
