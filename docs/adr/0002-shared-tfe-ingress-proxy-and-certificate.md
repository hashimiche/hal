# 2. One ingress proxy and one certificate for all TFE targets

- **Status:** Accepted
- **Date:** 2026-09-22
- **Branch for implementation:** `feature/shared-tfe-proxy-and-cert`
- **Builds on:** ADR 0001 (`bugfix/replace-minio`) — branched from it, because
  `hal terraform create` cannot boot on `main` at all (the pinned MinIO image was
  deleted from Docker Hub), so nothing here would be testable from `main`.
- **Supersedes:** the `bugfix/tfe-agent-ca-bundle` branch. That branch fixed the
  agent-image symptom by teaching each target to install *both* certs; a single
  shared cert removes the cause instead. **Drop that branch — do not merge it.**

---

## Context

The TFE lab has a primary instance and an optional twin. Today each target ships
its own duplicate of the same two pieces of infrastructure:

| | Primary | Twin |
|---|---|---|
| Ingress proxy | `hal-tfe-proxy` @ `.250` | `hal-tfe-bis-proxy` @ `.249` |
| Proxy config | `~/.hal/tfe-proxy.conf` | `~/.hal/hal-tfe-bis-proxy.conf` |
| Certificate | `~/.hal/tfe-certs` | `~/.hal/hal-tfe-bis-certs` |
| Cert generator | `ensureCerts` | `ensureCertsForTwin` |
| Rotation check | `shouldRotatePrimaryTFECert` | `shouldRotateTwinTFECert` |

PostgreSQL, Redis and object storage are already shared and already have the
machinery for it — `ensureSharedTFEEcosystemRunning()` gates the twin on them, and
`hal terraform delete` has a `preserveSharedBackend` path that spares them while a
twin is live. The proxy and the certificate are the two stragglers, and both
duplications have now caused real, separately-diagnosed outages:

1. **Two certs broke the run agents.** Each TFE container builds the image
   `hashicorp/tfe-agent:now` at boot ("Building tfe-agent image" in the
   `task-worker` log component) and bakes its own CA store into it. Both targets
   share that one image tag, so whichever booted last won, and the other's
   internal run agents then died on
   `x509: certificate signed by unknown authority` registering against their own
   hostname. Runs sat in `plan_queued` forever with nothing in the UI explaining
   why and the real error buried under ~17k sidekiq metric lines. Creating a twin
   and deleting it again left the primary permanently broken.

2. **Two proxies meant two static IPs, and nginx caches its upstream.**
   `proxy_pass https://hal-tfe:8443` with no `resolver` directive resolves once at
   worker start and caches the address for the process lifetime. Restarting
   `hal-tfe` gives it a fresh hal-net IP, after which the proxy 502s forever
   (`connect() failed (113: Host is unreachable)`) while TFE itself is perfectly
   healthy.

Neither is a deep problem. Both are the cost of duplicating per-target
infrastructure that was never per-target in the first place.

---

## Decision

Treat the ingress proxy and the TLS certificate as **shared services, exactly like
PostgreSQL / Redis / object storage.** One proxy, one certificate, one vhost per
target.

| # | Decision |
|---|---|
| 1 | **One certificate** covering every local TFE hostname, in `~/.hal/tfe-certs` |
| 2 | **One proxy** (`hal-tfe-proxy`), one static IP (`.250`); `hal-tfe-bis-proxy` and `.249` are removed |
| 3 | **One vhost file per target** in a bind-mounted directory, not one monolithic config |
| 4 | **`nginx -s reload`** to add/remove a vhost; recreate the container only when the published-port set must change |
| 5 | **`resolver` + variable upstream**, so nginx re-resolves and never hard-fails on an absent target |
| 6 | **Only emit vhosts for targets that exist** |
| 7 | **Lifecycle is consumer-aware**, reusing the existing `preserveSharedBackend` pattern |
| 8 | **Three redundant twin flags are removed**, with no aliases; host ports, hostnames and URLs are unchanged |

### 1. One certificate

A single self-signed cert in `~/.hal/tfe-certs`, subject
`O = HAL TFE Local Dev Environment`, SANs covering **both** targets:

```
DNS: localhost, hal-tfe, tfe.localhost, hal-tfe-bis, tfe-bis.localhost
IP:  127.0.0.1
```

