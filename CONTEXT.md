# HAL

HAL (HashiCorp Academy Labs) builds disposable HashiCorp product labs on a single
user's machine, and lets that user, or an LLM acting for them, inspect and drive them.

## Language

### Agentic IAM lab

**Agentic IAM lab**:
The Vault lab in which a demo agent reaches lab data on behalf of a persona, and
every access is decided by the IdP and Vault rather than by the agent.
_Avoid_: OBO lab, agentic lab, agent lab

**Demo agent**:
The AI agent HAL deploys inside the Agentic IAM lab. It never acts as itself, only
on behalf of a persona.
_Avoid_: agent (alone: ambiguous with the TFE agent and HAL Plus), bot, assistant

**Persona**:
A demo identity HAL creates in the IdP for the Agentic IAM lab, such as alice,
bob or charlie. It is a lab credential, not the person running HAL.
_Avoid_: user (that is the person running HAL), test user, demo user

**OBO token**:
The token that names both a persona, as its subject, and the demo agent, as its
actor, so that Vault sees who asked and who acts.
_Avoid_: delegation token, agent token, impersonation token

**Ceiling**:
The most Vault access the demo agent can ever have, whichever persona it acts for.
_Avoid_: agent policy (Vault ignores the agent's own policies when it acts for a persona)

**Task scope**:
The Vault accesses a persona consents to for one request, fixed from the prompt
before the demo agent reads any data.
_Avoid_: session scope, RAR (that is the mechanism that carries it)

**Decision point**:
One of the four places that can refuse the demo agent an access: the IdP, the
persona's own rights, the ceiling, and the task scope.
_Avoid_: layer, gate, check
