#!/usr/bin/env python3
"""Benchmark fresh, verified TLS connections to the local crypto agility app."""

import argparse
import csv
import hashlib
import io
import json
import math
import pathlib
import platform
import shlex
import statistics
import subprocess
import tempfile
from datetime import datetime, timezone


MODES = ("traditional", "hybrid", "pqc", "pure")
METRICS = ("tcp_connect_ms", "tls_handshake_ms", "request_ttfb_ms", "total_ttfb_ms")
GROUPS = {"traditional": "X25519", "hybrid": "X25519MLKEM768", "pqc": "X25519MLKEM768", "pure": "MLKEM768"}
CIPHER = "TLS_AES_256_GCM_SHA384"


def command(*args):
    return subprocess.check_output(args, text=True, stderr=subprocess.STDOUT).strip()


def sha256(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def percentile95(values):
    ordered = sorted(values)
    return ordered[math.ceil(len(ordered) * 0.95) - 1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--runs", type=int, default=30, help="measured full handshakes per mode (minimum 30)")
    parser.add_argument("--warmups", type=int, default=3, help="verified warmup connections per mode")
    parser.add_argument("--state-dir", type=pathlib.Path, default=pathlib.Path.home() / ".hal/crypto-agility")
    parser.add_argument("--out", type=pathlib.Path, default=None, help="new directory for raw CSV, summary CSV, and setup JSON")
    args = parser.parse_args()
    if args.runs < 30 or args.runs > 10000 or args.warmups < 0 or args.warmups > 1000:
        parser.error("runs must be 30..10000 and warmups must be 0..1000")
    state_dir = args.state_dir.expanduser().resolve()
    for name in ("traditional-root.pem", "pqc-root.pem", "traditional.pem", "hybrid.pem", "pqc.pem", "pure.pem"):
        if not (state_dir / name).is_file():
            parser.error(f"missing {state_dir / name}; enable the agility lab first")

    source = pathlib.Path(__file__).with_name("crypto_agility_bench.c")
    version = command("pkg-config", "--modversion", "openssl")
    parts = tuple(int(part) for part in version.split(".")[:2])
    if parts < (3, 5):
        parser.error(f"OpenSSL 3.5+ is required; pkg-config found {version}")
    flags = shlex.split(command("pkg-config", "--cflags", "--libs", "openssl"))
    started = datetime.now(timezone.utc).isoformat()
    with tempfile.TemporaryDirectory(prefix="hal-agility-bench-") as temp:
        binary = pathlib.Path(temp) / "bench"
        subprocess.run(["cc", "-O2", "-std=c11", "-Wall", "-Wextra", "-Werror", str(source),
                        "-o", str(binary), *flags], check=True)
        result = subprocess.run([str(binary), str(args.runs), str(args.warmups), str(state_dir)],
                                capture_output=True, text=True, check=False, timeout=600)
    if result.returncode:
        raise SystemExit(f"benchmark failed (no result files written):\n{result.stderr}")

    rows = list(csv.DictReader(io.StringIO(result.stdout)))
    if len(rows) != args.runs * len(MODES):
        raise SystemExit(f"expected {args.runs * len(MODES)} raw samples, got {len(rows)}")
    for name in MODES:
        samples = [row for row in rows if row["mode"] == name]
        if len(samples) != args.runs or sorted(int(row["sequence"]) for row in samples) != list(range(1, args.runs + 1)):
            raise SystemExit(f"incomplete {name} sample sequence")
        for row in samples:
            if (row["tls_version"] != "TLSv1.3" or row["cipher_suite"] != CIPHER or
                    row["negotiated_group"].lower() != GROUPS[name].lower() or
                    row["session_reused"] != "false" or int(row["chain_der_bytes"]) <= 0):
                raise SystemExit(f"invalid TLS evidence in {name} sample {row['sequence']}")
            if any(float(row[metric]) <= 0 for metric in METRICS):
                raise SystemExit(f"nonpositive timing in {name} sample {row['sequence']}")

    summary = []
    for name in MODES:
        samples = [row for row in rows if row["mode"] == name]
        for metric in METRICS:
            values = [float(row[metric]) for row in samples]
            summary.append({"mode": name, "metric": metric, "n": args.runs,
                            "median_ms": f"{statistics.median(values):.6f}",
                            "p95_ms": f"{percentile95(values):.6f}",
                            "certificate_algorithm": samples[0]["certificate_algorithm"],
                            "negotiated_group": samples[0]["negotiated_group"],
                            "chain_der_bytes": samples[0]["chain_der_bytes"]})

    out = args.out or (state_dir / "benchmarks" / datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ"))
    out = out.expanduser().resolve()
    out.mkdir(parents=True, exist_ok=False)
    raw_path = out / "raw.csv"
    raw_path.write_text(result.stdout, encoding="utf-8")
    summary_path = out / "summary.csv"
    with summary_path.open("w", newline="", encoding="utf-8") as file:
        writer = csv.DictWriter(file, fieldnames=summary[0].keys())
        writer.writeheader()
        writer.writerows(summary)

    try:
        ports = command("podman", "port", "kind-control-plane")
        path = "KinD node loopback host mappings" if all(f"127.0.0.1:{port}" in ports for port in (8094, 8095, 8096, 8097)) else "local forwarding; inspect podman/kubectl setup"
    except (FileNotFoundError, subprocess.CalledProcessError):
        path = "local forwarding; inspect podman/kubectl setup"
    try:
        deployment = json.loads(command("kubectl", "--context", "kind-kind", "--request-timeout=3s",
                                        "-n", "hal-crypto-agility", "get", "deployment", "app", "-o", "json"))
        configmap = json.loads(command("kubectl", "--context", "kind-kind", "--request-timeout=3s",
                                       "-n", "hal-crypto-agility", "get", "configmap", "app", "-o", "json"))
        server_image = deployment["spec"]["template"]["spec"]["containers"][0]["image"]
        server_config_sha256 = hashlib.sha256(configmap["data"]["nginx.conf"].encode()).hexdigest()
    except (FileNotFoundError, subprocess.CalledProcessError, KeyError, IndexError, json.JSONDecodeError):
        server_image = None
        server_config_sha256 = None
    setup = {
        "started_at_utc": started,
        "machine": f"{platform.system()} {platform.release()} {platform.machine()}",
        "openssl_library": version,
        "openssl_cli": command("openssl", "version"),
        "network_path": f"127.0.0.1:8094-8097 to {path} to one NGINX app pod",
        "server_image": server_image,
        "server_nginx_config_sha256": server_config_sha256,
        "tls_version": "TLSv1.3",
        "cipher_suite": CIPHER,
        "http_request": "GET /benchmark HTTP/1.1 with Connection: close",
        "http_response_body": "HAL crypto agility benchmark v1",
        "runs_per_mode": args.runs,
        "warmups_per_mode": args.warmups,
        "sampling_order": "round robin Traditional, Hybrid key exchange, Hybrid PQC certificate, Pure PQC",
        "sample_timing": "monotonic clock in one process; new TCP socket and SSL object per sample",
        "certificate_validation": "trust root, DNS hostname, exact Vault leaf SHA-256, TLS version, cipher and negotiated group",
        "resumed_sessions": "not measured; client cache/tickets and server cache/tickets disabled; every row requires session_reused=false",
        "tcp_connect_ms": "socket connect start to TCP connect completion",
        "tls_handshake_ms": "immediately before SSL_connect to verified handshake completion",
        "request_ttfb_ms": "after complete HTTP request write to first SSL_read bytes",
        "total_ttfb_ms": "before TCP connect to first SSL_read bytes",
        "excluded": ["Vault certificate issuance", "PQC workload backend mTLS"],
        "raw_csv_sha256": sha256(raw_path),
        "benchmark_c_sha256": sha256(source),
        "benchmark_python_sha256": sha256(pathlib.Path(__file__)),
    }
    (out / "setup.json").write_text(json.dumps(setup, indent=2) + "\n", encoding="utf-8")
    print(f"Raw samples: {raw_path}\nSummary: {summary_path}\nSetup: {out / 'setup.json'}")
    for item in summary:
        print(f"{item['mode']:12} {item['metric']:20} median {item['median_ms']:>10} ms  p95 {item['p95_ms']:>10} ms")


if __name__ == "__main__":
    main()