`ensureCertsForTwin` and `shouldRotateTwinTFECert` are deleted.
`ensureCerts`/`shouldRotatePrimaryTFECert` become
`ensureSharedTFECert(certDir, dnsNames)` / `shouldRotateSharedTFECert(certPath,
dnsNames)`, where rotation triggers when **any** required SAN is absent. That
single rule also performs the upgrade: a cert left over from either old generator
is missing at least one of the new SANs, so it is regenerated automatically.

This is what actually retires problem (1). With one cert, whichever target builds
`hashicorp/tfe-agent:now` bakes a cert valid for both hostnames, so the shared tag
is correct no matter who wrote it last. No trust-anchor juggling is needed — the
existing single-cert trust refresh (`cp /etc/ssl/tfe/cert.pem … &&
update-ca-certificates`) is already right once the cert is shared.

The twin's `supervisorctl restart tfe:archivist` is dropped from that command
chain: this image has no supervisord (see `LLM_CONTEXT.md`), so the step always
failed, which is why `create --target twin` always printed an empty
"Could not refresh twin TFE trust store automatically:" warning — the cert had in
fact been installed and only the dead final step failed, with output sent to
`/dev/null`.

**Accepted limitation.** The cert is minted during primary create, before
`--twin-hostname` / `--twin-container-name` are known, so it carries the *default*
twin names. Overriding them makes `ensureSharedTFECert` rotate the cert at twin
create; the proxy picks that up on reload, but a TFE container that is already
running keeps the old cert baked into `tfe-agent:now` until it restarts. Overriding
those flags against a live primary therefore needs a
`hal terraform create --update`. Documented rather than engineered around: the
defaults are what the lab uses.

### 2–4. One proxy, vhost per target, reload vs recreate

```
~/.hal/tfe-proxy/
  nginx.conf              # base: events{}, http{ resolver; include vhosts/*.conf; }
  vhosts/primary.conf     # server_name tfe.localhost      -> hal-tfe:8443, :8444
  vhosts/twin.conf        # server_name tfe-bis.localhost   -> hal-tfe-bis:8443
```

The whole directory is bind-mounted, so adding or removing a vhost file is visible
to the running container immediately — no recreation for a config change.

Host ports and hostnames are **unchanged**: `8443` (primary UI), `8444` (primary
admin), `9443` (twin UI). `--twin-https-port` keeps working. Both
`tfe.localhost` and `tfe-bis.localhost` become network aliases on the one proxy.

A single helper owns the whole decision:

```go
ensureTFEProxy(engine, image, certDir, vhosts []tfeProxyVhost) error
```

1. write `nginx.conf` + one file per vhost, removing stale vhost files
2. proxy not running → create it, publishing exactly the ports the vhosts need
3. running **and** already publishing every needed port → `nginx -t` then
   `nginx -s reload`
4. running but the published-port set is insufficient → recreate with the union

Step 3's port check is a **superset** test, not equality: removing a target leaves
its host port published until some later change forces a recreate. Harmless — nginx
stops listening on it, so the port answers nothing — and far better than disrupting
the surviving target's ingress to unpublish an idle port.

Step 4 is not an optimisation choice, it is a hard engine constraint: **a published
port cannot be added to a running container.** Verified on the live proxy —
`podman port hal-tfe-proxy` shows `8443` and `8444` only, so a twin's `9443` cannot
appear without recreation. Recreating nginx is stateless and sub-second, so the
fallback is cheap; the reload fast path is kept because it preserves in-flight
connections. `nginx -t` runs first so a bad config is rejected before it can take
the proxy down.

### 5. `resolver` + variable upstream

```nginx
resolver <engine-dns> valid=10s ipv6=off;

location / {
    set $tfe_upstream hal-tfe:8443;
    proxy_pass https://$tfe_upstream;
}
```

The variable is named `$tfe_upstream`, not `$upstream…`: nginx owns the `$upstream_`
namespace for its own built-ins (`$upstream_addr`, `$upstream_http_*`, …), so a
custom `set` there risks colliding with a reserved name.

This buys two distinct things:

- **It fixes problem (2).** A variable upstream is resolved per request against
  `resolver`, with a 10s TTL, so a core container that comes back on a new IP is
  picked up without touching the proxy.
