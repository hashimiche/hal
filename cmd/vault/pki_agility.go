package vault

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	vaultapi "github.com/hashicorp/vault/api"
	"github.com/spf13/cobra"
	"hal/internal/global"
)

//go:embed pki-agility/*
var agilityAssets embed.FS

var pkiAgility bool
var agilityImage string

const (
	agilityOwner   = global.CryptoAgilityOwner
	agilityCertTTL = "168h"
)

type agilityMode struct {
	name          string
	dns           string
	group         string
	algorithm     string
	mount         string
	rootMount     string
	containerPort int
	nodePort      int
	hostPort      int
}

func agilityModes() []agilityMode {
	return []agilityMode{
		{"traditional", agilityTraditionalDNS, "X25519", "RSA", pkiIntMount, pkiRootMount, 8443, agilityNodePort, agilityHostPort},
		{"hybrid", agilityHybridDNS, "X25519MLKEM768", "RSA", pkiIntMount, pkiRootMount, 8444, agilityHybridNodePort, agilityHybridHostPort},
		{"pqc", agilityPQCDNS, "X25519MLKEM768", "ML-DSA-65", pqcIntMount, pqcRootMount, 8445, agilityPQCNodePort, agilityPQCHostPort},
		{"pure", agilityPureDNS, "MLKEM768", "ML-DSA-65", pqcIntMount, pqcRootMount, 8446, agilityPureNodePort, agilityPureHostPort},
	}
}

func agilityModeLabel(name string) string {
	switch name {
	case "hybrid":
		return "hybrid key exchange"
	case "pqc":
		return "hybrid key exchange + PQC certificate"
	case "pure":
		return "pure PQC (experimental)"
	default:
		return name
	}
}

func agilityStateDir() (string, error) {
	return global.CryptoAgilityStateDir()
}

func agilityOwnerFile(dir string) string { return filepath.Join(dir, "owner") }

func agilityOwned(dir string) (bool, error) {
	b, err := os.ReadFile(agilityOwnerFile(dir))
	if os.IsNotExist(err) {
		if _, statErr := os.Stat(dir); os.IsNotExist(statErr) {
			return false, nil
		}
		return false, fmt.Errorf("refusing unowned crypto agility state at %s", dir)
	}
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(string(b)) != agilityOwner {
		return false, fmt.Errorf("refusing unowned crypto agility state at %s", dir)
	}
	return true, nil
}

func agilityDependencyActive() (bool, error) {
	dir, err := agilityStateDir()
	if err != nil {
		return false, err
	}
	owned, err := agilityOwned(dir)
	if err != nil {
		return false, err
	}
	if owned {
		return true, nil
	}
	return agilityNamespaceExists()
}

func agilityNamespaceExists() (bool, error) {
	if !halKindClusterRunning() {
		return false, nil
	}
	out, err := pqcKube(nil, "get", "namespace", agilityNamespace, "--ignore-not-found", "-o", "json")
	if err != nil {
		return false, err
	}
	if len(bytes.TrimSpace(out)) == 0 {
		return false, nil
	}
	var ns struct {
		Metadata struct{ Labels map[string]string }
	}
	if err := json.Unmarshal(out, &ns); err != nil {
		return false, err
	}
	if ns.Metadata.Labels["app.kubernetes.io/managed-by"] != agilityOwner {
		return false, fmt.Errorf("refusing unowned namespace %s", agilityNamespace)
	}
	return true, nil
}

func agilityCheckOwnership(client *vaultapi.Client, dir string) (bool, error) {
	owned, err := agilityOwned(dir)
	if err != nil {
		return false, err
	}
	ns, err := agilityNamespaceExists()
	if err != nil {
		return false, err
	}
	if ns && !owned {
		return false, fmt.Errorf("refusing to adopt %s without local ownership marker", agilityNamespace)
	}
	mounts, err := client.Sys().ListMounts()
	if err != nil {
		return false, err
	}
	for _, mount := range []string{pkiIntMount, pqcIntMount} {
		if mounts[mount+"/"] == nil {
			continue
		}
		role, err := client.Logical().Read(mount + "/roles/" + agilityRole)
		if err != nil {
			return false, err
		}
		if role != nil && !owned {
			return false, fmt.Errorf("refusing to adopt existing role %s/roles/%s", mount, agilityRole)
		}
	}
	return owned, nil
}

