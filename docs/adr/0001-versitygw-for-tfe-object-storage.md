# 1. Replace MinIO with VersityGW for TFE object storage

- **Status:** Accepted
- **Date:** 2026-09-21
- **Branch for implementation:** `bugfix/replace-minio`
- **Supersedes:** nothing
- **Working notes:** `docs/object-storage-replacement-spec.md` (full measurement
  data and the decision interview; delete or reduce it to nothing once this ADR
  is merged)

---

## Context

The TFE lab stack (`hal terraform create`) needs an S3-compatible object store for
`TFE_OBJECT_STORAGE_TYPE=s3`. It has always used MinIO. MinIO Community Edition is
now dead:

| Date | Event |
|---|---|
| 2025-05-24 | Embedded console gutted; LDAP/OIDC logins moved to commercial AIStor |
| 2025-09-07 | Last release with published binaries and container images |
| 2025-10-15 | Final release, source-only, no image published |
| 2025-12-03 | README switched to "maintenance mode" |
| 2026-04-25 | `minio/minio` GitHub repository archived — "NO LONGER MAINTAINED" |

The **`minio/minio` Docker Hub repository has been deleted** (404; `pull` returns
`requested access to the resource is denied`). `minio/mc`, `minio/operator` and
`minio/console` are archived or deleted. `dl.min.io` returns HTTP 410 Gone.

`cmd/terraform/defaults.go` pins `defaultTFEMinioImage = "minio/minio"` with
`defaultTFEMinioTag = "latest"`. **`hal terraform create` therefore fails on any
fresh clone.** It only works on machines that still hold the image in local cache.

MinIO has exactly one consumer in this repo: the TFE stack. Its vendor name is
baked into a container name, a volume name, two credential constants, and six CLI
flags.

---

## Decision

**Replace MinIO with VersityGW** (`ghcr.io/versity/versitygw:v1.8.0`), an
Apache-2.0 S3 gateway over a POSIX filesystem, and **rename the concept to the
vendor-neutral `s3`** throughout.

Full list of decisions, each settled deliberately:

| # | Decision |
|---|---|
| 1 | **Backend: VersityGW**, `ghcr.io/versity/versitygw:v1.8.0` |
| 2 | **Vocabulary: `s3`**, not `objectstore` and not a vendor name |
| 3 | **Clean CLI break** — no deprecated aliases for the old flag names |
| 4 | **No data migration** — HAL is stateless between an up and a down |
| 5 | **The console is dropped entirely** — no WebUI, no 19001 port, no flag |
| 6 | **Keep the S3 API host port** (19000) for troubleshooting |
| 7 | **Credentials reuse the shared lab password** `hal9000FTW` |
| 8 | **Pin a readable semver tag**, never `latest`; prefer GHCR over Docker Hub |
| 9 | **Drop the 3-second sleep**; keep `exec mkdir` for bucket creation |
| 10 | **Add `--health /livez`** as a real readiness endpoint |
| 11 | **Keep request logging verbose** (default) |
| 12 | **Object storage stays TFE-local**, not a registered shared service |

### Why VersityGW

Four candidates were measured by actually running them on arm64/podman:

| | quay pin | Silo | SeaweedFS | **VersityGW** |
|---|---|---|---|---|
| License | AGPLv3 (dead) | AGPL-3.0 | Apache-2.0 | **Apache-2.0** |
| Upstream | archived | fork, 1 maintainer | active since 2012 | **active, ~monthly** |
| Image size | 168 MB | 153 MB | 506 MB | **63.4 MB** |
| Idle RSS | 68.7 MB | 214 MB | ~70–300 MB | **~10 MB** |
| `mkdir` → bucket | yes | yes | **no** | **yes** |
| Diff in HAL | 2 constants | 2 constants | large | **small** |

Decisive properties, all verified empirically:

- **`mkdir` creates a bucket, bijectively.** `mkdir /data/<name>` makes the
  directory appear in `ListBuckets` with `HeadBucket` → 200, no `CreateBucket` and
  no restart. HAL's existing provisioning approach survives.
