package vault

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"embed"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
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

// pqcAssets contains runtime configuration only; generated identities stay outside the binary.
//
//go:embed pki-pqc/*
var pqcAssets embed.FS

var pkiPQC bool
var pqcImage, pqcDomain string

const pqcDefaultCertTTL = "10s"

var pqcCertTTL = pqcDefaultCertTTL

func validatePQCFlags(cmd *cobra.Command, action string) error {
	// PQC currently has one deployment mode: the shared Kubernetes cluster.
	// An explicit --k8s remains accepted; status never provisions infrastructure.
	for _, flag := range []string{"acme", "agility-image", "hsm", "no-hsm", "force", "root-mount", "int-mount", "root-ttl", "int-ttl", "allowed-domains", "max-cert-ttl", "acme-cert-ttl", "cert-manager-version", "vault-pki-web-backend-image", "vault-pki-web-backend-tag", "vault-pki-caddy-image", "vault-pki-caddy-tag", "softhsm-token-label", "softhsm-pin", "softhsm-so-pin", "softhsm-managed-key", "softhsm-key-label", "softhsm-hmac-label"} {
		if cmd.Flags().Changed(flag) {
			return fmt.Errorf("--pqc cannot be combined with --%s; PQC resources are isolated from conventional PKI/ACME/HSM", flag)
		}
	}
	if ttl, err := time.ParseDuration(pqcCertTTL); err != nil || ttl < 10*time.Second || ttl > 24*time.Hour {
		return fmt.Errorf("--pqc-cert-ttl must be a duration between 10s and 24h")
	}
	if !regexp.MustCompile(`^[a-z0-9]+([.-][a-z0-9]+)*$`).MatchString(pqcDomain) {
		return fmt.Errorf("invalid --pqc-cluster-domain")
	}
	if strings.TrimSpace(pqcImage) == "" {
		return fmt.Errorf("--pqc-image must name a compatible NGINX image with njs")
	}
	return nil
}

func pqcCheckVersion(v string) error {
	if !vaultVersionAtLeast(v, "2.2.0") || !regexp.MustCompile(`\+ent(?:\.[a-z0-9]+)*$`).MatchString(v) || strings.Contains(strings.Split(v, "+")[0], "-") {
		return fmt.Errorf("PQC requires stable Vault Enterprise 2.2.0+; running %s. Select a supported licensed deployment explicitly; HAL will not replace or upgrade it", v)
	}
	return nil
}

func pqcPrerequisites(client *vaultapi.Client) error {
	h, err := client.Sys().Health()
	if err != nil {
		return err
	}
	if err = pqcCheckVersion(h.Version); err != nil {
		return err
	}
	license, err := client.Logical().Read("sys/license/status")
	if err != nil {
		return fmt.Errorf("read Enterprise license status: %w", err)
	}
	active := false
	if license != nil {
		for _, k := range []string{"autoloaded", "persisted_autoload", "license"} {
			if d, ok := license.Data[k].(map[string]interface{}); ok {
				if exp, ok := d["expiration_time"].(string); ok {
					t, e := time.Parse(time.RFC3339, exp)
					active = active || (e == nil && t.After(time.Now()))
				}
			}
		}
	}
	if !active {
		return fmt.Errorf("vault must report an active Enterprise license at sys/license/status; install a valid license on the existing deployment")
	}
	return nil
}