func agilityValidateFlags(cmd *cobra.Command) error {
	if pkiPQC {
		return errors.New("--agility and --pqc select separate demos")
	}
	for _, flag := range []string{"acme", "force", "hsm", "no-hsm", "root-mount", "int-mount", "root-ttl", "int-ttl", "allowed-domains", "max-cert-ttl", "cert-manager-version", "acme-cert-ttl", "pqc-cert-ttl", "pqc-image", "pqc-cluster-domain", "vault-pki-web-backend-image", "vault-pki-web-backend-tag", "vault-pki-caddy-image", "vault-pki-caddy-tag", "softhsm-token-label", "softhsm-pin", "softhsm-so-pin", "softhsm-managed-key", "softhsm-key-label", "softhsm-hmac-label"} {
		if cmd.Flags().Changed(flag) {
			return fmt.Errorf("--agility cannot be combined with --%s", flag)
		}
	}
	if strings.TrimSpace(agilityImage) == "" {
		return errors.New("--agility-image must name an NGINX/OpenSSL image")
	}
	return nil
}

func agilityCAs(client *vaultapi.Client, dir string) error {
	pqcDir, err := pqcStateDir()
	if err != nil {
		return err
	}
	owner, err := os.ReadFile(filepath.Join(pqcDir, "owner"))
	if err != nil || strings.TrimSpace(string(owner)) != pqcOwner {
		return fmt.Errorf("PQC CA ownership state is missing; run hal vault pki enable --pqc first")
	}
	mounts, err := client.Sys().ListMounts()
	if err != nil {
		return err
	}
	for _, mount := range []string{pqcRootMount, pqcIntMount} {
		if !pqcOwnedMount(mounts[mount+"/"]) {
			return fmt.Errorf("%s is not a PQC lab-owned PKI mount; run hal vault pki enable --pqc first", mount)
		}
	}
	for _, pair := range [][3]string{{"traditional", pkiRootMount, pkiIntMount}, {"pqc", pqcRootMount, pqcIntMount}} {
		root, err := pqcCA(client, pair[1])
		if err != nil {
			return err
		}
		inter, err := pqcCA(client, pair[2])
		if err != nil {
			return err
		}
		if root == "" || inter == "" {
			return fmt.Errorf("%s CA hierarchy is missing; run hal vault pki enable%s first", pair[0], map[bool]string{true: " --pqc", false: ""}[pair[0] == "pqc"])
		}
		if err := pqcSave(dir, pair[0]+"-root.pem", root); err != nil {
			return err
		}
		if err := pqcSave(dir, pair[0]+"-intermediate.pem", inter); err != nil {
			return err
		}
		if _, err := pqcExec(nil, "openssl", "verify", "-CAfile", filepath.Join(dir, pair[0]+"-root.pem"), filepath.Join(dir, pair[0]+"-intermediate.pem")); err != nil {
			return fmt.Errorf("%s CA chain: %w", pair[0], err)
		}
		if pair[0] == "pqc" {
			for _, name := range []string{"root", "intermediate"} {
				out, err := pqcExec(nil, "openssl", "x509", "-in", filepath.Join(dir, "pqc-"+name+".pem"), "-noout", "-text")
				if err != nil {
					return err
				}
				if !bytes.Contains(out, []byte("Public Key Algorithm: ML-DSA-65")) ||
					!bytes.Contains(out, []byte("Signature Algorithm: ML-DSA-65")) {
					return fmt.Errorf("PQC %s CA is not ML-DSA-65", name)
				}
			}
		}
	}
	return nil
}

