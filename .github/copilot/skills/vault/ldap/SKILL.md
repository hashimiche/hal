---
name: ldap
description: Deploy and configure the Vault LDAP auth and secrets lab in hal. Use when the user asks for LDAP login, directory-backed auth, dynamic LDAP secrets, or LDAP lab reset.
---

# Vault LDAP Workflow

## Intent

Handle hal vault ldap requests with a stable lifecycle pattern.

## Primary Command

- hal vault ldap

## Validation

- Confirm LDAP containers and Vault mounts are configured.
- Summarize test users, policies, and next test commands.

## Edge Cases

- If Vault is offline, instruct the user to deploy Vault first.
- If LDAP teardown is blocked by active leases, suggest force/reset path.
