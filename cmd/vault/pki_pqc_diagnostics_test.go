package vault

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPQCDiagnosticSnapshotSurvivesExpiry(t *testing.T) {
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Skip("openssl is required")
	}
	dir := t.TempDir()
	now := time.Now().Truncate(time.Second)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	write := func(name string, cert, parent *x509.Certificate) {
		t.Helper()
		der, err := x509.CreateCertificate(rand.Reader, cert, parent, &key.PublicKey, key)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".pem"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
			t.Fatal(err)
		}
	}
	root := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Root"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	write("root", root, root)
	intermediate := *root
	intermediate.SerialNumber = big.NewInt(2)
	intermediate.Subject.CommonName = "Intermediate"
	write("intermediate", &intermediate, root)
	for _, name := range []string{pqcFrontend, pqcBackend} {
		t.Run(name, func(t *testing.T) {
			usage := x509.ExtKeyUsageClientAuth
			if name == pqcBackend {
				usage = x509.ExtKeyUsageServerAuth
			}
			leaf := &x509.Certificate{SerialNumber: big.NewInt(3), Subject: pkix.Name{CommonName: name}, NotBefore: now.Add(-70 * time.Second), NotAfter: now.Add(-60 * time.Second), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage}, DNSNames: []string{pqcBackend + "." + pqcNamespace + ".svc." + pqcDomain}}
			write(name, leaf, &intermediate)
			args, err := pqcLeafVerifyArgs(dir, name)
			if err != nil {
				t.Fatal(err)
			}
			if out, err := exec.Command("openssl", args...).CombinedOutput(); err != nil {
				t.Fatalf("expired diagnostic snapshot should verify within its lifetime: %v %s", err, out)
			}
			// Wall-clock verification must continue to reject expired live identities.
			live := append([]string{args[0]}, args[3:]...)
			if out, err := exec.Command("openssl", live...).CombinedOutput(); err == nil || !strings.Contains(string(out), "certificate has expired") {
				t.Fatalf("live expiry check was lost: %v %s", err, out)
			}
			leaf.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning}
			write(name, leaf, &intermediate)
			if _, err := exec.Command("openssl", args...).CombinedOutput(); err == nil {
				t.Fatal("snapshot verification accepted wrong EKU")
			}
			leaf.ExtKeyUsage = []x509.ExtKeyUsage{usage}
			if name == pqcBackend {
				leaf.DNSNames = []string{"wrong.invalid"}
				write(name, leaf, &intermediate)
				if _, err := exec.Command("openssl", args...).CombinedOutput(); err == nil {
					t.Fatal("snapshot verification accepted wrong service DNS")
				}
			}
		})
	}
}