func agilityIssue(client *vaultapi.Client, dir string) error {
	for _, mode := range agilityModes() {
		keyType := "rsa"
		if mode.algorithm == "ML-DSA-65" {
			keyType = "ml-dsa"
		}
		if mode.name == "traditional" || mode.name == "pqc" {
			// Each CA hierarchy has one role covering its two exact browser names.
			allowed := []string{agilityTraditionalDNS, agilityHybridDNS}
			if mode.name == "pqc" {
				allowed = []string{agilityPQCDNS, agilityPureDNS}
			}
			role := map[string]interface{}{
				"key_type": keyType, "allowed_domains": allowed, "allow_bare_domains": true,
				"allow_subdomains": false, "client_flag": false, "server_flag": true,
				"exclude_cn_from_sans": true,
				"key_usage":            []string{"DigitalSignature"}, "no_store": true,
				"ttl": agilityCertTTL, "max_ttl": agilityCertTTL,
			}
			if mode.algorithm == "ML-DSA-65" {
				role["parameter_set"] = "ml-dsa-65"
			} else {
				role["key_bits"] = 2048
			}
			if _, err := pqcWrite(client, mode.mount+"/roles/"+agilityRole, role); err != nil {
				return err
			}
		}
		data, err := pqcWrite(client, mode.mount+"/issue/"+agilityRole, map[string]interface{}{
			"common_name": mode.dns, "alt_names": mode.dns, "ttl": agilityCertTTL, "private_key_format": "pkcs8",
		})
		if err != nil {
			return err
		}
		leaf, _ := data["certificate"].(string)
		key, _ := data["private_key"].(string)
		if leaf == "" || key == "" {
			return fmt.Errorf("vault returned incomplete %s browser credentials", mode.name)
		}
		inter, err := os.ReadFile(filepath.Join(dir, map[bool]string{true: "pqc", false: "traditional"}[mode.algorithm == "ML-DSA-65"]+"-intermediate.pem"))
		if err != nil {
			return err
		}
		if err := pqcSave(dir, mode.name+".pem", leaf); err != nil {
			return err
		}
		if err := pqcSave(dir, mode.name+"-chain.pem", leaf+"\n"+string(inter)); err != nil {
			return err
		}
		if err := pqcSave(dir, mode.name+".key", key); err != nil {
			return err
		}
		if err := agilityValidateLeaf(dir, mode); err != nil {
			return err
		}
	}
	return nil
}

func agilityValidateLeaf(dir string, mode agilityMode) error {
	kind := "traditional"
	if mode.algorithm == "ML-DSA-65" {
		kind = "pqc"
	}
	leaf := filepath.Join(dir, mode.name+".pem")
	if _, err := pqcExec(nil, "openssl", "verify", "-CAfile", filepath.Join(dir, kind+"-root.pem"), "-untrusted", filepath.Join(dir, kind+"-intermediate.pem"), "-purpose", "sslserver", "-verify_hostname", mode.dns, leaf); err != nil {
		return fmt.Errorf("%s leaf validation: %w", mode.name, err)
	}
	out, err := pqcExec(nil, "openssl", "x509", "-in", leaf, "-noout", "-text")
	if err != nil {
		return err
	}
	want := "Public Key Algorithm: rsaEncryption"
	if mode.algorithm == "ML-DSA-65" {
		want = "Public Key Algorithm: ML-DSA-65"
	}
	if !bytes.Contains(out, []byte(want)) || !bytes.Contains(out, []byte("TLS Web Server Authentication")) || bytes.Contains(out, []byte("TLS Web Client Authentication")) {
		return fmt.Errorf("%s leaf lacks required %s key or exclusive serverAuth EKU", mode.name, mode.algorithm)
	}
	san, err := pqcExec(nil, "openssl", "x509", "-in", leaf, "-noout", "-ext", "subjectAltName")
	if err != nil {
		return err
	}
	lines := strings.SplitN(strings.TrimSpace(string(san)), "\n", 2)
	if len(lines) != 2 || strings.TrimSpace(lines[1]) != "DNS:"+mode.dns {
		return fmt.Errorf("%s leaf must have exactly one DNS SAN: %s", mode.name, mode.dns)
	}
	end, err := pqcExec(nil, "openssl", "x509", "-in", leaf, "-noout", "-enddate")
	if err != nil {
		return err
	}
	expiry, err := time.Parse("Jan _2 15:04:05 2006 MST", strings.TrimPrefix(strings.TrimSpace(string(end)), "notAfter="))
	if err != nil || expiry.Before(time.Now().Add(6*24*time.Hour)) {
		return fmt.Errorf("%s leaf has less than six days remaining; renew its issuing CA or check Vault TTL policy", mode.name)
	}
	return nil
}

func agilityFingerprint(dir, mode string) (string, error) {
	out, err := pqcExec(nil, "openssl", "x509", "-in", filepath.Join(dir, mode+".pem"), "-noout", "-fingerprint", "-sha256")
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(out)), "sha256 Fingerprint="))
	value = strings.TrimPrefix(value, "SHA256 Fingerprint=")
	if !regexp.MustCompile(`^([0-9A-F]{2}:){31}[0-9A-F]{2}$`).MatchString(value) {
		return "", fmt.Errorf("invalid %s SHA-256 fingerprint: %q", mode, value)
	}
	return value, nil
}