- **All 11 realistic TFE archivist key patterns pass**, including a 20 MB
  multipart upload (ETag `-4`, sha256 identical both ways) and presigned GET
  (200; tampered signature → 403).
- **Two-port shape and env-var credentials map 1:1** onto the current MinIO
  invocation.
- The POSIX limitation (`a` and `a/b` cannot coexist → 409 `ObjectParentIsFile`)
  is real but **structurally unreachable**: archivist keys are
  `archivist/v1/<type>/<ULID>/<name>` — fixed depth, leaf-terminal, so a segment
  is never both a container and a leaf.

### Why `s3` as the neutral term

The repo already uses it for exactly this concept: `tfeS3Bucket` and `tfeS3Region`
in `defaults.go`, and the `--twin-s3-bucket` flag. It matches the brevity of
`hal-tfe-db` (not `hal-tfe-postgres`), and "S3" names the **protocol** TFE speaks,
not a vendor — so it cannot become a lie at the next replacement. Choosing
`objectstore` would leave `--twin-objectstore-secret-key` sitting next to
`--twin-s3-bucket`.

### Precedent

Retiring Keycloak for Authentik was a hard swap: containers renamed
(`hal-keycloak` → `hal-authentik-server`), no compatibility aliases, MCP allowlist
updated. This change follows that precedent.

---

## Consequences

**Breaking for users.** Six flags are renamed and one is removed. Existing stacks
must be torn down and recreated; the old `hal-tfe-minio` container and
`hal-tfe-minio-data` volume become orphans. This is acceptable because HAL is
stateless between an up and a down, and a demo lab's state/plan has no value.

**No object browser.** The MinIO console — whose object browser still functioned
even after the 2025 gutting — is gone. Mitigation: the S3 API stays published on
host port 19000, so `aws --endpoint-url http://127.0.0.1:19000` works, and request
logging stays verbose so archivist traffic is visible in
`<engine> logs hal-tfe-s3`.

**Unverified against real TFE.** S3 conformance and archivist key patterns were
validated with `aws` CLI, but no TFE instance has consumed this gateway yet. The
acceptance test below is the gate.

**Gaps accepted:** no server-side encryption (`GetBucketEncryption` →
`NotImplemented`), no object-level ACLs, SigV4 only, versioning off unless
`--versioning-dir` is passed. None are used by TFE object storage.

**Filesystem requirement:** the POSIX backend stores metadata in extended
attributes. Fine on a named volume (verified), and verified to work on a macOS
bind mount over virtiofs. Relevant only if someone bind-mounts a host path.

---

## Implementation plan

Execute on branch `bugfix/replace-minio`. Steps are ordered; each is mechanical.
`<engine>` means the value HAL resolves (`docker` or `podman`).

### Step 0 — Read this first

- **Do not run `docker image prune` / `podman image prune` at any point.** The
  cached `docker.io/minio/minio:latest` image is irreplaceable: its Docker Hub
  repository has been deleted.
- **Do not add deprecated flag aliases.** The clean break is deliberate.
- **Do not use `sh -c` in the bucket-creation `exec`.** The verified invocation is
  `exec <container> mkdir -p /data/<bucket>` with no shell. Whether the VersityGW
  image ships `/bin/sh` was **not** verified — see Step 9.
- Global VersityGW flags must precede the `posix` subcommand; `/data` is the
  subcommand's positional argument.
- Do not pass any `VGW_*` environment variable. The image entrypoint is
  `if [ "$#" -gt 0 ]; then exec "$BIN" "$@"; fi`, so positional arguments
  short-circuit all `VGW_*` handling. We go fully positional.
  `ROOT_ACCESS_KEY`/`ROOT_SECRET_KEY` are read by the Go binary itself, not the
  entrypoint, so they work alongside positional arguments.

### Step 1 — `cmd/terraform/defaults.go`

Apply this exact rename map. Old names must not remain anywhere.