- **It makes decision 6 safe.** With a literal `proxy_pass` hostname, nginx
  refuses to start if that name does not resolve — so one leftover vhost for a
  removed target would brick the *entire* proxy, including the surviving
  instance. With a variable, an absent upstream degrades to a per-request `502`
  confined to that vhost.

The resolver address is engine-specific and must be resolved at config-write time:

| Engine | Resolver |
|---|---|
| Docker | `127.0.0.11` (embedded DNS) |
| Podman | the hal-net gateway, i.e. `global.HalNetStaticIP(engine, 1)` (aardvark-dns) |

Verified on this host: a container on `hal-net` under podman gets
`nameserver 10.89.0.1`, matching the network's gateway. Hard-coding `127.0.0.11`
would silently break every podman user, which is the default engine here.

### 6. Only emit vhosts for targets that exist

`create` emits the primary vhost; `create --target twin` adds the twin vhost;
each `delete` removes its own and re-runs `ensureTFEProxy`. Nothing writes a vhost
for a target that is not deployed, so `9443` is never published without a twin
behind it.

### 7. Consumer-aware lifecycle

- `tfeProxyContainer` moves out of `tfePrimaryContainers` into
  `tfeSharedBackendContainers`, so the existing `preserveSharedBackend` check
  already spares it while a twin runs.
- `ensureSharedTFEEcosystemRunning()` gains the proxy, so twin create reports a
  missing proxy the same way it reports a missing database.
- `delete` (primary) with a live twin: drop the primary vhost, reload, keep the
  proxy **and keep `~/.hal/tfe-certs`** — the twin is still serving from that cert.
  Only a teardown that takes the shared backend with it wipes the cert dir.
- `delete --target twin`: drop the twin vhost, reload, leave everything else.

---

## Consequences

**No URL change.** Same host ports, same hostnames, same
`--twin-https-port`. Existing bookmarks and docs keep working.

**Three twin flags are removed** — a small, deliberate CLI break:

| Removed | Why | Replacement |
|---|---|---|
| `--twin-proxy-image` | named a container that no longer exists | `--tfe-proxy-image` |
| `--twin-proxy-tag` | same | `--tfe-proxy-tag` |
| `--twin-proxy-ip` | actively harmful — it set the twin's `--add-host` to an IP where nothing listens now that the proxy is shared | none needed |

`--tfe-proxy-image` / `--tfe-proxy-tag` are already registered on the same
`create`/`update` commands the twin lifecycle runs through, so the version-override
contract in `LLM_CONTEXT.md` §6 is still satisfied by exactly one pair of flags for
the one container. Keeping the twin duplicates would have let the two disagree over
a single container, with last-writer-wins semantics.

Because those flag vars are only bound on `create`/`update` while `delete` and
`status` also reconcile the proxy, the image reference is resolved through
`tfeProxyImageRef()`, which falls back to the defaults per component rather than
producing `":"`.

**Two classes of bug become structurally impossible,** rather than patched: there
is no second cert to disagree with the first, and no second proxy to hold a stale
address.

**Fewer moving parts:** one container instead of two, one static IP instead of
two, one cert generator instead of two, one rotation rule instead of two.

**The proxy is now a shared failure domain.** A malformed vhost could affect both
targets. Mitigated by `nginx -t` before every reload, and by the variable upstream
confining an unreachable target to its own vhost.

**Orphans from the old layout.** `hal-tfe-bis-proxy`,
`~/.hal/hal-tfe-bis-certs/` and `~/.hal/hal-tfe-bis-proxy.conf` are left behind on
machines that ran a twin. `delete` removes these legacy paths explicitly, and
`hal delete`'s `hal-` prefix sweep already catches the container.

**Cert rotation against a live primary** is the one rough edge — see the accepted
limitation under decision 1.

---

## Implementation plan

Ordered; `<engine>` is whatever HAL resolves.

### Step 1 — `defaults.go`

| Change |
|---|
| `tfePrimaryProxyHostNum = 250` → rename `tfeProxyHostNum` (one proxy now) |
| **delete** `tfeTwinProxyHostNum = 249` |
| `tfeProxyConfName = "tfe-proxy.conf"` → `tfeProxyDirName = "tfe-proxy"` |
| add `tfeProxyVhostsDirName = "vhosts"` |
| add `defaultTFETwinContainer = "hal-tfe-bis"` so the cert SAN list and the `--twin-container-name` flag default share one constant |