// agilityChainDERBytes counts the certificates NGINX sends (leaf and
// intermediate), excluding the locally trusted root and PEM encoding.
func agilityChainDERBytes(chain []byte) (int, error) {
	total, count := 0, 0
	for len(chain) > 0 {
		block, rest := pem.Decode(chain)
		if block == nil || block.Type != "CERTIFICATE" {
			return 0, errors.New("invalid certificate chain PEM")
		}
		total += len(block.Bytes)
		count++
		chain = bytes.TrimSpace(rest)
	}
	if count != 2 {
		return 0, fmt.Errorf("certificate chain has %d certificates; want leaf and intermediate", count)
	}
	return total, nil
}

func agilityNginxConfig(dir string) (string, error) {
	head, err := agilityAssets.ReadFile("pki-agility/nginx-head.conf")
	if err != nil {
		return "", err
	}
	var out strings.Builder
	out.Write(head)
	for _, mode := range agilityModes() {
		fp, err := agilityFingerprint(dir, mode.name)
		if err != nil {
			return "", err
		}
		signature := "rsa_pss_rsae_sha256"
		if mode.algorithm == "ML-DSA-65" {
			signature = "mldsa65"
		}
		fmt.Fprintf(&out, `
  server {
    listen %d ssl;
    server_name %s;
    ssl_certificate /certs/%s/tls.crt;
    ssl_certificate_key /certs/%s/tls.key;
    ssl_protocols TLSv1.3;
    ssl_conf_command Groups %s;
    ssl_conf_command SignatureAlgorithms %s;
    ssl_session_cache off;
    ssl_session_tickets off;
    keepalive_timeout 0;
    set $agility_mode %s;
    set $agility_dns %s;
    set $agility_group %s;
    set $agility_algorithm %s;
    set $agility_fingerprint "%s";
    root /app;
    add_header Cache-Control "no-store" always;
    add_header X-Content-Type-Options nosniff always;
    location = / { try_files /index.html =404; }
    location = /diagnostics { js_content agility.report; }
    location = /performance.json {
      default_type application/json;
      try_files /performance.json =404;
    }
    location = /performance.js {
      default_type application/javascript;
      try_files /performance.js =404;
    }
    location = /benchmark {
      default_type text/plain;
      return 200 "HAL crypto agility benchmark v1";
    }
    location / { return 404; }
  }
`, mode.containerPort, mode.dns, mode.name, mode.name, mode.group, signature, mode.name, mode.dns, mode.group, mode.algorithm, fp)
	}
	out.WriteString("}\n")
	return out.String(), nil
}

func agilityMetadata(name string) map[string]interface{} {
	return map[string]interface{}{"name": name, "namespace": agilityNamespace, "labels": map[string]string{"app.kubernetes.io/managed-by": agilityOwner}}
}

func agilityApply(obj interface{}) error {
	b, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	_, err = pqcKube(b, "apply", "-f", "-")
	return err
}