| Old | New | Old value | New value |
|---|---|---|---|
| `tfeMinioContainer` | `tfeS3Container` | `"hal-tfe-minio"` | `"hal-tfe-s3"` |
| `tfeMinioVolume` | `tfeS3Volume` | `"hal-tfe-minio-data"` | `"hal-tfe-s3-data"` |
| `tfeMinioRootUser` | `tfeS3AccessKey` | `"minioadmin"` | `"haladmin"` |
| `tfeMinioRootPass` | `tfeS3SecretKey` | `"minioadmin"` | `"hal9000FTW"` |
| `defaultTFEMinioImage` | `defaultTFES3Image` | `"minio/minio"` | `"ghcr.io/versity/versitygw"` |
| `defaultTFEMinioTag` | `defaultTFES3Tag` | `"latest"` | `"v1.8.0"` |
| `defaultMinioAPIHostPort` | `defaultTFES3APIHostPort` | `19000` | `19000` |
| `defaultMinioConsoleHostPort` | **delete** | `19001` | — |

`tfeS3Bucket` (`"tfe-data"`) and `tfeS3Region` (`"us-east-1"`) already exist and
are **unchanged**.

The credential values deliberately mirror the existing TFE admin pair
(`defaultTFEAdminUsername = "haladmin"`, `defaultTFEAdminPassword = "hal9000FTW"`)
so the lab has one password to remember. Do not invent new secrets.

Add a comment above the credential constants noting that `hal9000FTW` is the
shared lab password, also used for TFE admin and GitLab root.

### Step 2 — `cmd/terraform/create.go`, package-level vars (around line 34)

```go
// before
	minioVersion        string
	minioImage          string
	minioAPIPort        int
	minioConsolePort    int

// after
	s3Version           string
	s3Image             string
	s3APIPort           int
```

`minioConsolePort` is deleted, not renamed.

### Step 3 — `cmd/terraform/create.go`, update teardown list (around line 132)

Replace `tfeMinioContainer` with `tfeS3Container` in the `rm -f` argument list.

### Step 4 — `cmd/terraform/create.go`, provisioning (around lines 172-181)

```go
// before
		// 7. Deploy MinIO (S3 Mock)
		step(1, "Provisioning object storage (MinIO)")
		_ = exec.Command(engine, "run", "-d", "--name", tfeMinioContainer, "--network", global.HalNetName,
			"-p", fmt.Sprintf("%d:9000", minioAPIPort), "-p", fmt.Sprintf("%d:9001", minioConsolePort),
			"-v", tfeMinioVolume+":/data",
			"-e", "MINIO_ROOT_USER="+tfeMinioRootUser, "-e", "MINIO_ROOT_PASSWORD="+tfeMinioRootPass,
			fmt.Sprintf("%s:%s", minioImage, minioVersion), "server", "/data", "--console-address", ":9001").Run()

		time.Sleep(3 * time.Second)
		_ = exec.Command(engine, "exec", tfeMinioContainer, "sh", "-c", "mkdir -p /data/"+tfeS3Bucket).Run()

// after
		// 7. Deploy the S3 object storage gateway
		step(1, "Provisioning object storage (S3)")
		_ = exec.Command(engine, "run", "-d", "--name", tfeS3Container, "--network", global.HalNetName,
			"-p", fmt.Sprintf("%d:9000", s3APIPort),
			"-v", tfeS3Volume+":/data",
			"-e", "ROOT_ACCESS_KEY="+tfeS3AccessKey, "-e", "ROOT_SECRET_KEY="+tfeS3SecretKey,
			fmt.Sprintf("%s:%s", s3Image, s3Version),
			"--port", ":9000", "--health", "/livez", "posix", "/data").Run()

		// A bucket is just a top-level directory on the POSIX backend, and the
		// gateway stats it per request — no restart and no readiness wait needed.
		// The gateway serves ~0.06s after `run -d` returns; the old 3s sleep was
		// ~50x larger than necessary.
		_ = exec.Command(engine, "exec", tfeS3Container, "mkdir", "-p", "/data/"+tfeS3Bucket).Run()
```

