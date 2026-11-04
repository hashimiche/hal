package vault

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"hal/internal/global"
)

func TestAgilityFlagsAndDryRun(t *testing.T) {
	oldAgility, oldPQC, oldImage, oldDry := pkiAgility, pkiPQC, agilityImage, global.DryRun
	t.Cleanup(func() { pkiAgility, pkiPQC, agilityImage, global.DryRun = oldAgility, oldPQC, oldImage, oldDry })
	pkiAgility, pkiPQC, agilityImage = true, false, pqcDefaultImage
	for _, flag := range []string{"pqc", "acme", "force", "root-mount", "pqc-image", "hsm"} {
		cmd := pqcFlagCommand()
		if flag == "pqc" {
			pkiPQC = true
		} else {
			cmd.Flags().Lookup(flag).Changed = true
		}
		if err := agilityValidateFlags(cmd); err == nil {
			t.Errorf("accepted incompatible --%s", flag)
		}
		pkiPQC = false
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())
	global.DryRun = true
	if err := runAgility(pqcFlagCommand(), []string{"enable"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".hal")); !os.IsNotExist(err) {
		t.Fatalf("dry run wrote state: %v", err)
	}
}

func TestAgilityConventionalOwnershipAndReuse(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	dir, err := agilityStateDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := agilityMark(dir, agilityConventionalMarker); err != nil {
		t.Fatal(err)
	}
	if owned, err := agilityHasMarker(dir, agilityConventionalMarker); err != nil || !owned {
		t.Fatalf("ownership marker: %t %v", owned, err)
	}
	if err := os.WriteFile(filepath.Join(dir, agilityConventionalMarker), []byte("other-owner"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := agilityHasMarker(dir, agilityConventionalMarker); err == nil {
		t.Fatal("accepted foreign ownership marker")
	}
	if err := os.Remove(filepath.Join(dir, agilityConventionalMarker)); err != nil {
		t.Fatal(err)
	}

	fv, client := newFakeVault(t, map[string]fakeVaultResponse{
		mountsKey:                  ok(`{"data":{"pki-root/":{"type":"pki"},"pki-int/":{"type":"pki"}}}`),
		"GET /v1/pki-root/cert/ca": ok(`{"data":{"certificate":"root-cert"}}`),
		"GET /v1/pki-int/cert/ca":  ok(`{"data":{"certificate":"inter-cert"}}`),
	})
	if err := agilityEnsureConventional(client, dir, ""); err != nil {
		t.Fatal(err)
	}
	if owned, err := agilityHasMarker(dir, agilityConventionalMarker); err != nil || owned {
		t.Fatalf("reused CA was claimed: %t %v", owned, err)
	}
	for _, call := range fv.calls {
		if !strings.HasPrefix(call.Key, "GET ") {
			t.Errorf("reuse mutated Vault: %s", call.Key)
		}
	}
}

func TestAgilityRejectsPartialUnownedConventionalPKI(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	_, client := newFakeVault(t, map[string]fakeVaultResponse{
		mountsKey: ok(`{"data":{"pki-root/":{"type":"pki"}}}`),
	})
	dir, err := agilityStateDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := agilityEnsureConventional(client, dir, ""); err == nil {
		t.Fatal("accepted partial unowned CA")
	}
	if owned, err := agilityHasMarker(dir, agilityConventionalMarker); err != nil || owned {
		t.Fatalf("partial CA was claimed: %t %v", owned, err)
	}
}

func TestAgilityBootstrapsConventionalPKI(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	dir, err := agilityStateDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	fv, client := newFakeVault(t, map[string]fakeVaultResponse{
		"PUT /v1/pki-root/root/generate/internal":        ok(`{"data":{"certificate":"root-cert"}}`),
		"PUT /v1/pki-int/intermediate/generate/internal": ok(`{"data":{"csr":"test-csr"}}`),
		"PUT /v1/pki-root/root/sign-intermediate":        ok(`{"data":{"certificate":"inter-cert"}}`),
		"GET /v1/pki-root/cert/ca":                       ok(`{"data":{"certificate":"root-cert"}}`),
		"GET /v1/pki-int/cert/ca":                        ok(`{"data":{"certificate":"inter-cert"}}`),
	})
	fv.seq[mountsKey] = []fakeVaultResponse{
		ok(`{"data":{}}`),
		ok(`{"data":{"pki-root/":{"type":"pki"},"pki-int/":{"type":"pki"}}}`),
	}
	if err := agilityEnsureConventional(client, dir, ""); err != nil {
		t.Fatal(err)
	}
	if owned, err := agilityHasMarker(dir, agilityConventionalMarker); err != nil || !owned {
		t.Fatalf("created CA not owned: %t %v", owned, err)
	}
	if pending, err := agilityHasMarker(dir, agilityConventionalPending); err != nil || pending {
		t.Fatalf("completed CA still pending: %t %v", pending, err)
	}
	for _, file := range []string{"traditional-root.pem", "traditional-intermediate.pem"} {
		if _, err := os.Stat(filepath.Join(dir, file)); err != nil {
			t.Fatalf("missing %s: %v", file, err)
		}
	}
	for _, key := range []string{"PUT /v1/pki-root/root/generate/internal", "PUT /v1/pki-int/intermediate/generate/internal", "PUT /v1/pki-root/root/sign-intermediate"} {
		if !slices.Contains(fv.keys(), key) {
			t.Errorf("bootstrap skipped %s", key)
		}
	}
}

func TestAgilityRemovesOnlyOwnedConventionalPKI(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	dir, err := agilityStateDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	fv, client := newFakeVault(t, map[string]fakeVaultResponse{
		mountsKey: ok(`{"data":{"pki-root/":{"type":"pki"},"pki-int/":{"type":"pki"}}}`),
		"GET /v1/sys/policies/acl/hal-pki-issuer": ok(fmt.Sprintf(`{"data":{"policy":%q}}`, pkiIssuerPolicy())),
	})
	if err := agilityRemoveConventional(client, dir, ""); err != nil {
		t.Fatal(err)
	}
	for _, call := range fv.calls {
		if strings.HasPrefix(call.Key, "DELETE ") {
			t.Fatalf("removed reused PKI: %s", call.Key)
		}
	}
	if err := agilityMark(dir, agilityConventionalMarker); err != nil {
		t.Fatal(err)
	}
	if err := agilityRemoveConventional(client, dir, ""); err != nil {
		t.Fatal(err)
	}
	var deletes []string
	for _, call := range fv.calls {
		if strings.HasPrefix(call.Key, "DELETE ") {
			deletes = append(deletes, call.Key)
		}
	}
	want := []string{"DELETE /v1/sys/mounts/pki-int", "DELETE /v1/sys/mounts/pki-root", "DELETE /v1/sys/policies/acl/hal-pki-issuer"}
	if strings.Join(deletes, ",") != strings.Join(want, ",") {
		t.Fatalf("cleanup = %v, want %v", deletes, want)
	}
}

func TestAgilityRefusesChangedConventionalCA(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	dir, err := agilityStateDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := agilityMark(dir, agilityConventionalMarker); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "traditional-root.pem"), []byte("original-root\n"), 0600); err != nil {
		t.Fatal(err)
	}
	fv, client := newFakeVault(t, map[string]fakeVaultResponse{
		mountsKey:                  ok(`{"data":{"pki-root/":{"type":"pki"},"pki-int/":{"type":"pki"}}}`),
		"GET /v1/pki-root/cert/ca": ok(`{"data":{"certificate":"replacement-root"}}`),
	})
	if err := agilityRemoveConventional(client, dir, ""); err == nil {
		t.Fatal("removed changed CA")
	}
	for _, call := range fv.calls {
		if strings.HasPrefix(call.Key, "DELETE ") {
			t.Fatalf("changed CA cleanup mutated Vault: %s", call.Key)
		}
	}
}

