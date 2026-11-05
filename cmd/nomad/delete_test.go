package nomad

import (
	"io"
	"os"
	"strings"
	"testing"

	"hal/internal/global"
)

func TestDeleteDryRunNamesTheScopedPurge(t *testing.T) {
	old := global.DryRun
	global.DryRun = true
	t.Cleanup(func() { global.DryRun = old })

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w
	destroyCmd.Run(destroyCmd, nil)
	os.Stdout = stdout
	_ = w.Close()
	b, _ := io.ReadAll(r)
	out := string(b)

	if !strings.Contains(out, "multipass delete --purge hal-nomad") {
		t.Errorf("dry-run does not show the real command:\n%s", out)
	}
	if strings.Contains(out, "multipass purge") {
		t.Errorf("dry-run mentions a global purge:\n%s", out)
	}
}