### Step 2 — shared cert (`create.go`)

- `ensureCerts(certDir)` → `ensureSharedTFECert(certDir, dnsNames []string)`;
  subject `O = HAL TFE Local Dev Environment`, CN `tfe.localhost`.
- `shouldRotatePrimaryTFECert` → `shouldRotateSharedTFECert(certPath, dnsNames)`:
  rotate when any required SAN is missing (keep the existing legacy-issuer check).
- add `sharedTFECertDNSNames(extra ...string)` returning the deduped union of
  `localhost`, `tfeCoreContainer`, `tfePrimaryHostname`,
  `defaultTFETwinContainer`, `defaultTFETwinHostname`, plus any override.

### Step 3 — proxy helper (new `proxy.go`)

```go
type tfeProxyVhost struct {
    Name       string // "primary" | "twin" — the vhost filename
    ServerName string // tfe.localhost
    Upstream   string // hal-tfe
    HTTPSPort  int    // 8443
    AdminPort  int    // 8444, 0 when absent
}

func tfeProxyPaths() (dir, confPath, vhostsDir string, err error)
func tfeProxyResolver(engine string) string
func renderTFEProxyBaseConf(engine string) string
func renderTFEProxyVhost(v tfeProxyVhost) string
func desiredTFEProxyVhosts(engine string) ([]tfeProxyVhost, error) // from what is deployed
func ensureTFEProxy(engine, image, certDir string, vhosts []tfeProxyVhost) error
func removeTFEProxyVhost(engine, image, certDir, name string) error
```

`renderTFEProxyVhost` keeps every behaviour of both current configs verbatim —
`proxy_ssl_verify off`, the `Host`/`X-Forwarded-*` headers,
`proxy_set_header Accept-Encoding ""`, the `_archivist/` `sub_filter`, and both
`proxy_redirect` rules — only parameterised per target. The primary's `8444` admin
server block is emitted only when `AdminPort != 0`.

Also drop the stray duplicate `text/html` in `sub_filter_types` that makes nginx
log `duplicate MIME type "text/html"` on every start.

### Step 4 — `create.go` wiring

- Write vhosts and call `ensureTFEProxy` instead of the inline `run -d`.
- Mount `-v <dir>:/etc/nginx/hal:ro` and use `/etc/nginx/hal/nginx.conf`; keep
  `-v <certDir>:/etc/ssl/tfe:ro`.
- Restore the single-cert trust refresh
  (`cp /etc/ssl/tfe/cert.pem … tfe-localhost.crt && update-ca-certificates`).
- Network aliases: `tfe.localhost` always; `tfe-bis.localhost` too, so a later twin
  needs no alias change (aliases, unlike ports, are free).

### Step 5 — `twin.go`

- Delete `ensureCertsForTwin` and `shouldRotateTwinTFECert`.
- `tfeTwinLayout`: drop `ProxyContainer` and `ProxyConfPath`; point `CertDir` at
  the shared `~/.hal/tfe-certs`.
- Call `ensureSharedTFECert(sharedCertDir, sharedTFECertDNSNames(layout.CoreContainer, tfeTwinHostname))`.
- Replace the inline twin-proxy `run -d` with `ensureTFEProxy` over both vhosts.
- `ensureSharedTFEEcosystemRunning` += `tfeProxyContainer`.
- `destroyTFETwin`: `removeTFEProxyVhost(..., "twin")`; stop deleting `CertDir`
  (now shared) and `ProxyConfPath` (gone).
- Status table: report the shared proxy, not a twin-specific one.

### Step 6 — `delete.go`

- Move `tfeProxyContainer` from `tfePrimaryContainers` to
  `tfeSharedBackendContainers`.
- When `preserveSharedBackend`, call `removeTFEProxyVhost(..., "primary")` and skip
  the cert-dir wipe.
- Remove legacy `~/.hal/hal-tfe-bis-certs/`, `~/.hal/hal-tfe-bis-proxy.conf` and
  `~/.hal/tfe-proxy.conf`, plus a legacy `<engine> rm -f hal-tfe-bis-proxy`.