func agilityDeploy(dir, image string) error {
	if err := agilityApply(map[string]interface{}{"apiVersion": "v1", "kind": "Namespace", "metadata": map[string]interface{}{"name": agilityNamespace, "labels": map[string]string{"app.kubernetes.io/managed-by": agilityOwner}}}); err != nil {
		return err
	}
	volumes := []interface{}{}
	mounts := []interface{}{}
	config, err := agilityNginxConfig(dir)
	if err != nil {
		return err
	}
	page, err := agilityAssets.ReadFile("pki-agility/index.html")
	if err != nil {
		return err
	}
	script, err := agilityAssets.ReadFile("pki-agility/diagnostics.js")
	if err != nil {
		return err
	}
	performanceScript, err := agilityAssets.ReadFile("pki-agility/performance.js")
	if err != nil {
		return err
	}
	chainSizes := make(map[string]int, len(agilityModes()))
	for _, mode := range agilityModes() {
		chain, err := os.ReadFile(filepath.Join(dir, mode.name+"-chain.pem"))
		if err != nil {
			return err
		}
		chainSizes[mode.name], err = agilityChainDERBytes(chain)
		if err != nil {
			return fmt.Errorf("%s chain size: %w", mode.name, err)
		}
		key, err := os.ReadFile(filepath.Join(dir, mode.name+".key"))
		if err != nil {
			return err
		}
		obj := map[string]interface{}{"apiVersion": "v1", "kind": "Secret", "metadata": agilityMetadata(mode.name + "-browser"), "type": "kubernetes.io/tls", "stringData": map[string]string{"tls.crt": string(chain), "tls.key": string(key)}}
		b, _ := json.Marshal(obj)
		if _, err := pqcKube(b, "apply", "--server-side", "--field-manager="+agilityOwner, "-f", "-"); err != nil {
			return err
		}
		volumes = append(volumes, map[string]interface{}{"name": mode.name, "secret": map[string]interface{}{"secretName": mode.name + "-browser", "defaultMode": 0440}})
		mounts = append(mounts, map[string]interface{}{"name": mode.name, "mountPath": "/certs/" + mode.name, "readOnly": true})
	}
	performanceJSON, err := json.Marshal(map[string]interface{}{"chain_der_bytes": chainSizes})
	if err != nil {
		return err
	}
	if err := agilityApply(map[string]interface{}{"apiVersion": "v1", "kind": "ConfigMap", "metadata": agilityMetadata("app"), "data": map[string]string{"nginx.conf": config, "index.html": string(page), "diagnostics.js": string(script), "performance.js": string(performanceScript), "performance.json": string(performanceJSON)}}); err != nil {
		return err
	}
	volumes = append(volumes, map[string]interface{}{"name": "app", "configMap": map[string]string{"name": "app"}}, map[string]interface{}{"name": "tmp", "emptyDir": map[string]interface{}{"sizeLimit": "32Mi"}})
	mounts = append(mounts, map[string]interface{}{"name": "app", "mountPath": "/app", "readOnly": true}, map[string]interface{}{"name": "tmp", "mountPath": "/tmp"})
	digestInput := append([]byte(config), page...)
	digestInput = append(digestInput, script...)
	digestInput = append(digestInput, performanceScript...)
	digestInput = append(digestInput, performanceJSON...)
	digest := sha256.Sum256(digestInput)
	labels := map[string]string{"app": agilityOwner, "app.kubernetes.io/managed-by": agilityOwner}
	ports := []interface{}{}
	servicePorts := []interface{}{}
	for _, mode := range agilityModes() {
		ports = append(ports, map[string]interface{}{"name": mode.name, "containerPort": mode.containerPort})
		servicePorts = append(servicePorts, map[string]interface{}{"name": mode.name, "port": mode.containerPort, "targetPort": mode.containerPort, "nodePort": mode.nodePort})
	}
	pod := map[string]interface{}{"automountServiceAccountToken": false, "enableServiceLinks": false, "securityContext": map[string]interface{}{"runAsNonRoot": true, "runAsUser": 65532, "runAsGroup": 65532, "fsGroup": 65532}, "volumes": volumes, "containers": []interface{}{map[string]interface{}{
		"name": "app", "image": image, "imagePullPolicy": "IfNotPresent", "command": []string{"nginx"}, "args": []string{"-c", "/app/nginx.conf", "-g", "daemon off;"}, "ports": ports, "volumeMounts": mounts,
		"securityContext": map[string]interface{}{"allowPrivilegeEscalation": false, "readOnlyRootFilesystem": true, "capabilities": map[string]interface{}{"drop": []string{"ALL"}}},
		"readinessProbe":  map[string]interface{}{"tcpSocket": map[string]interface{}{"port": 8443}, "initialDelaySeconds": 1, "periodSeconds": 2},
		"resources":       map[string]interface{}{"requests": map[string]string{"cpu": "50m", "memory": "32Mi"}, "limits": map[string]string{"memory": "128Mi"}},
	}}}
	deployment := map[string]interface{}{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": agilityMetadata("app"), "spec": map[string]interface{}{"replicas": 1, "selector": map[string]interface{}{"matchLabels": map[string]string{"app": agilityOwner}}, "template": map[string]interface{}{"metadata": map[string]interface{}{"labels": labels, "annotations": map[string]string{"hal.agility-config": fmt.Sprintf("%x", digest)}}, "spec": pod}}}
	if err := agilityApply(deployment); err != nil {
		return err
	}
	service := map[string]interface{}{"apiVersion": "v1", "kind": "Service", "metadata": agilityMetadata("app"), "spec": map[string]interface{}{"type": "NodePort", "selector": map[string]string{"app": agilityOwner}, "ports": servicePorts}}
	if err := agilityApply(service); err != nil {
		return err
	}
	_, err = pqcKube(nil, "rollout", "status", "deployment/app", "-n", agilityNamespace, "--timeout=120s")
	return err
}

func agilityPortMapped(engine string) bool {
	for _, mode := range agilityModes() {
		out, err := pqcExec(nil, engine, "port", kindControlPlaneName, fmt.Sprintf("%d/tcp", mode.nodePort))
		if err != nil || !strings.Contains(string(out), fmt.Sprintf(":%d", mode.hostPort)) {
			return false
		}
	}
	return true
}

func agilityProbe(dir string, mode agilityMode, group string, wantSuccess bool) error {
	kind := "traditional"
	if mode.algorithm == "ML-DSA-65" {
		kind = "pqc"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "openssl", "s_client", "-connect", fmt.Sprintf("127.0.0.1:%d", mode.hostPort), "-servername", mode.dns, "-tls1_3", "-groups", group, "-verify_return_error", "-verify_hostname", mode.dns, "-CAfile", filepath.Join(dir, kind+"-root.pem"), "-brief")
	cmd.Stdin = strings.NewReader("Q\n")
	out, err := cmd.CombinedOutput()
	if wantSuccess {
		if err != nil || !bytes.Contains(out, []byte("Verification: OK")) ||
			!bytes.Contains(out, []byte("Protocol version: TLSv1.3")) ||
			agilityNegotiatedGroup(out) != group {
			return fmt.Errorf("%s expected %s handshake failed: %v (%s)", mode.name, group, err, strings.TrimSpace(string(out)))
		}
		if mode.algorithm == "ML-DSA-65" && !bytes.Contains(out, []byte("Signature type: mldsa65")) {
			return fmt.Errorf("%s handshake did not use ML-DSA-65: %s", mode.name, strings.TrimSpace(string(out)))
		}
	} else if err == nil {
		return fmt.Errorf("%s unexpectedly accepted disallowed group %s", mode.name, group)
	}
	return nil
}

func agilityNegotiatedGroup(out []byte) string {
	for _, line := range bytes.Split(out, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if group, ok := bytes.CutPrefix(line, []byte("Negotiated TLS1.3 group:")); ok {
			return strings.TrimSpace(string(group))
		}
		if key, ok := bytes.CutPrefix(line, []byte("Peer Temp Key:")); ok {
			name, _, _ := bytes.Cut(key, []byte(","))
			return strings.TrimSpace(string(name))
		}
	}
	return ""
}

func agilityVerifyRuntime(dir string, modes []agilityMode) error {
	for _, mode := range modes {
		if err := agilityProbe(dir, mode, mode.group, true); err != nil {
			return err
		}
		other := "X25519MLKEM768"
		if mode.group == "X25519MLKEM768" {
			other = "MLKEM768"
		}
		if err := agilityProbe(dir, mode, other, false); err != nil {
			return err
		}
	}
	return nil
}

func agilityVerifyDeployment(dir, engine string) error {
	if agilityPortMapped(engine) {
		if err := agilityVerifyRuntime(dir, agilityModes()); err != nil {
			return fmt.Errorf("TLS mode enforcement probe failed: %w", err)
		}
	} else {
		if err := agilityVerifyWithPortForward(dir); err != nil {
			return fmt.Errorf("TLS mode enforcement probe through temporary port-forward failed: %w", err)
		}
		fmt.Println("Existing KinD cluster lacks host ports 8094-8097; use the port-forward shown below to open the app.")
	}
	fmt.Println("TLS probes verified each mode with its required group and rejected the other group.")
	return nil
}

var agilityForwardPattern = regexp.MustCompile(`Forwarding from 127\.0\.0\.1:(\d+) -> (844[3456])`)

func agilityForwardPort(line string) (local, remote int, ok bool) {
	match := agilityForwardPattern.FindStringSubmatch(line)
	if len(match) != 3 {
		return 0, 0, false
	}
	local, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, 0, false
	}
	remote, err = strconv.Atoi(match[2])
	if err != nil {
		return 0, 0, false
	}
	return local, remote, true
}