func TestAgilitySkipsPQCNotCreatedAfterFailedEnable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	dir, err := agilityStateDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := agilityMark(dir, agilityPQCMarker); err != nil {
		t.Fatal(err)
	}
	fv, client := newFakeVault(t, map[string]fakeVaultResponse{mountsKey: ok(`{"data":{}}`)})
	if err := agilityRemovePQC(client, dir); err != nil {
		t.Fatal(err)
	}
	for _, call := range fv.calls {
		if strings.HasPrefix(call.Key, "DELETE ") {
			t.Fatalf("failed enable deleted unrelated PQC resources: %s", call.Key)
		}
	}
}

func TestAgilityUpdateDelegatesPQCEnable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	_, client := newFakeVault(t, map[string]fakeVaultResponse{mountsKey: ok(`{"data":{}}`)})
	dir, err := agilityStateDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	oldEnable, oldDisable, oldUpdate, oldDry := pkiEnable, pkiDisable, pkiUpdate, global.DryRun
	pkiEnable, pkiDisable, pkiUpdate, global.DryRun = false, false, true, true
	t.Cleanup(func() {
		pkiEnable, pkiDisable, pkiUpdate, global.DryRun = oldEnable, oldDisable, oldUpdate, oldDry
	})
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = writer
	err = agilityEnsurePQC(client, dir)
	os.Stdout = stdout
	_ = writer.Close()
	output, readErr := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || readErr != nil {
		t.Fatalf("PQC delegation: %v, capture: %v", err, readErr)
	}
	if !strings.Contains(string(output), "[DRY RUN] PQC enable:") {
		t.Fatalf("delegated wrong PQC action: %s", output)
	}
	if !pkiUpdate || pkiEnable || pkiDisable {
		t.Fatal("delegation changed outer lifecycle flags")
	}
	if owned, err := agilityHasMarker(dir, agilityPQCMarker); err != nil || !owned {
		t.Fatalf("new PQC lab was not marked as owned: %t %v", owned, err)
	}
}

