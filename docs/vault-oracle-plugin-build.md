# Building vault-plugin-database-oracle

The Oracle database plugin for Vault is a CGO binary that links against Oracle Instant Client (`libclntsh.so.19.1`). HashiCorp publishes a prebuilt `linux/amd64` binary. There is no official `linux/arm64` build.

This document covers building the plugin for both architectures.

---

## amd64 (Intel/AMD Linux)

HashiCorp publishes a prebuilt binary at `releases.hashicorp.com`. Download it and pass the path:

```bash
hal vault database enable --backend oracle \
  --oracle-plugin-path /path/to/vault-plugin-database-oracle
```

---

## arm64 (Apple Silicon / AWS Graviton)

You need to compile the plugin from source. Oracle provides arm64 Instant Client starting with version 23.26, which includes `libclntsh.so.19.1` as a backward-compat symlink.

### Prerequisites

- Docker or Podman with `linux/arm64` support (native on Apple Silicon)
- Internet access (downloads ~400 MB total: Go, Instant Client, plugin source)

### Build

Run this on your arm64 machine:

```bash
# 1. Write the oci8.pc pkg-config file
mkdir -p /tmp/oracle-plugin-build
cat > /tmp/oracle-plugin-build/oci8.pc <<'EOF'
libdir=/opt/oracle/instantclient_23_26
includedir=/opt/oracle/instantclient_23_26/sdk/include

Name: oci8
Description: oci8 library
Libs: -L${libdir} -lclntsh
Cflags: -I${includedir}
Version: 23.26
EOF

# 2. Write the Dockerfile
cat > /tmp/oracle-plugin-build/Dockerfile <<'EOF'
FROM --platform=linux/arm64 debian:12-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    wget unzip git gcc libc6-dev pkg-config ca-certificates libaio1 libaio-dev \
    && rm -rf /var/lib/apt/lists/*

# Oracle Instant Client 23.26.2 arm64 (basic + SDK headers)
RUN mkdir -p /opt/oracle && \
    wget -q "https://download.oracle.com/otn_software/linux/instantclient/2326200/instantclient-basic-linux.arm64-23.26.2.0.0.zip" \
        -O /tmp/ic.zip && \
    unzip -qo /tmp/ic.zip -d /opt/oracle && rm /tmp/ic.zip
RUN wget -q "https://download.oracle.com/otn_software/linux/instantclient/2326200/instantclient-sdk-linux.arm64-23.26.2.0.0.zip" \
        -O /tmp/sdk.zip && \
    unzip -qo /tmp/sdk.zip -d /opt/oracle && rm /tmp/sdk.zip

RUN mkdir -p /usr/local/lib/pkgconfig
COPY oci8.pc /usr/local/lib/pkgconfig/oci8.pc

RUN echo "/opt/oracle/instantclient_23_26" > /etc/ld.so.conf.d/oracle-instantclient.conf && ldconfig

# Go 1.23.6 (matches plugin .go-version)
RUN wget -q "https://golang.org/dl/go1.23.6.linux-arm64.tar.gz" -O /tmp/go.tar.gz && \
    tar -C /usr/local -xzf /tmp/go.tar.gz && rm /tmp/go.tar.gz
ENV PATH="/usr/local/go/bin:$PATH"
ENV GOPATH=/go
ENV PKG_CONFIG_PATH=/usr/local/lib/pkgconfig

# Clone and build
RUN git clone --depth=1 https://github.com/hashicorp/vault-plugin-database-oracle.git \
    /src/vault-plugin-database-oracle
WORKDIR /src/vault-plugin-database-oracle
RUN mkdir -p /out && \
    CGO_ENABLED=1 GOOS=linux GOARCH=arm64 \
    go build -o /out/vault-plugin-database-oracle ./plugin/.
EOF

# 3. Build
docker build --platform linux/arm64 \
    -t vault-oracle-plugin-builder:arm64 \
    -f /tmp/oracle-plugin-build/Dockerfile \
    /tmp/oracle-plugin-build

# 4. Extract the binary
docker run --rm --platform linux/arm64 \
    -v /tmp/oracle-plugin-build:/out \
    vault-oracle-plugin-builder:arm64 \
    cp /out/vault-plugin-database-oracle /out/vault-plugin-database-oracle

ls -lh /tmp/oracle-plugin-build/vault-plugin-database-oracle
```

The binary will be at `/tmp/oracle-plugin-build/vault-plugin-database-oracle`.

### Use with hal

```bash
hal vault database enable --backend oracle \
  --oracle-plugin-path /tmp/oracle-plugin-build/vault-plugin-database-oracle
```

---

## What `hal vault database enable --backend oracle` does with the binary

1. Builds `hal-vault-oracle-runtime` — a debian-slim image with the Vault binary + Oracle Instant Client (the official Alpine image lacks glibc)
2. Restarts Vault on the runtime image (preserves license, volumes, ports)
3. Copies the plugin binary into `/vault/plugins/`
4. Registers it with Vault via the API using sha256 only (Enterprise requirement — no `version` field)
5. Starts Oracle Database Free, configures the database secrets engine, and generates test credentials

---

## Version compatibility

| Plugin release | Instant Client (amd64) | Instant Client (arm64) | Oracle DB |
|---|---|---|---|
| 0.14.1+ent | 19.26 | 23.26.2 (symlink to 19.1) | 19c, 21c, 23ai Free |
| 0.13.0+ent | 19.26 | 23.26.2 (symlink to 19.1) | 19c, 21c |

- arm64 Instant Client 23.26.2 provides `libclntsh.so.19.1` as a backward-compat symlink → the plugin loads correctly
- `gvenzl/oracle-free` (Oracle Database 23ai Free) is confirmed compatible with the plugin's SQL dialect

---

## Things that may need updating

| What | Where to check | Current value |
|---|---|---|
| Plugin version | https://releases.hashicorp.com/vault-plugin-database-oracle/ | `0.14.1+ent` |
| amd64 Instant Client | https://www.oracle.com/database/technologies/instant-client/linux-x86-64-downloads.html | `19.26.0.0.0` |
| arm64 Instant Client | https://www.oracle.com/database/technologies/instant-client/linux-arm-aarch64-downloads.html | `23.26.2.0.0` |
| Go version (for source build) | https://github.com/hashicorp/vault-plugin-database-oracle/blob/main/.go-version | `1.23.6` |
| Oracle Free image | https://hub.docker.com/r/gvenzl/oracle-free/tags | `gvenzl/oracle-free:slim` |
| Vault Enterprise image | https://hub.docker.com/r/hashicorp/vault-enterprise/tags | `2.0.4-ent` |