func agilityVerifyWithPortForward(dir string) error {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "kubectl", "--context", "kind-kind", "-n", agilityNamespace,
		"port-forward", "--address", "127.0.0.1", "service/app", ":8443", ":8444", ":8445", ":8446")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		cancel()
		return err
	}
	waited := false
	defer func() {
		if !waited {
			cancel()
			_ = cmd.Wait()
		}
	}()
	lines := make(chan string, 8)
	go func() {
		scan := bufio.NewScanner(stdout)
		for scan.Scan() {
			select {
			case lines <- scan.Text():
			case <-ctx.Done():
				close(lines)
				return
			}
		}
		close(lines)
	}()
	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()
	forwarded := map[int]int{}
	for len(forwarded) < len(agilityModes()) {
		select {
		case line, open := <-lines:
			if !open {
				cancel()
				_ = cmd.Wait()
				waited = true
				return fmt.Errorf("temporary port-forward exited before all ports were ready: %s", strings.TrimSpace(stderr.String()))
			}
			if local, remote, ok := agilityForwardPort(line); ok {
				forwarded[remote] = local
			}
		case <-timer.C:
			return fmt.Errorf("temporary port-forward did not expose all four TLS listeners within 20s")
		}
	}
	modes := agilityModes()
	for i := range modes {
		modes[i].hostPort = forwarded[modes[i].containerPort]
	}
	return agilityVerifyRuntime(dir, modes)
}

