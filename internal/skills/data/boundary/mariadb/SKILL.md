---
name: mariadb
description: Deploy or remove the Boundary MariaDB target lab in hal. Use when the user asks for database target access through Boundary.
---

# Boundary Mariadb Workflow

## Intent

Handle hal boundary mariadb requests with a stable lifecycle pattern, including optional Vault dynamic credential brokering.

## Primary Command

- hal boundary mariadb

## Common Variants

- Deploy standalone DB target: hal boundary mariadb enable --mariadb-version 11.4
- Deploy and re-bootstrap from scratch: hal boundary mariadb update
- Attach Boundary target to Vault dynamic DB creds: hal boundary mariadb enable --mariadb-version 11.4 --with-vault
- Disable lab resources: hal boundary mariadb disable

## Post-Deploy Access Guidance

- Always authenticate first:
	- BOUNDARY_AUTHENTICATE_PASSWORD_PASSWORD=password boundary authenticate password -addr http://127.0.0.1:9200 -auth-method-id <auth-method-id> -login-name dba-user
- For this CLI version, prefer boundary connect (not boundary connect tcp).
- If using Vault brokering, prefer:
	- boundary connect mysql -addr http://127.0.0.1:9200 -target-id <target-id>
- If user wants a raw TCP tunnel:
	- boundary connect -addr http://127.0.0.1:9200 -target-id <target-id> -listen-port 13306
	- mariadb -h 127.0.0.1 -P 13306 -u admin -ppassword targetdb

## Validation

- Confirm command output and summarize the resulting lab state.
- If applicable, suggest the next expected command in the lifecycle.
- For with-vault flows, verify target includes brokered credential source IDs.

## Edge Cases

- If prerequisites are missing, explain exactly what to install or start first.
- If resources are partially deployed, suggest update or cleanup path when supported.
- If user gets authorize-session or malformed identifier errors, verify command shape and current target ID.
- If Vault creds do not appear, verify the target has brokered credential source attachment.
- Prefer one-shot env assignment for password auth so no shell cleanup (unset) is needed.