// Check the owned mount's live API before generating any CA keys.
func pqcCheckSchema(client *vaultapi.Client, mount string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	resp, err := client.Logical().ReadRawWithDataWithContext(ctx, mount+"/root/generate/internal", map[string][]string{"help": {"1"}})
	if err != nil {
		return fmt.Errorf("read PKI algorithm schema: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var data map[string]interface{}
	if err := resp.DecodeJSON(&data); err != nil {
		return err
	}
	raw, _ := json.Marshal(data)
	if !bytes.Contains(raw, []byte("ml-dsa-65")) {
		return fmt.Errorf("running PKI route does not advertise ML-DSA-65 parameter_set; use a PQC-capable Vault build")
	}
	return nil
}

func pqcStateDir() (string, error) { return global.PQCPKIStateDir() }
func pqcExec(input []byte, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if global.Debug {
		fmt.Printf("[DEBUG] PQC exec: %s %s (stdin omitted)\n", name, strings.Join(args, " "))
	}
	cmd := exec.CommandContext(ctx, name, args...)
	if input != nil {
		cmd.Stdin = bytes.NewReader(input)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w (%s)", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}
func pqcKube(input []byte, args ...string) ([]byte, error) {
	return pqcExec(input, "kubectl", append([]string{"--context", "kind-kind", "--request-timeout=15s"}, args...)...)
}
func pqcOwnedMount(m *vaultapi.MountOutput) bool {
	return m != nil && m.Type == "pki" && m.Description == pqcMountDescription
}
func pqcCheckOwnership(client *vaultapi.Client) error {
	dir, e := pqcStateDir()
	if e != nil {
		return e
	}
	if _, e = os.Stat(dir); e == nil {
		owner, err := os.ReadFile(filepath.Join(dir, "owner"))
		if err != nil || strings.TrimSpace(string(owner)) != pqcOwner {
			return fmt.Errorf("refusing to mutate or adopt unowned local PQC state: %s", dir)
		}
	} else if !os.IsNotExist(e) {
		return e
	}

	mounts, err := client.Sys().ListMounts()
	if err != nil {
		return err
	}
	for _, m := range []string{pqcRootMount, pqcIntMount} {
		if entry := mounts[m+"/"]; entry != nil && !pqcOwnedMount(entry) {
			return fmt.Errorf("refusing to adopt or delete unowned Vault mount %s", m)
		}
	}
	if halKindClusterRunning() {
		out, e := pqcKube(nil, "get", "namespace", pqcNamespace, "--ignore-not-found", "-o", "json")
		if e != nil {
			return e
		}
		if len(bytes.TrimSpace(out)) > 0 {
			var ns struct {
				Metadata struct{ Labels map[string]string }
			}
			if e = json.Unmarshal(out, &ns); e != nil {
				return e
			}
			if ns.Metadata.Labels["app.kubernetes.io/managed-by"] != pqcOwner {
				return fmt.Errorf("refusing to adopt or delete unowned namespace %s", pqcNamespace)
			}
		}
	}
	return nil
}
func pqcWrite(client *vaultapi.Client, path string, data map[string]interface{}) (map[string]interface{}, error) {
	s, e := client.Logical().Write(path, data)
	if e != nil {
		return nil, fmt.Errorf("vault %s: %w", path, e)
	}
	if s == nil {
		return nil, nil
	}
	return s.Data, nil
}
func pqcCA(client *vaultapi.Client, mount string) (string, error) {
	s, e := client.Logical().Read(mount + "/cert/ca")
	if e != nil {
		var response *vaultapi.ResponseError
		if errors.As(e, &response) && response.StatusCode == 400 && len(response.Errors) == 1 && response.Errors[0] == "no default issuer currently configured" {
			return "", nil
		}
		return "", e
	}
	if s == nil {
		return "", nil
	}
	v, _ := s.Data["certificate"].(string)
	return v, nil
}
func pqcSave(dir, name, value string) error {
	return os.WriteFile(filepath.Join(dir, name), []byte(value+"\n"), 0600)
}

func pqcIssue(client *vaultapi.Client, dir string) (err error) {
	mounts, err := client.Sys().ListMounts()
	if err != nil {
		return err
	}
	// Roll back only mounts created by this attempt, preserving existing CAs.
	var created []string
	defer func() {
		if err == nil {
			return
		}
		for i := len(created) - 1; i >= 0; i-- {
			if cleanupErr := client.Sys().Unmount(created[i]); cleanupErr != nil {
				err = errors.Join(err, fmt.Errorf("remove newly created mount %s: %w", created[i], cleanupErr))
			}
		}
	}()
	for _, m := range []string{pqcRootMount, pqcIntMount} {
		if existing := mounts[m+"/"]; existing != nil {
			if !pqcOwnedMount(existing) {
				return fmt.Errorf("refusing to adopt unowned mount %s", m)
			}
		} else {
			if err = client.Sys().Mount(m, &vaultapi.MountInput{Type: "pki", Description: pqcMountDescription, Config: vaultapi.MountConfigInput{MaxLeaseTTL: "8760h"}}); err != nil {
				return err
			}
			created = append(created, m)
		}
	}
	for _, m := range []string{pqcRootMount, pqcIntMount} {
		if err = pqcCheckSchema(client, m); err != nil {
			return err
		}
	}
	root, err := pqcCA(client, pqcRootMount)
	if err != nil {
		return err
	}
	if root == "" {
		d, e := pqcWrite(client, pqcRootMount+"/root/generate/internal", map[string]interface{}{"common_name": "HAL ML-DSA Root", "key_type": "ml-dsa", "parameter_set": "ml-dsa-65", "ttl": "8760h"})
		if e != nil {
			return e
		}
		root, _ = d["certificate"].(string)
	}
	inter, err := pqcCA(client, pqcIntMount)
	if err != nil {
		return err
	}
	if inter == "" {
		d, e := pqcWrite(client, pqcIntMount+"/intermediate/generate/internal", map[string]interface{}{"common_name": "HAL ML-DSA Intermediate", "key_type": "ml-dsa", "parameter_set": "ml-dsa-65"})
		if e != nil {
			return e
		}
		signed, e := pqcWrite(client, pqcRootMount+"/root/sign-intermediate", map[string]interface{}{"csr": d["csr"], "common_name": "HAL ML-DSA Intermediate", "ttl": "4380h", "max_path_length": 0})
		if e != nil {
			return e
		}
		inter, _ = signed["certificate"].(string)
		if _, e = pqcWrite(client, pqcIntMount+"/intermediate/set-signed", map[string]interface{}{"certificate": inter}); e != nil {
			return e
		}
	}
	if err = pqcSave(dir, "root.pem", root); err != nil {
		return err
	}
	if err = pqcSave(dir, "intermediate.pem", inter); err != nil {
		return err
	}
	dns := []string{pqcBackend, pqcBackend + "." + pqcNamespace, pqcBackend + "." + pqcNamespace + ".svc", pqcBackend + "." + pqcNamespace + ".svc." + pqcDomain}
	for _, name := range []string{pqcFrontend, pqcBackend} {
		names := dns
		if name == pqcFrontend {
			names = []string{pqcFrontend + "." + pqcNamespace + ".svc." + pqcDomain}
		}
		_, e := pqcWrite(client, pqcIntMount+"/roles/"+name, map[string]interface{}{"key_type": "ml-dsa", "parameter_set": "ml-dsa-65", "allowed_domains": names, "allow_bare_domains": true, "allow_subdomains": false, "client_flag": name == pqcFrontend, "server_flag": name == pqcBackend, "key_usage": []string{"DigitalSignature"}, "not_before_duration": "1s", "no_store": true, "ttl": pqcCertTTL, "max_ttl": pqcCertTTL})
		if e != nil {
			return e
		}
		d, e := pqcWrite(client, pqcIntMount+"/issue/"+name, map[string]interface{}{"common_name": names[0], "alt_names": strings.Join(names, ","), "ttl": pqcCertTTL, "private_key_format": "pkcs8"})
		if e != nil {
			return e
		}
		cert, _ := d["certificate"].(string)
		key, _ := d["private_key"].(string)
		if cert == "" || key == "" {
			return fmt.Errorf("vault did not return %s leaf credentials", name)
		}
		if e = pqcSave(dir, name+".pem", cert); e != nil {
			return e
		}
		if e = pqcSave(dir, name+"-chain.pem", cert+"\n"+inter); e != nil {
			return e
		}
		if e = pqcSave(dir, name+".key", key); e != nil {
			return e
		}
	}
	return nil
}

// Local leaves are diagnostic snapshots, not the identities served by pods.
// Verify them within their signed validity interval so startup latency cannot
// consume a ten-second lifetime. Live NGINX verification still uses wall time.
func pqcLeafVerifyArgs(dir, leaf string) ([]string, error) {
	path := filepath.Join(dir, leaf+".pem")
	out, err := pqcExec(nil, "openssl", "x509", "-in", path, "-noout", "-enddate")
	if err != nil {
		return nil, err
	}
	end, err := time.Parse("Jan _2 15:04:05 2006 MST", strings.TrimPrefix(strings.TrimSpace(string(out)), "notAfter="))
	if err != nil {
		return nil, fmt.Errorf("read %s diagnostic certificate expiry: %w", leaf, err)
	}
	args := []string{"verify", "-attime", strconv.FormatInt(end.Unix()-1, 10), "-CAfile", filepath.Join(dir, "root.pem"), "-untrusted", filepath.Join(dir, "intermediate.pem"), "-purpose", "sslclient"}
	if leaf == pqcBackend {
		args[len(args)-1] = "sslserver"
		args = append(args, "-verify_hostname", pqcBackend+"."+pqcNamespace+".svc."+pqcDomain)
	}
	return append(args, path), nil
}

func pqcBrowserCert(dir string) error {
	if raw, err := os.ReadFile(filepath.Join(dir, "browser.pem")); err == nil {
		if block, _ := pem.Decode(raw); block != nil {
			if cert, e := x509.ParseCertificate(block.Bytes); e == nil && cert.VerifyHostname(pqcBrowserDNS) == nil && cert.NotAfter.After(time.Now().Add(24*time.Hour)) {
				if _, e = os.Stat(filepath.Join(dir, "browser.key")); e == nil {
					return nil
				}
			}
		}
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}
	cert := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: pqcBrowserDNS}, DNSNames: []string{pqcBrowserDNS, "localhost"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(30 * 24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		return err
	}
	raw, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(dir, "browser.pem"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "browser.key"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: raw}), 0600)
}

// pqcPrepareImage reuses a distribution image; HAL no longer compiles TLS code.
func pqcPrepareImage(engine string) (string, error) {
	if _, err := pqcExec(nil, engine, "image", "inspect", "--format", "{{.Id}}", pqcImage); err != nil {
		fmt.Println("Pulling stock NGINX PQC image...")
		if _, err = pqcExec(nil, engine, "pull", pqcImage); err != nil {
			return "", err
		}
	}
	out, err := pqcExec(nil, engine, "run", "--rm", "--network", "none", "--entrypoint", "nginx", pqcImage, "-V")
	if err != nil {
		return "", fmt.Errorf("PQC image must provide NGINX with OpenSSL and bundled njs: %w", err)
	}
	nginxVersion := regexp.MustCompile(`nginx/([0-9.]+)`).FindStringSubmatch(string(out))
	opensslVersion := regexp.MustCompile(`OpenSSL ([0-9.]+)`).FindStringSubmatch(string(out))
	if len(nginxVersion) != 2 || !vaultVersionAtLeast(nginxVersion[1], "1.29.3") || len(opensslVersion) != 2 || !vaultVersionAtLeast(opensslVersion[1], "3.5.0") {
		return "", fmt.Errorf("PQC image needs NGINX 1.29.3+ and OpenSSL 3.5+; verify interoperability before selecting an override")
	}
	if _, err = pqcExec(nil, engine, "run", "--rm", "--network", "none", "--entrypoint", "nginx", pqcImage, "-t", "-g", "load_module /usr/lib/nginx/modules/ngx_http_js_module.so;"); err != nil {
		return "", fmt.Errorf("PQC image must include a compatible HTTP njs module: %w", err)
	}
	fmt.Printf("PQC runtime: NGINX %s, OpenSSL %s, bundled HTTP njs (algorithm interoperability is tested separately).\n", nginxVersion[1], opensslVersion[1])
	return pqcImage, nil
}

func pqcApply(obj interface{}) error {
	b, e := json.Marshal(obj)
	if e != nil {
		return e
	}
	_, e = pqcKube(b, "apply", "-f", "-")
	return e
}
func pqcMetadata(name string) map[string]interface{} {
	return map[string]interface{}{"name": name, "namespace": pqcNamespace, "labels": map[string]string{"app.kubernetes.io/managed-by": pqcOwner}}
}
func pqcDeploy(dir, image, proxy string) error {
	if e := pqcApply(map[string]interface{}{"apiVersion": "v1", "kind": "Namespace", "metadata": map[string]interface{}{"name": pqcNamespace, "labels": map[string]string{"app.kubernetes.io/managed-by": pqcOwner}}}); e != nil {
		return e
	}
	for name, files := range map[string]map[string]string{"frontend": {"rotation.json": "frontend-rotation.json"}, "backend": {"rotation.json": "backend-rotation.json"}, "browser": {"tls.crt": "browser.pem", "tls.key": "browser.key"}} {
		data := map[string]string{}
		for k, f := range files {
			b, e := os.ReadFile(filepath.Join(dir, f))
			if e != nil {
				return e
			}
			data[k] = string(b)
		}
		// Server-side apply avoids storing an additional copy of leaf keys in the
		// last-applied-configuration annotation.
		secretName, secretType := name+"-rotation", "Opaque"
		if name == "browser" {
			secretName, secretType = name, "kubernetes.io/tls"
		}
		obj := map[string]interface{}{"apiVersion": "v1", "kind": "Secret", "metadata": pqcMetadata(secretName), "type": secretType, "stringData": data}
		b, _ := json.Marshal(obj)
		if _, e := pqcKube(b, "apply", "--server-side", "--field-manager="+pqcOwner, "-f", "-"); e != nil {
			return e
		}
	}
	root, e := os.ReadFile(filepath.Join(dir, "root.pem"))
	if e != nil {
		return e
	}
	trust := map[string]string{"root.pem": string(root)}
	if ca, err := os.ReadFile(filepath.Join(dir, "vault-ca.pem")); err == nil {
		trust["vault-ca.pem"] = string(ca)
	} else if !os.IsNotExist(err) {
		return err
	}
	if e = pqcApply(map[string]interface{}{"apiVersion": "v1", "kind": "ConfigMap", "metadata": pqcMetadata("trust"), "data": trust}); e != nil {
		return e
	}
	for _, name := range []string{pqcBackend, pqcFrontend} {
		config, err := pqcAssets.ReadFile("pki-pqc/" + name + ".conf")
		if err != nil {
			return err
		}
		config = []byte(pqcRotationConfig(strings.ReplaceAll(string(config), "__CLUSTER_DOMAIN__", pqcDomain), proxy))
		script, err := pqcAssets.ReadFile("pki-pqc/evidence.js")
		if err != nil {
			return err
		}
		rotation, err := pqcAssets.ReadFile("pki-pqc/rotation.js")
		if err != nil {
			return err
		}
		data := map[string]string{"nginx.conf": string(config), "evidence.js": string(script), "rotation.js": string(rotation)}
		if name == pqcFrontend {
			page, err := pqcAssets.ReadFile("pki-pqc/index.html")
			if err != nil {
				return err
			}
			data["index.html"] = string(page)
		}
		if err = pqcApply(map[string]interface{}{"apiVersion": "v1", "kind": "ConfigMap", "metadata": pqcMetadata(name + "-config"), "data": data}); err != nil {
			return err
		}
		configHash := sha256.New()
		configHash.Write(config)
		configHash.Write(script)
		configHash.Write(rotation)
		credentials, err := os.ReadFile(filepath.Join(dir, name+"-rotation.json"))
		if err != nil {
			return err
		}
		configHash.Write(credentials)
		configHash.Write([]byte(data["index.html"]))
		labels := map[string]string{"app": pqcOwner + "-" + name, "app.kubernetes.io/managed-by": pqcOwner}
		volumes := []interface{}{map[string]interface{}{"name": "identity", "secret": map[string]interface{}{"secretName": name + "-rotation", "defaultMode": 0440}}, map[string]interface{}{"name": "trust", "configMap": map[string]string{"name": "trust"}}}
		mounts := []interface{}{map[string]interface{}{"name": "identity", "mountPath": "/identity", "readOnly": true}, map[string]interface{}{"name": "trust", "mountPath": "/trust", "readOnly": true}}
		if name == pqcFrontend {
			volumes = append(volumes, map[string]interface{}{"name": "browser", "secret": map[string]interface{}{"secretName": "browser", "defaultMode": 0440}})
			mounts = append(mounts, map[string]interface{}{"name": "browser", "mountPath": "/browser", "readOnly": true})
		}
		volumes = append(volumes, map[string]interface{}{"name": "app", "configMap": map[string]string{"name": name + "-config"}}, map[string]interface{}{"name": "tmp", "emptyDir": map[string]interface{}{"sizeLimit": "32Mi"}})
		mounts = append(mounts, map[string]interface{}{"name": "app", "mountPath": "/app", "readOnly": true}, map[string]interface{}{"name": "tmp", "mountPath": "/tmp"})
		pod := map[string]interface{}{"automountServiceAccountToken": false, "enableServiceLinks": false, "securityContext": map[string]interface{}{"runAsNonRoot": true, "runAsUser": 65532, "runAsGroup": 65532, "fsGroup": 65532}, "volumes": volumes, "containers": []interface{}{map[string]interface{}{
			"name": name, "image": image, "imagePullPolicy": "IfNotPresent", "command": []string{"nginx"}, "args": []string{"-c", "/app/nginx.conf", "-g", "daemon off;"},
			"ports": []interface{}{map[string]interface{}{"name": "tls", "containerPort": 8443}}, "volumeMounts": mounts,
			"securityContext": map[string]interface{}{"allowPrivilegeEscalation": false, "readOnlyRootFilesystem": true, "capabilities": map[string]interface{}{"drop": []string{"ALL"}}},
			"readinessProbe":  map[string]interface{}{"httpGet": map[string]interface{}{"path": "/health", "port": 8080}, "initialDelaySeconds": 1, "periodSeconds": 1},
			"resources":       map[string]interface{}{"requests": map[string]string{"cpu": "50m", "memory": "32Mi"}, "limits": map[string]string{"memory": "128Mi"}},
		}}}
		deployment := map[string]interface{}{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": pqcMetadata(name), "spec": map[string]interface{}{"replicas": 1, "selector": map[string]interface{}{"matchLabels": map[string]string{"app": labels["app"]}}, "template": map[string]interface{}{"metadata": map[string]interface{}{"labels": labels, "annotations": map[string]string{"hal.pqc-config": fmt.Sprintf("%x", configHash.Sum(nil))}}, "spec": pod}}}
		if e = pqcApply(deployment); e != nil {
			return e
		}
		port := map[string]interface{}{"name": "tls", "port": 8443, "targetPort": 8443}
		kind := "ClusterIP"
		if name == pqcFrontend {
			port["nodePort"] = pqcNodePort
			kind = "NodePort"
		}
		if e = pqcApply(map[string]interface{}{"apiVersion": "v1", "kind": "Service", "metadata": pqcMetadata(name), "spec": map[string]interface{}{"type": kind, "selector": map[string]string{"app": labels["app"]}, "ports": []interface{}{port}}}); e != nil {
			return e
		}
	}
	for _, name := range []string{pqcBackend, pqcFrontend} {
		if _, e = pqcKube(nil, "rollout", "status", "deployment/"+name, "-n", pqcNamespace, "--timeout=120s"); e != nil {
			return e
		}
	}
	// Remove the previous static leaf Secrets only after both rotating workloads are ready.
	_, e = pqcKube(nil, "delete", "secret", pqcFrontend, pqcBackend, "-n", pqcNamespace, "--ignore-not-found")
	return e
}

// Inspect the runtime mapping without changing the shared cluster.
func pqcHostPortMapped(engine string) bool {
	out, err := pqcExec(nil, engine, "port", kindControlPlaneName, fmt.Sprintf("%d/tcp", pqcNodePort))
	return err == nil && strings.Contains(string(out), fmt.Sprintf(":%d", pqcHostPort))
}

func pqcStatus(client *vaultapi.Client) error {
	if err := pqcCheckOwnership(client); err != nil {
		return err
	}
	root, e := pqcCA(client, pqcRootMount)
	if e != nil {
		return e
	}
	inter, e := pqcCA(client, pqcIntMount)
	if e != nil {
		return e
	}
	fmt.Printf("PQC PKI: root=%t intermediate=%t (isolated Vault mounts)\n", root != "", inter != "")
	out, err := pqcKube(nil, "get", "deployments", "-n", pqcNamespace, "-l", "app.kubernetes.io/managed-by="+pqcOwner, "-o", "json")
	ready := false
	if err == nil {
		var d struct {
			Items []struct {
				Status struct {
					ReadyReplicas int `json:"readyReplicas"`
				}
			}
		}
		if json.Unmarshal(out, &d) == nil && len(d.Items) == 2 {
			ready = d.Items[0].Status.ReadyReplicas > 0 && d.Items[1].Status.ReadyReplicas > 0
		}
	}
	fmt.Printf("PQC Kubernetes workloads ready: %t (readiness is not cryptographic proof)\n", ready)
	if ready {
		fmt.Printf("Browser: https://%s:%d\n", pqcBrowserDNS, pqcHostPort)
		engine, err := global.DetectEngine()
		if err == nil && pqcHostPortMapped(engine) {
			fmt.Printf("Next Step: open https://%s:%d and run the connection test.\n", pqcBrowserDNS, pqcHostPort)
		} else {
			fmt.Printf("Next Step: kubectl --context kind-kind -n %s port-forward --address 127.0.0.1 service/%s %d:8443\nThen open the browser URL and run the connection test.\n", pqcNamespace, pqcFrontend, pqcHostPort)
		}
	} else {
		fmt.Println("Next Step: hal vault pki enable --pqc")
	}
	return nil
}

func pqcDisable(client *vaultapi.Client) error {
	if err := pqcCheckOwnership(client); err != nil {
		return err
	}
	if halKindClusterRunning() {
		if _, e := pqcKube(nil, "delete", "namespace", pqcNamespace, "--ignore-not-found", "--wait=true", "--timeout=120s"); e != nil {
			return e
		}
	}
	// Preserve the shared cluster: its product/global lifecycle owns final cleanup.
	mounts, e := client.Sys().ListMounts()
	if e != nil {
		return e
	}
	for _, m := range []string{pqcIntMount, pqcRootMount} {
		if mounts[m+"/"] != nil {
			if e = client.Sys().Unmount(m); e != nil {
				return e
			}
		}
	}
	dir, err := pqcStateDir()
	if err != nil {
		return err
	}
	if err := pqcRevokeRotation(client, dir); err != nil {
		return err
	}
	return global.RemovePQCPKIState()
}

func runPQC(cmd *cobra.Command, args []string) error {
	enable, disable, update := pkiEnable, pkiDisable, pkiUpdate
	if e := parseLifecycleAction(args, &enable, &disable, &update); e != nil {
		return e
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
	return runPQCAction(cmd, action)
}

func runPQCAction(cmd *cobra.Command, action string) error {
	if e := validatePQCFlags(cmd, action); e != nil {
		return e
	}
	if global.DryRun {
		fmt.Printf("[DRY RUN] PQC %s: isolated mounts %s/%s, namespace %s; shared KinD; stock NGINX image; browser HTTPS on %d. No keys or resources generated.\n", action, pqcRootMount, pqcIntMount, pqcNamespace, pqcHostPort)
		return nil
	}
	client, e := GetHealthyClient()
	if e != nil {
		if action == "status" {
			fmt.Printf("PQC PKI: down (%v)\nNext Step: hal vault create\n", e)
			return nil
		}
		return e
	}
	if action == "status" {
		return pqcStatus(client)
	}
	if e = pqcCheckOwnership(client); e != nil {
		return e
	}
	if action == "disable" {
		active, err := agilityDependencyActive()
		if err != nil {
			return err
		}
		if active {
			return fmt.Errorf("crypto agility uses the PQC CA; run hal vault pki disable --agility first")
		}
		if e := pqcDisable(client); e != nil {
			return e
		}
		fmt.Println("PQC lab disabled; shared cluster and distribution images preserved.\nNext Step: hal vault pki enable --pqc")
		return nil
	}
	if e = pqcPrerequisites(client); e != nil {
		return e
	}
	for _, tool := range []string{"kind", "kubectl", "openssl"} {
		if _, e = exec.LookPath(tool); e != nil {
			return fmt.Errorf("PQC requires %s: %w", tool, e)
		}
	}
	engine, e := global.DetectEngine()
	if e != nil {
		return e
	}
	if halKindClusterRunning() {
		// Never recreate an existing cluster just to add this lab's browser port.
		if !pqcHostPortMapped(engine) {
			fmt.Printf("Existing shared KinD cluster has no PQC host mapping; preserve it and use:\n  kubectl --context kind-kind -n %s port-forward --address 127.0.0.1 service/%s %d:8443\n", pqcNamespace, pqcFrontend, pqcHostPort)
		}
		out, e := pqcKube(nil, "get", "configmap", "coredns", "-n", "kube-system", "-o", "jsonpath={.data.Corefile}")
		if e != nil {
			return e
		}
		if !strings.Contains(string(out), "kubernetes "+pqcDomain+" ") {
			return fmt.Errorf("--pqc-cluster-domain must match the existing CoreDNS kubernetes zone")
		}
	}
	if !halKindClusterRunning() && pqcDomain != pqcDefaultDomain {
		return fmt.Errorf("new shared HAL clusters use %s; --pqc-cluster-domain overrides are for an existing cluster", pqcDefaultDomain)
	}
	dir, e := pqcStateDir()
	if e != nil {
		return e
	}
	if st, err := os.Stat(dir); err == nil && st.IsDir() {
		b, err := os.ReadFile(filepath.Join(dir, "owner"))
		if err != nil || strings.TrimSpace(string(b)) != pqcOwner {
			return fmt.Errorf("refusing to adopt local state without PQC ownership marker: %s", dir)
		}
	}
	if action == "enable" {
		if _, err := os.Stat(filepath.Join(dir, "deployed")); err == nil && global.PQCPKIReadiness() == "up" {
			fmt.Println("PQC already enabled; use update to reconcile configuration or renew certificates.")
			return pqcStatus(client)
		}
	}
	image, e := pqcPrepareImage(engine)
	if e != nil {
		return e
	}
	if e = os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	if e = os.WriteFile(filepath.Join(dir, "owner"), []byte(pqcOwner), 0600); e != nil {
		return e
	}
	if e = pqcIssue(client, dir); e != nil {
		return e
	}
	// Offline diagnostics are mandatory and precede Kubernetes mutation.
	// CAs must still be valid now, independently of historical leaf diagnostics.
	if _, err := pqcExec(nil, "openssl", "verify", "-CAfile", filepath.Join(dir, "root.pem"), filepath.Join(dir, "root.pem"), filepath.Join(dir, "intermediate.pem")); err != nil {
		return err
	}
	for _, ca := range []string{"root", "intermediate"} {
		out, err := pqcExec(nil, "openssl", "x509", "-in", filepath.Join(dir, ca+".pem"), "-noout", "-text")
		if err != nil {
			return err
		}
		if !bytes.Contains(out, []byte("Public Key Algorithm: ML-DSA-65")) || !bytes.Contains(out, []byte("Signature Algorithm: ML-DSA-65")) {
			return fmt.Errorf("%s CA is not ML-DSA-65", ca)
		}
	}
	for _, leaf := range []string{pqcFrontend, pqcBackend} {
		args, err := pqcLeafVerifyArgs(dir, leaf)
		if err != nil {
			return err
		}
		out, err := pqcExec(nil, "openssl", args...)
		if err != nil {
			return err
		}
		fmt.Print(string(out))
		out, err = pqcExec(nil, "openssl", "x509", "-in", filepath.Join(dir, leaf+".pem"), "-noout", "-text")
		if err != nil {
			return err
		}
		if !bytes.Contains(out, []byte("Public Key Algorithm: ML-DSA-65")) || !bytes.Contains(out, []byte("Signature Algorithm: ML-DSA-65")) {
			return fmt.Errorf("vault %s certificate is not ML-DSA-65", leaf)
		}
	}
	if e = pqcBrowserCert(dir); e != nil {
		return e
	}
	if e = ensureHALKindCluster(engine, pkiKindNodeImage); e != nil {
		return e
	}
	// Let Kubernetes pull the selected registry image. Importing a host-engine
	// archive can leave stale CRI aliases on fresh containerd/KinD nodes.
	proxy, ca, err := pqcVaultProxy(client, engine)
	if err != nil {
		return err
	}
	if len(ca) > 0 {
		if err := os.WriteFile(filepath.Join(dir, "vault-ca.pem"), ca, 0600); err != nil {
			return err
		}
	}
	if e = pqcConfigureRotation(client, dir); e != nil {
		return e
	}
	if e = pqcDeploy(dir, image, proxy); e != nil {
		return e
	}
	if e = os.WriteFile(filepath.Join(dir, "deployed"), []byte(image), 0600); e != nil {
		return e
	}
	fmt.Printf("PQC workloads ready. Browser trust certificate: %s (not installed).\n", filepath.Join(dir, "browser.pem"))
	return pqcStatus(client)
}

func init() {
	original := vaultPKICmd.Run
	vaultPKICmd.Run = nil
	vaultPKICmd.RunE = func(cmd *cobra.Command, args []string) error {
		if pkiAgility {
			return runAgility(cmd, args)
		}
		if pkiPQC {
			return runPQC(cmd, args)
		}
		if cmd.Flags().Changed("agility-image") {
			return fmt.Errorf("--agility-image requires --agility")
		}
		teardown := (pkiEnable || (len(args) > 0 && args[0] == "enable")) && !pkiK8s && !pkiACME ||
			pkiDisable || (len(args) > 0 && args[0] == "disable") ||
			((pkiUpdate || (len(args) > 0 && args[0] == "update")) && ((!pkiK8s && !pkiACME) || pkiForce))
		if teardown && !global.DryRun {
			active, err := agilityDependencyActive()
			if err != nil {
				return err
			}
			if active {
				return fmt.Errorf("crypto agility uses the conventional CA; run hal vault pki disable --agility first")
			}
		}
		for _, f := range []string{"pqc-image", "pqc-cluster-domain", "pqc-cert-ttl"} {
			if cmd.Flags().Changed(f) {
				return fmt.Errorf("--%s requires --pqc", f)
			}
		}
		if pkiRootMount == pqcRootMount || pkiRootMount == pqcIntMount || pkiIntMount == pqcRootMount || pkiIntMount == pqcIntMount {
			return fmt.Errorf("PQC mount names are reserved; use --pqc")
		}
		original(cmd, args)
		return nil
	}
	f := vaultPKICmd.Flags()
	f.BoolVar(&pkiPQC, "pqc", false, "Use isolated ML-DSA-65 mutual TLS and ML-KEM-768 lab (implies Kubernetes)")
	f.StringVar(&pqcCertTTL, "pqc-cert-ttl", pqcDefaultCertTTL, "PQC workload certificate TTL (10s to 24h); automatically rotates at half lifetime")
	f.StringVar(&pqcImage, "pqc-image", pqcDefaultImage, "Stock NGINX image with njs and ML-DSA/ML-KEM-768 TLS support")
	f.StringVar(&pqcDomain, "pqc-cluster-domain", pqcDefaultDomain, "Actual Kubernetes Service DNS domain")
}