func agilityStatus(client *vaultapi.Client, dir string) error {
	owned, err := agilityCheckOwnership(client, dir)
	if err != nil {
		return err
	}
	fmt.Printf("Crypto agility owned state: %t\n", owned)
	if !owned {
		fmt.Println("Next Step: hal vault pki enable --agility")
		return nil
	}
	var created []string
	for _, entry := range [][2]string{{agilityConventionalMarker, "conventional PKI"}, {agilityPQCMarker, "PQC lab"}, {agilityClusterMarker, "KinD cluster"}} {
		marked, err := agilityHasMarker(dir, entry[0])
		if err != nil {
			return err
		}
		if marked {
			created = append(created, entry[1])
		}
	}
	if len(created) > 0 {
		fmt.Printf("Prerequisites created by agility: %s\n", strings.Join(created, ", "))
	}
	out, err := pqcKube(nil, "get", "deployment", "app", "-n", agilityNamespace, "--ignore-not-found", "-o", "json")
	ready := false
	if err == nil && len(bytes.TrimSpace(out)) > 0 {
		var d struct {
			Status struct {
				ReadyReplicas int `json:"readyReplicas"`
			}
		}
		if json.Unmarshal(out, &d) == nil {
			ready = d.Status.ReadyReplicas > 0
		}
	}
	fmt.Printf("NGINX ready: %t (readiness is not TLS proof)\n", ready)
	for _, mode := range agilityModes() {
		fmt.Printf("%s: https://%s:%d — %s authentication, %s key exchange (configured)\n", agilityModeLabel(mode.name), mode.dns, mode.hostPort, mode.algorithm, mode.group)
	}
	fmt.Println("Pure PQC is experimental: standard browsers will likely fail to connect without standalone MLKEM768 support. Verify with OpenSSL 3.5+.")
	engine, err := global.DetectEngine()
	if err == nil && agilityPortMapped(engine) {
		fmt.Println("Next Step: open a mode URL and inspect Chrome certificate details and diagnostics.")
	} else {
		fmt.Printf("Next Step: kubectl --context kind-kind -n %s port-forward --address 127.0.0.1 service/app 8094:8443 8095:8444 8096:8445 8097:8446\n", agilityNamespace)
	}
	return nil
}

func agilityDisable(client *vaultapi.Client, dir string) error {
	owned, err := agilityCheckOwnership(client, dir)
	if err != nil {
		return err
	}
	if !owned {
		return nil
	}
	if halKindClusterRunning() {
		if _, err := pqcKube(nil, "delete", "namespace", agilityNamespace, "--ignore-not-found", "--wait=true", "--timeout=120s"); err != nil {
			return err
		}
	}
	mounts, err := client.Sys().ListMounts()
	if err != nil {
		return err
	}
	for _, mount := range []string{pkiIntMount, pqcIntMount} {
		if mounts[mount+"/"] == nil {
			continue
		}
		if _, err := client.Logical().Delete(mount + "/roles/" + agilityRole); err != nil {
			return err
		}
	}
	return nil
}