Note: only **one** `-p` mapping now, the console mapping is gone. If `time` becomes
an unused import in this file, remove it — check, it is likely still used elsewhere.

### Step 5 — `cmd/terraform/create.go`, TFE environment (around lines 251-258)

Only the endpoint host and the two credential constants change:

```go
			"-e", fmt.Sprintf("TFE_OBJECT_STORAGE_S3_ENDPOINT=http://%s:9000", tfeS3Container),
			"-e", "TFE_OBJECT_STORAGE_S3_BUCKET="+tfeS3Bucket,
			"-e", "TFE_OBJECT_STORAGE_S3_REGION="+tfeS3Region,
			"-e", "TFE_OBJECT_STORAGE_S3_ACCESS_KEY_ID="+tfeS3AccessKey,
			"-e", "TFE_OBJECT_STORAGE_S3_SECRET_ACCESS_KEY="+tfeS3SecretKey,
			"-e", "TFE_OBJECT_STORAGE_S3_FORCE_PATH_STYLE=true",
```

`TFE_OBJECT_STORAGE_S3_USE_INSTANCE_PROFILE=false` and `FORCE_PATH_STYLE=true` are
unchanged. `TFE_OBJECT_STORAGE_S3_REGION` is **ignored** by VersityGW (its default
is already `us-east-1`); keep it set so TFE's own validation is satisfied.

### Step 6 — `cmd/terraform/create.go`, endpoint output (around lines 389-390)

```go
// before
		ui.Field("MinIO API", fmt.Sprintf("http://127.0.0.1:%d", minioAPIPort))
		ui.Field("MinIO UI", fmt.Sprintf("http://127.0.0.1:%d", minioConsolePort))

// after
		ui.Field("S3 API", fmt.Sprintf("http://127.0.0.1:%d", s3APIPort))
```

### Step 7 — `cmd/terraform/create.go`, flags (around lines 557-560)

```go
// before
	cmd.Flags().StringVar(&minioVersion, "tfe-minio-tag", defaultTFEMinioTag, "MinIO image tag for TFE object storage")
	cmd.Flags().StringVar(&minioImage, "tfe-minio-image", defaultTFEMinioImage, "MinIO image name for TFE object storage")
	cmd.Flags().IntVar(&minioAPIPort, "minio-api-port", defaultMinioAPIHostPort, "Host port mapped to MinIO S3 API container port 9000")
	cmd.Flags().IntVar(&minioConsolePort, "minio-console-port", defaultMinioConsoleHostPort, "Host port mapped to MinIO console container port 9001")

// after
	cmd.Flags().StringVar(&s3Version, "tfe-s3-tag", defaultTFES3Tag, "Object storage gateway image tag for TFE object storage")
	cmd.Flags().StringVar(&s3Image, "tfe-s3-image", defaultTFES3Image, "Object storage gateway image name for TFE object storage")
	cmd.Flags().IntVar(&s3APIPort, "s3-api-port", defaultTFES3APIHostPort, "Host port mapped to the S3 API container port 9000")
```

### Step 8 — `cmd/terraform/twin.go`

| Location | Change |
|---|---|
| ~line 46-47 | `tfeTwinMinioRootUser` → `tfeTwinS3AccessKey`, `tfeTwinMinioRootPassword` → `tfeTwinS3SecretKey` |
| ~line 158 | message `"...shared PG/Redis/MinIO..."` → `"...shared PG/Redis/S3..."` |
| ~line 182 | message `"Ensuring shared MinIO has twin bucket..."` → `"Ensuring shared object storage has twin bucket..."` |
| ~line 247 | `TFE_OBJECT_STORAGE_S3_ENDPOINT=http://hal-tfe-minio:9000` → build from `tfeS3Container`, do not hardcode the name |
| ~line 250-251 | the two credential env vars use the renamed twin vars |
| ~line 348 | `"🪣 Shared Bucket: %s (on hal-tfe-minio)"` → use `tfeS3Container` |
| ~line 424 | `{"Shared Object Storage (MinIO)", tfeMinioContainer}` → `{"Shared Object Storage (S3)", tfeS3Container}` |
| ~line 450 | message listing `hal-tfe-minio` → `hal-tfe-s3` |
| ~line 492 | message listing `hal-tfe-minio` → `hal-tfe-s3` |
| ~line 499 | `required := []string{...tfeMinioContainer}` → `tfeS3Container` |
| ~line 538 | see below |
| ~line 713-715 | flags, see below |

