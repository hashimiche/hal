summary: Ansible Automation Platform jobs log in to Vault with a JWT bound to their organization, then fetch secrets and SSH certificates.
read: hal vault aap | hal vault status | hal creds status
actions: hal vault aap enable | hal vault aap update | hal vault aap disable
docs:
- Vault JWT verification: https://developer.hashicorp.com/vault/docs/auth/jwt#jwt-verification
- Vault signed SSH certificates: https://developer.hashicorp.com/vault/docs/secrets/ssh/signed-ssh-certificates#client-key-signing

# Vault with Ansible Automation Platform (AAP)

## The mechanism

An automation controller runs jobs for several teams and has to prove to Vault which
team a job belongs to, without keeping a Vault credential for each one. The same
idea as the CI topic (`vault/jwt`) applies: Vault trusts the platform that runs the
job. Two things differ here: the platform is not GitLab, and the claim that
separates the teams is the organization, not a project or a Git ref.

The controller acts as an OpenID Connect issuer. It publishes a discovery document,
and with it the public keys it signs with. For a job, it signs a short-lived JWT.
The job's Vault credential in the controller sends that JWT to Vault with the name of
a role. Vault fetches the keys from the discovery URL, checks the signature and the
audience, then compares the claims in the token with the role. The role binds one
claim, the name of the controller organization, so a job from one organization can
use only the role made for that organization, and gets only that role's policy.

If the discovery endpoint is served with a certificate from a private authority,
Vault has to be given that authority's certificate in the auth method's
configuration. Without it, Vault cannot fetch the keys and every login fails.

The lab adds a second use of the same login: **SSH certificate signing**. Vault's SSH
secrets engine holds a signing key (a certificate authority). A job presents its
public key, and Vault returns that key signed as a short-lived user certificate. A
server that trusts the authority's public key accepts the certificate, so no
long-lived key has to be copied to the server.

## How this lab stages it

- **Two containers.** `hal vault aap enable` starts the controller as `hal-aap`
  (hostname `aap.demo.local`, its HTTPS port published on the host port 443 by
  default) and a target server `hal-ssh-target` (hostname `ssh-target.demo.local`).
  Nothing here is shared with another lab. The controller runs privileged, with the
  host cgroup namespace and in-memory `/run` mounts, and takes minutes to be ready:
  HAL polls its ping endpoint for at least twenty minutes before it gives up.
- **The controller image is not built by HAL.** HAL runs the image `ubi9-aap`, and
  prefers a local one named `local/ubi9-aap` when it exists. The image name, tag and
  host port are flags (`hal vault aap --help` lists them, with the SSH target's
  image).
- **A separate auth mount, `auth/jwt-aap`.** It is a JWT auth method, not the `jwt`
  mount of the CI scenario, so the two scenarios do not collide. It is configured
  with the controller's discovery URL (`https://hal-aap/o` on the lab network) and
  with the controller's CA certificate, which HAL reads out of the `hal-aap`
  container.
- **Two roles, one per organization.** `aap-development-role` accepts tokens whose
  `aap_controller_organization_name` claim is `Development`, and
  `aap-production-role` the ones for `Production`. Both require the audience
  `http://hal-vault:8200` and take the Vault user from the `sub` claim. The
  organization names and the audience are flags.
- **Policies and seeded values.** `aap-development-policy` can read one path, and
  `aap-production-policy` can read one other. They sit under `secret/data/` as
  `development` and `production`, and HAL seeds each with one demo value, named
  `env_name`. HAL does not mount that KV engine itself: it only writes to the path
  under `secret/`. Vault's documentation says dev mode starts with a KV v2 mount named
  `secret`, which is what this relies on. If the Vault you run has no such mount, the
  seed step would fail. Whether the prod mode has it was not checked.
- **SSH.** Vault mounts the `ssh` engine and generates a signing key. The role
  `ssh-signer-role` signs user certificates (and host certificates) for the user
  `rhel` only, valid 30 minutes by default and one hour at most. The policy
  `ssh-signer` allows signing with that role. Two more roles on `auth/jwt-aap`,
  `aap-ssh-dev-role` and `aap-ssh-prod-role`, are bound to the same two
  organizations and carry that policy.
- **The target trusts Vault's authority.** HAL installs an SSH daemon in
  `hal-ssh-target`, creates the user `rhel`, writes Vault's CA public key into the
  daemon's trusted user keys file and reloads the daemon.
- **The controller side is configured by HAL too,** through the controller's API: the
  organizations `Development` and `Production`, a Vault lookup credential per
  organization (it uses the controller's built-in credential type for Vault with
  OIDC), a custom credential type and credential per organization, an input source
  that fills the custom credential's `env_name` from the Vault path of its
  organization, a project and a job template per organization (named
  `... KV Demo`), an inventory called `SSH Target Inventory` holding the target, and
  an SSH machine credential whose certificate comes from Vault. The projects point at
  a public GitHub repository of third-party sample playbooks, so the controller has to
  reach GitHub, and `enable` stops with an error if the expected playbook does not
  appear in a project within 90 seconds.

## What to expect

- The job template of an organization is built to show the `env_name` value of that
  organization's path: `Development` reads `development`, `Production` reads
  `production`. What the playbook prints comes from a third-party repository and was
  not checked here. On the Vault side the separation holds either way: a token from
  one organization matches only its own role, and that role's policy covers one path.
- If a login is refused, the usual causes are the audience (the job's token must
  carry `http://hal-vault:8200`), the organization name (it is compared exactly),
  and Vault not trusting the controller's certificate authority. The last one
  appears if the controller is rebuilt with a new authority and Vault still holds
  the old one. `update` reads the controller's certificate again.
- The SSH certificates are short-lived, so the credential has to be used within its
  lifetime. After 30 minutes, a new job asks Vault for a new certificate.
- The controller's own web login is a lab secret and is not written here.

## Commands

- `hal vault aap` with no action is read-only. It reports whether `hal-aap` and
  `hal-ssh-target` are running, whether the `jwt-aap` mount, the roles, the policies,
  the seeded values, the `ssh` engine, its signing key and its roles exist, and
  whether the controller-side resources are reconciled. It then names the next step.
  `hal vault status` lists `jwt-aap` among the active auth methods and `ssh (ssh)`
  among the secrets engines.
- `enable` needs a running, healthy Vault and stops with an error otherwise. It
  starts `hal-aap` if it is not already running, configures Vault, starts the target,
  then reconciles the controller.
- `update` does not tear anything down. It runs the same steps as `enable` again and
  makes the existing pieces match: the roles and policies are rewritten, the
  controller's objects are checked and fixed. This differs from `ldap` and `userpass`,
  where `update` is a destroy and a rebuild.
- `disable` removes, in this order, the SSH credentials and the other objects HAL made
  in the controller, the SSH engine and the SSH roles and policy in Vault, the
  `jwt-aap` mount with its roles and policies and the two seeded values, and finally
  the `hal-ssh-target` and `hal-aap` containers. It cleans the controller before it
  removes it and does not need Vault to be up: with Vault offline it skips the
  Vault part. It does not remove the `secret` mount itself, which is not HAL's.

The controller login is a lab secret, so it is not written in this file.
`hal creds status` shows it under Vault.