func runAgility(cmd *cobra.Command, args []string) error {
	enable, disable, update := pkiEnable, pkiDisable, pkiUpdate
	if err := parseLifecycleAction(args, &enable, &disable, &update); err != nil {
		return err
	}
	if err := agilityValidateFlags(cmd); err != nil {
		return err
	}
	action := "status"
	if enable {
		action = "enable"
	}
	if disable {
		action = "disable"
	}
	if update {
		action = "update"
	}
	if global.DryRun {
		switch action {
		case "enable", "update":
			fmt.Printf("[DRY RUN] Crypto agility %s: provision or reuse conventional PKI and PQC lab, then manage browser roles and namespace %s. No state changed.\n", action, agilityNamespace)
		case "disable":
			fmt.Printf("[DRY RUN] Crypto agility disable: remove browser roles and namespace %s, plus prerequisites this lifecycle created. No state changed.\n", agilityNamespace)
		default:
			fmt.Println("[DRY RUN] Crypto agility status: read-only; no state changed.")
		}
		return nil
	}
	client, err := GetHealthyClient()
	if err != nil {
		if action == "status" {
			fmt.Printf("Crypto agility: down (%v)\nNext Step: hal vault create\n", err)
			return nil
		}
		return err
	}
	dir, err := agilityStateDir()
	if err != nil {
		return err
	}
	if action == "status" {
		return agilityStatus(client, dir)
	}
	owned, err := agilityCheckOwnership(client, dir)
	if err != nil {
		return err
	}
	if action == "disable" {
		if err := agilityCheckConventionalConsumers(dir); err != nil {
			return err
		}
		if err := agilityDisable(client, dir); err != nil {
			return err
		}
		if owned {
			if err := agilityCleanupDependencies(client, dir); err != nil {
				return err
			}
			if err := global.RemoveCryptoAgilityState(); err != nil {
				return err
			}
		}
		fmt.Println("Crypto agility disabled; reused PKI and shared resources preserved.")
		return nil
	}
	if err := pqcPrerequisites(client); err != nil {
		return err
	}
	for _, tool := range []string{"kind", "kubectl", "openssl"} {
		if _, err := exec.LookPath(tool); err != nil {
			return err
		}
	}
	// Check PQC ownership before creating a conventional CA. A collision in
	// either lab must not leave a newly provisioned conventional hierarchy.
	if err := pqcCheckOwnership(client); err != nil {
		return err
	}
	engine, err := global.DetectEngine()
	if err != nil {
		return err
	}
	if !owned {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		if err := os.WriteFile(agilityOwnerFile(dir), []byte(agilityOwner), 0600); err != nil {
			return err
		}
	}
	if err := agilityEnsureConventional(client, dir, engine); err != nil {
		return err
	}
	clusterWasRunning := halKindClusterRunning()
	pqcErr := agilityEnsurePQC(client, dir)
	if !clusterWasRunning && halKindClusterRunning() {
		if err := agilityMark(dir, agilityClusterMarker); err != nil {
			return err
		}
	}
	if pqcErr != nil {
		return pqcErr
	}
	if err := agilityCAs(client, dir); err != nil {
		return err
	}
	if action == "enable" && owned {
		if b, err := os.ReadFile(filepath.Join(dir, "deployed")); err == nil && len(b) > 0 {
			if err := agilityVerifyDeployment(dir, engine); err != nil {
				return err
			}
			fmt.Println("Crypto agility already enabled; use update to rotate browser certificates.")
			return agilityStatus(client, dir)
		}
	}
	originalImage := pqcImage
	pqcImage = agilityImage
	image, err := pqcPrepareImage(engine)
	pqcImage = originalImage
	if err != nil {
		return err
	}
	if err := agilityIssue(client, dir); err != nil {
		return err
	}
	if err := ensureHALKindCluster(engine, pkiKindNodeImage); err != nil {
		return err
	}
	if err := agilityDeploy(dir, image); err != nil {
		return err
	}
	if err := agilityVerifyDeployment(dir, engine); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "deployed"), []byte(image), 0600); err != nil {
		return err
	}
	fmt.Printf("Private trust roots (not installed): %s and %s\n", filepath.Join(dir, "traditional-root.pem"), filepath.Join(dir, "pqc-root.pem"))
	return agilityStatus(client, dir)
}

func init() {
	f := vaultPKICmd.Flags()
	f.BoolVar(&pkiAgility, "agility", false, "Deploy four browser HTTPS modes; enable missing conventional PKI and PQC lab prerequisites")
	f.StringVar(&agilityImage, "agility-image", pqcDefaultImage, "NGINX/OpenSSL image for the browser crypto agility demo")
}