`ensureTwinBucketExists` (~line 538):

```go
// before
	out, err := exec.Command(engine, "exec", tfeMinioContainer, "sh", "-c", fmt.Sprintf("mkdir -p /data/%s", trimmed)).CombinedOutput()
	if err != nil {
		return fmt.Errorf("minio bucket creation failed: %s", strings.TrimSpace(string(out)))
	}

// after
	out, err := exec.Command(engine, "exec", tfeS3Container, "mkdir", "-p", "/data/"+trimmed).CombinedOutput()
	if err != nil {
		return fmt.Errorf("s3 bucket creation failed: %s", strings.TrimSpace(string(out)))
	}
```

Keep the existing input validation (rejecting empty names, `/` and spaces) exactly
as it is — it is still required.

Flags (~line 713-715):

```go
// before
	cmd.Flags().StringVar(&tfeTwinMinioRootUser, "twin-minio-root-user", "minioadmin", "MinIO root user for shared object storage")
	cmd.Flags().StringVar(&tfeTwinMinioRootPassword, "twin-minio-root-password", "minioadmin", "MinIO root password for shared object storage")
	cmd.Flags().StringVar(&tfeTwinObjectStorageBucket, "twin-s3-bucket", "tfe-bis-data", "S3 bucket name for twin TFE objects in shared MinIO")

// after
	cmd.Flags().StringVar(&tfeTwinS3AccessKey, "twin-s3-access-key", tfeS3AccessKey, "S3 access key for shared object storage")
	cmd.Flags().StringVar(&tfeTwinS3SecretKey, "twin-s3-secret-key", tfeS3SecretKey, "S3 secret key for shared object storage")
	cmd.Flags().StringVar(&tfeTwinObjectStorageBucket, "twin-s3-bucket", "tfe-bis-data", "S3 bucket name for twin TFE objects in shared object storage")
```

Note the defaults now reference the constants instead of repeating literals — this
is what `defaults.go`'s header comment requires. `--twin-s3-bucket` keeps its name.

### Step 9 — Remaining code references

| File | Line | Change |
|---|---|---|
| `cmd/terraform/delete.go` | ~29 | `tfeMinioContainer` → `tfeS3Container` |
| `cmd/terraform/delete.go` | ~37 | `tfeMinioVolume` → `tfeS3Volume` |
| `cmd/terraform/delete.go` | ~82 | message `hal-tfe-minio` → `hal-tfe-s3` |
| `cmd/terraform/status.go` | ~65 | `{"Object Storage (MinIO)", ...}` → `{"Object Storage (S3)", tfeS3Container}` |
| `cmd/terraform/status.go` | ~128 | `{"Shared Object Storage (MinIO)", ...}` → `{"Shared Object Storage (S3)", tfeS3Container}` |
| `cmd/terraform/agent.go` | ~666-667 | passthrough flag names `"twin-minio-root-user"`, `"twin-minio-root-password"` → `"twin-s3-access-key"`, `"twin-s3-secret-key"` |
| `cmd/terraform/api-workflow.go` | ~1442-1443 | same two passthrough flag names |
| `cmd/capacity.go` | ~105 | container list `"hal-tfe-minio"` → `"hal-tfe-s3"` |
| `cmd/mcp/advanced.go` | ~282 | container list `"hal-tfe-minio"` → `"hal-tfe-s3"` |
| `internal/global/status_snapshot.go` | ~77 | container list `"hal-tfe-minio"` → `"hal-tfe-s3"` |
| `internal/integrations/authentik.go` | ~31-32 | comments referencing "MinIO internal" / "MinIO console internal" — the 9001 note is now stale, rewrite or drop |