func TestAgilityClusterCleanupPreservesOtherNamespaces(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	deleteLog := filepath.Join(t.TempDir(), "deleted")
	namespaces := filepath.Join(t.TempDir(), "namespaces.json")
	t.Setenv("HAL_TEST_CLUSTER_DELETE", deleteLog)
	t.Setenv("HAL_TEST_CLUSTER_NAMESPACES", namespaces)
	for name, script := range map[string]string{
		"kind":    "#!/bin/sh\nif [ \"$1\" = get ]; then echo kind; exit 0; fi\nif [ \"$1\" = delete ]; then echo deleted >> \"$HAL_TEST_CLUSTER_DELETE\"; exit 0; fi\nexit 1\n",
		"kubectl": "#!/bin/sh\n/bin/cat \"$HAL_TEST_CLUSTER_NAMESPACES\"\n",
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	dir, err := agilityStateDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := agilityMark(dir, agilityClusterMarker); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(namespaces, []byte(`{"items":[{"metadata":{"name":"default"}},{"metadata":{"name":"app1"}}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := agilityRemoveCluster(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(deleteLog); !os.IsNotExist(err) {
		t.Fatalf("deleted shared cluster: %v", err)
	}
	if err := os.WriteFile(namespaces, []byte(`{"items":[{"metadata":{"name":"default"}},{"metadata":{"name":"kube-system"}}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := agilityRemoveCluster(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(deleteLog); err != nil {
		t.Fatalf("did not delete owned exclusive cluster: %v", err)
	}
}

func TestAgilityOwnershipRejectsRoleAdoption(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir()) // no KinD; role ownership is checked read-only
	fv, client := newFakeVault(t, map[string]fakeVaultResponse{
		mountsKey:                              ok(`{"data":{"pki-int/":{"type":"pki"},"hal-pqc-int/":{"type":"pki"}}}`),
		"GET /v1/pki-int/roles/" + agilityRole: ok(`{"data":{"key_type":"rsa"}}`),
	})
	dir, err := agilityStateDir()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agilityCheckOwnership(client, dir); err == nil {
		t.Fatal("adopted existing role without marker")
	}
	for _, call := range fv.calls {
		if !strings.HasPrefix(call.Key, "GET ") {
			t.Errorf("ownership check mutated Vault: %s", call.Key)
		}
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agilityOwnerFile(dir), []byte(agilityOwner), 0600); err != nil {
		t.Fatal(err)
	}
	if owned, err := agilityCheckOwnership(client, dir); err != nil || !owned {
		t.Fatalf("owned state rejected: %t %v", owned, err)
	}
	if active, err := agilityDependencyActive(); err != nil || !active {
		t.Fatalf("CA teardown dependency missed: %t %v", active, err)
	}
	if err := os.WriteFile(agilityOwnerFile(dir), []byte("someone-else"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := agilityDependencyActive(); err == nil {
		t.Fatal("unowned marker did not block CA teardown")
	}
}

func agilityTestCertificate(t *testing.T, dir, file, dns string, eku x509.ExtKeyUsage, isCA bool, parent *x509.Certificate, parentKey *rsa.PrivateKey, extraDNS ...string) (*x509.Certificate, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	cert := &x509.Certificate{
		SerialNumber: big.NewInt(now.UnixNano()), Subject: pkix.Name{CommonName: dns},
		DNSNames: append([]string{dns}, extraDNS...), NotBefore: now.Add(-time.Hour), NotAfter: now.Add(7 * 24 * time.Hour),
		BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{eku},
	}
	if isCA {
		cert.IsCA = true
		cert.KeyUsage = x509.KeyUsageCertSign | x509.KeyUsageCRLSign
		cert.ExtKeyUsage = nil
		cert.DNSNames = nil
	}
	if parent == nil {
		parent, parentKey = cert, key
	}
	der, err := x509.CreateCertificate(rand.Reader, cert, parent, &key.PublicKey, parentKey)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, file+".pem"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	return parsed, key
}

func TestAgilityLeafSANAndEKUValidation(t *testing.T) {
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Skip("OpenSSL required")
	}
	dir := t.TempDir()
	root, rootKey := agilityTestCertificate(t, dir, "traditional-root", "Root", x509.ExtKeyUsageServerAuth, true, nil, nil)
	inter, interKey := agilityTestCertificate(t, dir, "traditional-intermediate", "Intermediate", x509.ExtKeyUsageServerAuth, true, root, rootKey)
	mode := agilityModes()[0]
	agilityTestCertificate(t, dir, mode.name, mode.dns, x509.ExtKeyUsageServerAuth, false, inter, interKey)
	if err := agilityValidateLeaf(dir, mode); err != nil {
		t.Fatalf("valid browser leaf rejected: %v", err)
	}
	agilityTestCertificate(t, dir, "other-root", "Other", x509.ExtKeyUsageServerAuth, true, nil, nil)
	original, err := os.ReadFile(filepath.Join(dir, "traditional-root.pem"))
	if err != nil {
		t.Fatal(err)
	}
	other, err := os.ReadFile(filepath.Join(dir, "other-root.pem"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "traditional-root.pem"), other, 0600); err != nil {
		t.Fatal(err)
	}
	if err := agilityValidateLeaf(dir, mode); err == nil {
		t.Fatal("accepted wrong trust root")
	}
	if err := os.WriteFile(filepath.Join(dir, "traditional-root.pem"), original, 0600); err != nil {
		t.Fatal(err)
	}
	agilityTestCertificate(t, dir, mode.name, "wrong.agility.localhost", x509.ExtKeyUsageServerAuth, false, inter, interKey)
	if err := agilityValidateLeaf(dir, mode); err == nil {
		t.Fatal("accepted wrong DNS SAN")
	}
	agilityTestCertificate(t, dir, mode.name, mode.dns, x509.ExtKeyUsageServerAuth, false, inter, interKey, "extra.agility.localhost")
	if err := agilityValidateLeaf(dir, mode); err == nil {
		t.Fatal("accepted extra DNS SAN")
	}
	agilityTestCertificate(t, dir, mode.name, mode.dns, x509.ExtKeyUsageClientAuth, false, inter, interKey)
	if err := agilityValidateLeaf(dir, mode); err == nil {
		t.Fatal("accepted clientAuth-only leaf")
	}
}

func TestAgilityModeRoutingAndEvidenceConfig(t *testing.T) {
	wantModes := map[string]struct {
		group, algorithm, dns             string
		containerPort, nodePort, hostPort int
	}{
		"traditional": {"X25519", "RSA", agilityTraditionalDNS, 8443, agilityNodePort, agilityHostPort},
		"hybrid":      {"X25519MLKEM768", "RSA", agilityHybridDNS, 8444, agilityHybridNodePort, agilityHybridHostPort},
		"pqc":         {"X25519MLKEM768", "ML-DSA-65", agilityPQCDNS, 8445, agilityPQCNodePort, agilityPQCHostPort},
		"pure":        {"MLKEM768", "ML-DSA-65", agilityPureDNS, 8446, agilityPureNodePort, agilityPureHostPort},
	}
	modes := agilityModes()
	if len(modes) != len(wantModes) {
		t.Fatalf("got %d agility modes, want %d", len(modes), len(wantModes))
	}
	dir := t.TempDir()
	seen := map[string]bool{}
	for _, mode := range modes {
		want, ok := wantModes[mode.name]
		if !ok || seen[mode.name] {
			t.Fatalf("unexpected or duplicate agility mode %q", mode.name)
		}
		seen[mode.name] = true
		if mode.group != want.group || mode.algorithm != want.algorithm || mode.dns != want.dns ||
			mode.containerPort != want.containerPort || mode.nodePort != want.nodePort || mode.hostPort != want.hostPort {
			t.Errorf("%s has unexpected certificate, group, DNS, or port: %+v", mode.name, mode)
		}
		agilityTestCertificate(t, dir, mode.name, mode.dns, x509.ExtKeyUsageServerAuth, false, nil, nil)
	}
	config, err := agilityNginxConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range agilityModes() {
		for _, want := range []string{
			"listen " + stringPort(mode.containerPort) + " ssl;", "server_name " + mode.dns + ";",
			"ssl_conf_command Groups " + mode.group + ";", "ssl_session_tickets off;",
			"location = /benchmark", "HAL crypto agility benchmark v1",
			"default_type application/javascript;", "default_type application/json;",
		} {
			if !strings.Contains(config, want) {
				t.Errorf("%s missing %q", mode.name, want)
			}
		}
	}
	page, err := agilityAssets.ReadFile("pki-agility/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), "id=\"performance-tab\"") ||
		!strings.Contains(string(page), "id=\"measure-browser\"") {
		t.Error("performance tab or browser measurement control missing")
	}
	if _, err := agilityAssets.ReadFile("pki-agility/performance.js"); err != nil {
		t.Fatal(err)
	}
	for _, mode := range agilityModes() {
		if !strings.Contains(string(page), "https://"+mode.dns+":"+stringPort(mode.hostPort)+"/") {
			t.Errorf("missing navigation to %s", mode.name)
		}
	}
	if !strings.Contains(string(page), "Experimental") || !strings.Contains(string(page), "likely") {
		t.Error("pure PQC browser warning missing")
	}
	script, err := agilityAssets.ReadFile("pki-agility/diagnostics.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ssl_curve", "ssl_sigalg", "server_policy_match", "browser_trust"} {
		if !strings.Contains(string(script), want) {
			t.Errorf("diagnostics missing %s", want)
		}
	}
}

func TestAgilityServedChainSize(t *testing.T) {
	dir := t.TempDir()
	root, rootKey := agilityTestCertificate(t, dir, "root", "Root", x509.ExtKeyUsageServerAuth, true, nil, nil)
	inter, interKey := agilityTestCertificate(t, dir, "intermediate", "Intermediate", x509.ExtKeyUsageServerAuth, true, root, rootKey)
	leaf, _ := agilityTestCertificate(t, dir, "leaf", "traditional.agility.localhost", x509.ExtKeyUsageServerAuth, false, inter, interKey)
	leafPEM, err := os.ReadFile(filepath.Join(dir, "leaf.pem"))
	if err != nil {
		t.Fatal(err)
	}
	interPEM, err := os.ReadFile(filepath.Join(dir, "intermediate.pem"))
	if err != nil {
		t.Fatal(err)
	}
	chain := append(leafPEM, interPEM...)
	got, err := agilityChainDERBytes(chain)
	if err != nil || got != len(leaf.Raw)+len(inter.Raw) {
		t.Fatalf("served DER chain size = %d, %v", got, err)
	}
	if _, err := agilityChainDERBytes(leafPEM); err == nil {
		t.Fatal("accepted a chain without an intermediate")
	}
}

func TestAgilityKindPortMappings(t *testing.T) {
	path, err := writeHALKindConfig()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Remove(path); err != nil {
			t.Errorf("remove KinD config: %v", err)
		}
	})
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	config := string(raw)
	for _, mode := range agilityModes() {
		want := "containerPort: " + stringPort(mode.nodePort) + "\n      hostPort: " + stringPort(mode.hostPort) + "\n      listenAddress: 127.0.0.1"
		if !strings.Contains(config, want) {
			t.Errorf("missing loopback KinD mapping for %s", mode.name)
		}
	}
}

func TestAgilityPortForwardParsing(t *testing.T) {
	local, remote, ok := agilityForwardPort("Forwarding from 127.0.0.1:41235 -> 8446")
	if !ok || local != 41235 || remote != 8446 {
		t.Fatalf("bad port-forward mapping: %d %d %t", local, remote, ok)
	}
	if _, _, ok := agilityForwardPort("Forwarding from 0.0.0.0:41235 -> 8444"); ok {
		t.Fatal("accepted non-loopback port-forward")
	}
}

func TestAgilityNegotiatedGroup(t *testing.T) {
	for _, tc := range []struct {
		name, output, want string
	}{
		{"traditional OpenSSL 3.6", "Peer Temp Key: X25519, 253 bits\n", "X25519"},
		{"hybrid OpenSSL 3.6", "Negotiated TLS1.3 group: X25519MLKEM768\n", "X25519MLKEM768"},
		{"pure PQC OpenSSL 3.6", "Negotiated TLS1.3 group: MLKEM768\n", "MLKEM768"},
		{"wrong group", "Peer Temp Key: X25519MLKEM768, 1216 bits\n", "X25519MLKEM768"},
		{"missing group", "Verification: OK\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := agilityNegotiatedGroup([]byte(tc.output)); got != tc.want {
				t.Fatalf("negotiated group = %q, want %q", got, tc.want)
			}
		})
	}
}

func stringPort(port int) string { return fmt.Sprint(port) }
