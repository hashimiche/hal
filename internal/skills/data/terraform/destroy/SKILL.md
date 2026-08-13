---
name: destroy
description: Destroy Terraform lab resources in hal. Use for teardown and clean reset.
---

# Terraform Destroy Workflow

## Intent

Handle hal terraform delete requests with a stable lifecycle pattern.

## Primary Command

- hal terraform delete

## Validation

- Confirm command output and summarize the resulting lab state.
- If applicable, suggest the next expected command in the lifecycle.

## Edge Cases

- If prerequisites are missing, explain exactly what to install or start first.
- If resources are partially deployed, suggest update or cleanup path when supported.
- Deleting TFE must drop `terraform-vcs-workflow-*` / `tfe-saml` shared-service consumers for the destroyed target. Otherwise `hal vault jwt disable` will refuse to remove GitLab because it still thinks VCS owns it. Vault JWT / the other TFE target keep GitLab if they are still registered or running.