`cmd/teardown.go` (~line 25) — **keep both entries**:

```go
	"hal-tfe-s3-data",    // TFE S3 object storage (cmd/terraform/create.go)
	"hal-tfe-minio-data", // legacy: pre-VersityGW MinIO volume, kept so `hal teardown`
	                      // still reclaims it on machines that ran the old stack
```

> **Assumption flagged for review.** This is the one point not explicitly
> confirmed by the decision interview. Keeping the legacy name is three words and
> prevents a permanent ghost volume for every user who ran the old stack; it is the
> only intentional remaining "minio" string in the Go code. If the preference is a
> total purge, delete the legacy line and say so in the changelog instead. Consider
> adding `"hal-tfe-minio"` to the legacy container list for the same reason.

### Step 10 — Documentation

| File | Change |
|---|---|
| `README.md` ~297-299 | example flags `--tfe-minio-tag`, `--minio-api-port`, `--minio-console-port` → `--tfe-s3-tag`, `--s3-api-port`; drop the console flag |
| `README.md` ~307 | "reuses the primary ecosystem (PostgreSQL, Redis, MinIO)" → "(PostgreSQL, Redis, S3)" |
| `README.md` ~498-499 | replace the two rows `MinIO API` / `MinIO Console` with a single `S3 API` row pointing at `http://127.0.0.1:19000` |
| `docs/commands/terraform-deploy.md` ~30-32 | rewrite the three `--minio-*` / `--minio-version` lines as `--s3-api-port` and `--tfe-s3-tag` / `--tfe-s3-image`; delete the console line |
| `docs/commands/terraform-deploy.md` ~48-49, 53 | `--twin-minio-root-*` → `--twin-s3-access-key` / `--twin-s3-secret-key`; update the `--twin-s3-bucket` description |
| `internal/skills/data/terraform/twin/SKILL.md` ~14, ~48 | "MinIO, Redis, Postgres" → "S3, Redis, Postgres"; `hal-tfe-minio` → `hal-tfe-s3` |
| `LLM_CONTEXT.md` ~52 | the sidecar example list mentions minio — replace with another example |
| `LLM_CONTEXT.md` ~102 | "mocked PostgreSQL, Redis, and MinIO stack" → "...and S3 (VersityGW) stack" |
| `docs/scim-idp-spec.md` ~58 | the port-collision note cites 9000/9001 as MinIO — 9001 is no longer used; correct it |

Add a changelog / release-note entry stating plainly: **breaking change**, the six
flags are renamed, `--minio-console-port` is removed, and existing stacks must be
recreated (`hal terraform delete && hal terraform create`) because no data is
migrated.

### Step 11 — Delete the working notes

Delete `docs/object-storage-replacement-spec.md`, or reduce it to nothing if any
part is still wanted. Its measurement data is summarised in this ADR.

---

## Verification

Run in order. Do not skip the runtime test — the static checks cannot catch a
broken TFE handshake.

### Static

```bash
go build ./...
go vet ./...
golangci-lint run
```

Then confirm the rename is complete. The **only** permitted matches are the legacy
`teardown.go` entry, this ADR, and the changelog:

```bash
grep -rn -i "minio" --include="*.go" --include="*.md" . | grep -v docs/adr/
```

Confirm the new flags exist and the old ones are gone:

```bash
go run . terraform create --help | grep -E "s3|minio"
# expect: --tfe-s3-image, --tfe-s3-tag, --s3-api-port
# expect NO: --tfe-minio-*, --minio-api-port, --minio-console-port
```

### Runtime acceptance test

The bar, per the decision interview: **`hal terraform create` succeeding is
sufficient** — if TFE cannot reach an S3 object store, its startup fails.

