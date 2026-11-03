package vault

import (
	"os"
	"os/exec"
	"testing"
)

// TestPQCIntegration uses live Vault issuance and temporary stock NGINX containers.
// It never changes the shared Kubernetes cluster. See docs/commands/vault-pki.md.
func TestPQCIntegration(t *testing.T) {
	if os.Getenv("HAL_PQC_INTEGRATION") != "1" {
		t.Skip("set HAL_PQC_INTEGRATION=1 to test against an existing licensed Vault")
	}
	image := os.Getenv("HAL_PQC_TEST_IMAGE")
	if image == "" {
		image = pqcDefaultImage
	}
	engine := os.Getenv("HAL_PQC_TEST_ENGINE")
	if engine == "" {
		engine = "podman"
	}
	cmd := exec.Command("python3", "testdata/pki_pqc_integration.py", "--state", t.TempDir(), "--image", image, "--engine", engine)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
}
