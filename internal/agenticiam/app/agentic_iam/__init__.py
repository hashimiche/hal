"""HAL's Agentic IAM lab: the chat (a web page and its BFF) and the demo agent.

A persona logs in to the chat through Authentik. The demo agent acts for them
with an OBO token obtained by token exchange, and Vault decides each access to
the lab data in MariaDB. See ADR 0004 in the HAL repository.
"""
