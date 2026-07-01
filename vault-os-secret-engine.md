# The Vault OS Secret Engine in Plain English

## What It Does

The OS secret engine manages Linux user passwords on remote hosts. It does **not** create users — it only rotates passwords for users that already exist. You register existing accounts with Vault, and from that point Vault owns their credentials.

## How Vault Connects to the Host

Vault SSHes into the host using **password-based authentication**. Host key verification is configurable:

- **TOFU** (trust-on-first-use): Vault trusts and stores the host key on first connect. Easier to set up, less secure.
- **Pinned key**: You supply the host's public key in `authorized_keys` format at registration. Vault rejects connections if the key changes.

## Two Rotation Patterns

### Parent-managed
Vault SSHes into the host as a dedicated **mgmt account** and runs `sudo chpasswd` to change another user's password. The mgmt account needs passwordless sudo for `/usr/sbin/chpasswd`.

```
Vault → SSH as mgmt-user → echo "target-user:newpass" | sudo chpasswd
```

**Key advantage**: Vault never needs the target user's current password to rotate it. If someone changes the password out-of-band, Vault can still forcibly reset it on the next cycle — it can never lose control.

### Standalone (self-rotating)
Vault SSHes into the host **as that user** and runs `passwd` to change its own password.

```
Vault → SSH as self-user → passwd (interactive PTY session)
```

**Key limitation**: Vault's stored password must stay in sync with the actual password. If anyone changes the password out-of-band, Vault is locked out and can no longer rotate.

## What You Need Before You Start

1. A **mgmt account** on each host with passwordless sudo for `chpasswd` (for parent-managed accounts)
2. All **target accounts** already created on the host — Vault does not provision users
3. SSH password authentication enabled on the host
4. Initial passwords for all accounts to register them in Vault

## Configuration Levels

### `os/config` (global)
| Field | Default | Description |
|---|---|---|
| `ssh_host_key_trust_on_first_use` | `false` | Enable TOFU host key verification |
| `max_versions` | `10` | Password versions to retain per account (max 5000) |

### `os/hosts/<name>` (per host)
| Field | Description |
|---|---|
| `address` | IP or hostname Vault SSHes to |
| `port` | SSH port (default 22) |
| `ssh_host_key` | Pinned public key — production alternative to TOFU |
| `password_policy` | Named Vault password policy for this host |
| `custom_metadata` | Arbitrary key-value tags |

### `os/hosts/<name>/accounts/<name>` (per account)
| Field | Description |
|---|---|
| `username` | OS username |
| `password` | Initial password (Vault takes over from here) |
| `parent_account_ref` | Name of the mgmt account — omit for standalone |
| `rotation_period` | Auto-rotation interval in seconds |
| `password_policy` | Override the host-level password policy |
| `custom_metadata` | Arbitrary key-value tags |

## Password Generation

By default, Vault generates a **64-character base62 string** (`[A-Za-z0-9]`). If your target system has password complexity requirements (symbols, length limits), configure a named Vault password policy and attach it at the host or account level.

## Host Entries

One host entry per VM — accounts are scoped under their host. If you manage accounts across multiple VMs, each VM needs its own host entry and its own set of registered accounts (including a mgmt account per host).

## Rotation Triggers

Three ways to rotate a password:

1. **Manual**: `vault write -f os/hosts/<host>/accounts/<name>/rotate`
2. **Periodic**: Set `rotation_period` on the account — rotates on that interval regardless of usage
3. **Scheduled**: Cron-style rotation schedule

There is no native rotate-on-read. To approximate it, either call rotate then read client-side, or add a combined endpoint to the plugin (which avoids the race condition of two separate API calls).

## Audit Trail

The plugin does not inject caller identity into rotation events. Attribution comes from **Vault's audit log**, which captures the token metadata (entity ID, policies, display name) of whoever called the endpoint.

`custom_metadata` on hosts and accounts is stored with the entry and returned when reading the registration path — it does **not** appear in audit log entries for `/rotate` or `/creds` calls, since those endpoints don't include it in their responses.

## Common Design Patterns

| Pattern | Setup | Best For |
|---|---|---|
| **Break-glass** | One privileged account, no rotation period, manual rotate only | Emergency access with full audit trail |
| **Per-service accounts** | One account per app, periodic rotation, apps read creds at startup | Service-to-host credential management |
| **Parent + children** | mgmt account rotates N child accounts | Cleanest privilege separation — mgmt credentials never exposed to humans |
| **Per-engineer accounts** | One account per person, rotate before/after each session | Individual accountability on shared hosts |