### Step 7 — remaining references

| File | Change |
|---|---|
| `cmd/terraform/saml.go` ~83 | `tfeSAMLProxyContainerForTarget` returns `tfeProxyContainer` for both targets |
| `cmd/terraform/agent.go` ~270-274 | both add-host entries use `tfeProxyHostNum` |
| `cmd/terraform/status.go` ~130 | twin row reports the shared proxy |
| `cmd/capacity.go` ~106 | twin stack loses `hal-tfe-bis-proxy` |
| `cmd/teardown.go` | note `hal-tfe-bis-proxy` is covered by the `hal-` sweep |

### Step 8 — docs

`LLM_CONTEXT.md` (shared-proxy + shared-cert architecture, and the
`resolver`/variable-upstream requirement), `README.md`, and
`internal/skills/data/terraform/twin/SKILL.md` (twin reuses the proxy and cert
too, not just PG/Redis/S3).

---

## Verification

### Static

```bash
go build ./... && go vet ./... && go test ./...
gofmt -l ./cmd ./internal
grep -rn "hal-tfe-bis-proxy\|ensureCertsForTwin\|tfeTwinProxyHostNum" --include="*.go" .
# only permitted hits: the legacy-cleanup lines in delete.go
```

### Runtime

```bash
# 1. primary alone
go run . terraform create
<engine> port hal-tfe-proxy                      # expect 8443, 8444 — no 9443
openssl x509 -in ~/.hal/tfe-certs/cert.pem -noout -ext subjectAltName
# expect all five DNS names + 127.0.0.1
ls ~/.hal/tfe-proxy/vhosts/                      # expect primary.conf only

# 2. twin joins — proxy recreated for 9443, both vhosts served
go run . terraform create --target twin
ls ~/.hal/tfe-proxy/vhosts/                      # primary.conf AND twin.conf
<engine> port hal-tfe-proxy                      # now includes 9443
test ! -d ~/.hal/hal-tfe-bis-certs               # no second cert dir
<engine> ps --format '{{.Names}}' | grep -c bis-proxy   # expect 0
curl -sk -o /dev/null -w "%{http_code}\n" https://tfe.localhost:8443/api/v2/ping
curl -sk -o /dev/null -w "%{http_code}\n" https://tfe-bis.localhost:9443/api/v2/ping

# 3. the bug ADR 0001's twin test exposed: primary runs must survive a twin boot
#    trigger a run on the primary -> must reach planned/applied, and
#    `<engine> logs hal-tfe | grep x509` must be empty

# 4. one cert, trusted for both hostnames, from inside the agent image itself
<engine> run --rm --network hal-net --entrypoint sh hashicorp/tfe-agent:now -lc '
  for t in tfe.localhost tfe-bis.localhost; do
    echo | openssl s_client -connect <proxyIP>:443 -servername $t \
      -CAfile /etc/ssl/certs/ca-certificates.crt -verify_return_error 2>&1 \
      | grep -m1 "Verify return code"; done'
# expect: Verify return code: 0 (ok)   twice

# 5. the 502 regression — restart the core, proxy must recover on its own
<engine> restart hal-tfe && sleep 20
curl -sk -o /dev/null -w "%{http_code}\n" https://tfe.localhost:8443/api/v2/ping
# expect 2xx/204 with NO proxy restart, proving resolver + variable upstream works

# 6. twin removal reloads without disturbing the primary
go run . terraform delete --target twin
ls ~/.hal/tfe-proxy/vhosts/                      # primary.conf only
curl -sk -o /dev/null -w "%{http_code}\n" https://tfe.localhost:8443/api/v2/ping

# 7. primary removal while the twin lives keeps the shared proxy and cert
go run . terraform create --target twin && go run . terraform delete
<engine> ps --format '{{.Names}}' | grep hal-tfe-proxy   # still there
test -f ~/.hal/tfe-certs/cert.pem                        # still there
curl -sk -o /dev/null -w "%{http_code}\n" https://tfe-bis.localhost:9443/api/v2/ping
```

Step 5 is the regression test for the cached-upstream 502 and must not be skipped —
it is the only check that proves the `resolver` directive is actually in effect.

---

## Alternatives considered and rejected