```bash
# 1. Tear the old stack down first. This is a breaking change; there is no
#    in-place upgrade path.
go run . terraform delete

# 2. Confirm the image pulls from GHCR (anonymous pull, no credentials needed)
<engine> pull ghcr.io/versity/versitygw:v1.8.0

# 3. Bring the stack up
go run . terraform create

# 4. The gateway must be healthy and unauthenticated on /livez
curl -s -o - -w " <- HTTP %{http_code}\n" http://127.0.0.1:19000/livez
# expect: OK <- HTTP 200

# 5. The bucket must exist
<engine> exec hal-tfe-s3 ls -la /data
# expect a `tfe-data` directory

# 6. TFE must be up and must have talked to the gateway
go run . terraform status
<engine> logs hal-tfe-s3 | head -40
# expect request log lines proving TFE reached it, e.g. `| vgw | 200 |`

# 7. End-to-end: run a plan/apply and confirm the plan log renders and the
#    configuration version does NOT stay stuck in `fetching`.
```

If Step 5 fails because `mkdir` is not present in the image, fall back to the
verified pre-creation form, which needs no shell and no running server:

```go
_ = exec.Command(engine, "run", "--rm", "-v", tfeS3Volume+":/data",
    "--entrypoint", "mkdir", fmt.Sprintf("%s:%s", s3Image, s3Version),
    "-p", "/data/"+tfeS3Bucket).Run()
```

Both forms were verified working; `exec mkdir` succeeded 8/8 times with zero
sleep. Prefer `exec` for the smaller diff, switch to the pre-creation form if it
proves flaky.

### Twin path

```bash
go run . terraform create --target twin
go run . terraform status --target twin
<engine> exec hal-tfe-s3 ls -la /data   # expect tfe-data AND tfe-bis-data
```

---

## Alternatives considered and rejected

- **Pin `quay.io/minio/minio:RELEASE.2025-09-07T16-13-09Z`.** Quay is still alive
  and this tag is bit-identical to the cached image (same local image ID, same
  arm64 digest). A genuine zero-risk two-constant fix, and a valid emergency
  stopgap. Rejected as the destination because it keeps the lab on an archived,
  unmaintained, AGPL codebase that will never receive another CVE fix.
- **`pgsty/silo`** (community MinIO fork, AGPL-3.0, actively released, restores the
  full console). A literal drop-in. Rejected because it is structurally the same
  bet that just failed — one maintainer, one fork, one promise — and it measured
  the heaviest of the four at 214 MB idle RSS.
- **SeaweedFS** (`chrislusf/seaweedfs:4.47`, Apache-2.0, active since 2012). Fully
  validated: 40 MB multipart, path-style, presigned GET, correct 404 on HEAD of a
  nonexistent bucket, and a real bucket browser on port 23646. **The strongest
  fallback if VersityGW fails the runtime test.** Rejected as first choice because
  it has no `mkdir`-to-bucket shortcut (its `/data` holds `filerldb2/` and `.dat`
  files), a 506 MB image, and a larger diff — for capabilities TFE does not use.
- **Garage** — AGPL-3.0, no web UI, requires a config file plus layout bootstrap,
  no versioning or Object Lock, SQLite metadata is a single point of failure on one
  node.
- **RustFS** — Apache-2.0 and a literal port-for-port MinIO drop-in, but 1.0.0 GA
  landed 2026-09-16, CVE-2025-68926 scored CVSS 9.8 (hardcoded gRPC token identical
  across all deployments), 13 security advisories between Dec 2025 and Apr 2026,
  and its console did not respond under test.
- **Ceph RGW** — 8-15 GB RAM in stacked daemon minimums, no maintained
  single-container path.
- **Apache Ozone** — six containers minimum, JVM-based, no versioning or SSE.
- **Zenko CloudServer** — Docker Hub images frozen since 2023/2019; the maintained
  image is on GHCR and poorly documented.
- **OpenMaxIO** — no official Docker image, no commit since 2025-06-24.
- **LocalStack** — requires an auth token at startup since March 2026, free tier
  restricted to non-commercial use. A test emulator, not a persistent store.
