summary: GitLab CI logs into Vault with a JWT bound to a tag.
read: hal vault jwt | hal vault status | hal creds status
actions: hal vault jwt enable | hal vault jwt update | hal vault jwt disable
docs:
- Vault JWT auth: https://developer.hashicorp.com/vault/docs/auth/jwt#bound-claims
- GitLab CI JWT: https://docs.gitlab.com/ci/secrets/id_token_authentication/#token-payload

# Vault JWT auth with GitLab CI

## The mechanism

A CI job has to prove who it is to Vault without holding a stored secret. This is
the "secret zero" problem, and the JWT auth method solves it by trusting the
platform that runs the job instead of the job itself.

The platform signs a short-lived JWT for each job. The job sends it to Vault with
the name of a role. Vault checks the signature against the issuer's published
public keys, checks the issuer and the audience, then compares the claims in the
token with the role's bound claims. If everything matches, Vault returns a token
that carries the role's policies. Nothing was shared in advance except the trust
in the issuer.

## How this lab stages it

This is CI authentication, not a human login. A person signing in to Vault from a
browser through an identity provider is browser SSO, which is the OIDC topic and a
different scenario.

- **GitLab is the issuer.** `hal vault jwt enable` starts a GitLab CE container
  (`hal-gitlab`), or reuses the one already running for another scenario. Vault is
  configured with GitLab's public key set and with GitLab's own URL as the bound
  issuer. The URL is the `gitlab.localhost` alias on the lab network, which is
  GitLab's external URL, so the `iss` claim of every token matches it.
- **The role is `cicd-role`**, on the auth method mounted at `auth/jwt`. It accepts
  tokens whose audience is `vault`, takes the Vault user from the `user_login`
  claim, and compares bound claims as globs.
- **The bound claims are on the project and the ref.** `project_path` must be
  `root/secret-zero` and `ref` must match `v*`. Only pipelines of the demo project
  can use the role, and only when they run on a tag whose name starts with `v`.
- **The tags `v*` are protected in GitLab**, so only a maintainer can create them.
  Without that, the `ref` check would prove nothing, because anyone allowed to
  create a tag could name one to match.
- **The role grants the policy `cicd-read`.** It can read one path, `cicd`, in a
  KV v2 mount named `kv-jwt`. The value stored there is a demo value.

The demo project is `root/secret-zero` (public, default branch `main`). Its
pipeline has one job, `vault-auth`. The job asks GitLab for an ID token with the
audience `vault` (the `id_tokens` keyword), posts it to Vault's JWT login for
`cicd-role` over the lab's Docker network (`http://hal-vault:8200`), then reads the
secret with the token Vault returns and prints it. It runs on a GitLab Runner
container (`hal-gitlab-runner`, shell executor, tag `hal-jwt-shell`) that the
command registers.

## What to expect

- The pipeline file is committed to `main`, so the first pipeline runs on the ref
  `main`. That ref does not match `v*`, so by the role's bound claims Vault is
  expected to refuse that login and the job to fail. That is the guardrail
  working, not a broken lab.
- Creating a tag such as `v1.0.0` on the project starts a pipeline whose ref
  matches. The job logs in and prints the demo value.
- Letting branches log in too means changing the role's bound claims. The lab does
  not do it.
- A login refused for a reason other than the ref usually comes from the audience
  (the job's token must say `vault`), from the issuer (it must equal GitLab's
  external URL), or from the project path.
- If no runner is online, the job never starts. `hal vault jwt` shows whether a
  runner is active.

## Commands

- `hal vault jwt` with no action is read-only. It reports GitLab, the demo
  repository, the runner and whether the JWT method is configured, then names the
  next step. `hal vault status` lists `jwt` among the active auth methods.
- `enable` needs a running, healthy Vault. It stops with an error otherwise, so
  start Vault first (see the Vault topic). It boots or reuses GitLab, creates the
  project, the runner and the protected tags, configures Vault, and commits the
  pipeline. The GitLab and runner images and the GitLab port can be overridden with
  flags; `hal vault jwt --help` lists them. The port is ignored when GitLab is
  already running.
- `update` removes what `disable` removes, then runs `enable` again. Use it when
  the status says the environment is partially degraded.
- `disable` disables `auth/jwt`, unmounts `kv-jwt`, deletes the policy
  `cicd-read`, removes the runner and releases this scenario's claim on GitLab.
  GitLab itself stops only when no other scenario and no Terraform Enterprise
  runtime still needs it.

The GitLab web UI needs a login. It is a lab secret, so it is not written in this
file: it is the GitLab entry under Vault in the credentials that `hal vault status`
prints, and `hal creds status` shows the same tree.