- **Keep two proxies, just add `resolver` to each.** Fixes the 502 and nothing
  else. Leaves the duplicated cert, so the agent-image bug survives and still needs
  the `bugfix/tfe-agent-ca-bundle` patch. Rejected as treating symptoms.
- **Keep two certs, teach both targets to trust both** (the
  `bugfix/tfe-agent-ca-bundle` branch — implemented and verified working).
  Rejected: it is a bandaid over a duplication that should not exist, and every
  future per-target hostname has to remember to join the anchor set.
- **One cert per target but signed by a shared local CA.** Correct, and closer to
  real-world PKI. Rejected as disproportionate for a demo lab: it adds a CA to
  generate, store, rotate and distribute, to replace one self-signed cert that
  already works.
- **Serve both targets on host port 8443, split by SNI** (`tfe.localhost:8443` and
  `tfe-bis.localhost:8443`). Genuinely elegant — one published port, no recreate
  path needed at all. Rejected because it changes the twin's URL, makes
  `--twin-https-port` meaningless, and breaks every existing bookmark and doc for
  no functional gain.
- **Always publish `9443`, even with no twin,** to make twin create a pure reload.
  Rejected: it binds a host port nobody is using, and still needs the recreate path
  the moment someone passes a non-default `--twin-https-port`.
- **`nginx -s reload` for everything.** Impossible: a published port cannot be
  added to a running container.
- **Recreate the proxy for everything, never reload.** Simpler (one code path) and
  nearly free, since nginx is stateless. Rejected only because reload preserves
  in-flight connections and the branch costs three lines — but this is the
  fallback if the reload path proves flaky.

---

## Verification result

Executed 2026-09-22 on macOS arm64 / rootless podman against TFE 2.0.5. **Every
check above passed.**

| Check | Result |
|---|---|
| `go build`, `go vet`, `go test`, `gofmt -l` | clean |
| Completeness grep | only the intended legacy-cleanup lines in `delete.go` |
| Primary alone: published ports | `8443`, `8444` — no `9443` |
| Primary alone: vhosts | `primary.conf` only |
| Shared cert | `O=HAL TFE Local Dev Environment, CN=tfe.localhost`, SANs `localhost, hal-tfe, tfe.localhost, hal-tfe-bis, tfe-bis.localhost` + `127.0.0.1` |
| Second cert dir / second proxy | absent (`hal-tfe-bis-certs` gone, 0 `bis-proxy` containers) |
| Generated config | `resolver 10.89.0.1 valid=10s ipv6=off` (podman gateway), `set $tfe_upstream` |
| **502 regression (step 5)** | `hal-tfe` restarted, IP moved `10.89.0.43` → `.44`; proxy recovered to `204` in ~20s (the `valid=10s` TTL) **with no proxy restart**. Previously 502'd forever. |
| Twin joins: ports | recreated to publish `8443`, `8444`, `9443` |
| Twin joins: vhosts | `primary.conf` **and** `twin.conf` |
| All three endpoints via one proxy | `tfe.localhost:8443` 204, `tfe-bis.localhost:9443` 204, `tfe.localhost:8444` 200 |
| **Shared cert trusted for both hostnames** | from inside `hashicorp/tfe-agent:now`: `tfe.localhost` and `tfe-bis.localhost` both `Verify return code: 0 (ok)` |
| `tfe-agent:now` contents | exactly **one** cert, the shared one — not a per-target cert |
| **ADR 0001 regression (step 3)** | primary run after twin boot → `planned_and_finished`; `x509` count in `hal-tfe` logs: **0** |
| Primary delete under live twin (step 7) | proxy **and** cert preserved, only `primary.conf` retracted, twin still `204` |
| Primary recreate under live twin | twin's vhost preserved, both endpoints `204` |
| Twin delete (step 6) | `twin.conf` removed, shared cert kept, primary `204` / admin `200` |

Two findings the runtime test surfaced, both now fixed in this branch:

1. The twin's trust refresh still carried the dead
   `supervisorctl restart tfe:archivist`, so `create --target twin` kept printing the
   empty "Could not refresh twin TFE trust store automatically:" warning. Both
   targets now share one `refreshTFETrustStoreCmd`, and the warning is gone.
2. `hal terraform status` listed the shared proxy for the twin but not for the
   primary. It is a shared service that can fail on its own, so it is now reported
   for both.
