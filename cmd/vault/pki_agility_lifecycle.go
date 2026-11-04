package vault

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	vaultapi "github.com/hashicorp/vault/api"
	"github.com/spf13/cobra"
	"hal/internal/global"
)

// These markers are written before provisioning. A failed enable can be retried
// or disabled without claiming CA hierarchies that were already present.
const (
	agilityConventionalMarker  = "created-conventional"
	agilityConventionalPending = "rebuilding-conventional"
	agilityPQCMarker           = "created-pqc"
	agilityClusterMarker       = "created-cluster"
)

func agilityHasMarker(dir, name string) (bool, error) {
	b, err := os.ReadFile(filepath.Join(dir, name))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(string(b)) != agilityOwner {
		return false, fmt.Errorf("refusing invalid crypto agility ownership marker %s", name)
	}
	return true, nil
}

func agilityMark(dir, name string) error {
	return os.WriteFile(filepath.Join(dir, name), []byte(agilityOwner+"\n"), 0600)
}

func agilityConventionalCertsUnchanged(client *vaultapi.Client, dir string) error {
	pending, err := agilityHasMarker(dir, agilityConventionalPending)
	if err != nil || pending {
		return err
	}
	mounts, err := client.Sys().ListMounts()
	if err != nil {
		return err
	}
	for _, pair := range [][2]string{{pkiRootMount, "traditional-root.pem"}, {pkiIntMount, "traditional-intermediate.pem"}} {
		if mounts[pair[0]+"/"] == nil {
			continue
		}
		saved, err := os.ReadFile(filepath.Join(dir, pair[1]))
		if os.IsNotExist(err) {
			continue // a failed enable may not have reached certificate export
		}
		if err != nil {
			return err
		}
		live, err := pqcCA(client, pair[0])
		if err != nil {
			return err
		}
		if strings.TrimSpace(string(saved)) != strings.TrimSpace(live) {
			return fmt.Errorf("refusing to remove or adopt changed conventional CA at %s", pair[0])
		}
	}
	return nil
}

func agilityRecordConventionalCAs(client *vaultapi.Client, dir string) error {
	for _, pair := range [][2]string{{pkiRootMount, "traditional-root.pem"}, {pkiIntMount, "traditional-intermediate.pem"}} {
		ca, err := pqcCA(client, pair[0])
		if err != nil {
			return err
		}
		if ca == "" {
			return fmt.Errorf("conventional CA %s is missing", pair[0])
		}
		if err := pqcSave(dir, pair[1], ca); err != nil {
			return err
		}
	}
	return os.Remove(filepath.Join(dir, agilityConventionalPending))
}

// Return whether the two CA mounts can be reused, without ever generating a
// new root on a pre-existing mount. A previously owned partial setup may be
// rebuilt by the conventional setup path.
func agilityConventionalReady(client *vaultapi.Client, owned bool) (bool, error) {
	mounts, err := client.Sys().ListMounts()
	if err != nil {
		return false, err
	}
	root, inter := mounts[pkiRootMount+"/"], mounts[pkiIntMount+"/"]
	if root == nil && inter == nil {
		return false, nil
	}
	if (root != nil && root.Type != "pki") || (inter != nil && inter.Type != "pki") {
		return false, fmt.Errorf("refusing non-PKI conventional mount %s or %s", pkiRootMount, pkiIntMount)
	}
	if root != nil && inter != nil && root.Type == "pki" && inter.Type == "pki" {
		rootCA, err := pqcCA(client, pkiRootMount)
		if err != nil {
			return false, err
		}
		intCA, err := pqcCA(client, pkiIntMount)
		if err != nil {
			return false, err
		}
		if rootCA != "" && intCA != "" {
			return true, nil
		}
	}
	if owned {
		return false, nil
	}
	return false, fmt.Errorf("refusing incomplete or non-PKI conventional mounts %s/%s; resolve them before enabling agility", pkiRootMount, pkiIntMount)
}

func agilityEnsureConventional(client *vaultapi.Client, dir, engine string) error {
	owned, err := agilityHasMarker(dir, agilityConventionalMarker)
	if err != nil {
		return err
	}
	ready, err := agilityConventionalReady(client, owned)
	if err != nil || ready {
		if err == nil && owned && ready {
			pending, markerErr := agilityHasMarker(dir, agilityConventionalPending)
			if markerErr != nil {
				return markerErr
			}
			if pending {
				return agilityRecordConventionalCAs(client, dir)
			}
			return agilityConventionalCertsUnchanged(client, dir)
		}
		return err
	}
	if !owned {
		policy, err := client.Sys().GetPolicy("hal-pki-issuer")
		if err != nil {
			return err
		}
		if policy != "" {
			return fmt.Errorf("refusing to replace existing hal-pki-issuer policy; enable conventional PKI separately")
		}
		if hsmManagedKeyActive(client) {
			return fmt.Errorf("refusing to claim an existing SoftHSM managed key; enable conventional PKI separately")
		}
		if isVaultHSMBuild(client) && exec.Command(engine, "exec", vaultContainer, "test", "-d", "/vault/data/softhsm").Run() == nil {
			return fmt.Errorf("refusing to claim existing SoftHSM token data; enable conventional PKI separately")
		}
		if err := agilityMark(dir, agilityConventionalMarker); err != nil {
			return err
		}
	}
	if err := agilityMark(dir, agilityConventionalPending); err != nil {
		return err
	}
	fmt.Println("Setting up conventional Vault PKI for crypto agility...")
	if isVaultHSMBuild(client) {
		runVaultPKIHSMSetup(client, engine, owned)
	} else {
		runVaultPKISetup(client, owned)
	}
	ready, err = agilityConventionalReady(client, true)
	if err != nil {
		return err
	}
	if !ready {
		return fmt.Errorf("conventional PKI setup did not produce both CA hierarchies; fix the reported error and retry enable or run disable --agility")
	}
	return agilityRecordConventionalCAs(client, dir)
}

func agilityEnsurePQC(client *vaultapi.Client, dir string) error {
	owned, err := agilityHasMarker(dir, agilityPQCMarker)
	if err != nil {
		return err
	}
	mounts, err := client.Sys().ListMounts()
	if err != nil {
		return err
	}
	pqcDir, err := pqcStateDir()
	if err != nil {
		return err
	}
	_, stateErr := os.Stat(pqcDir)
	if stateErr != nil && !os.IsNotExist(stateErr) {
		return stateErr
	}
	if !owned && mounts[pqcRootMount+"/"] == nil && mounts[pqcIntMount+"/"] == nil && os.IsNotExist(stateErr) {
		if err := agilityMark(dir, agilityPQCMarker); err != nil {
			return err
		}
	}
	fmt.Println("Enabling PQC PKI workload lab for crypto agility...")
	return runPQCAction(&cobra.Command{}, "enable")
}

func agilityRemoveConventional(client *vaultapi.Client, dir, engine string) error {
	owned, err := agilityHasMarker(dir, agilityConventionalMarker)
	if err != nil || !owned {
		return err
	}
	mounts, err := client.Sys().ListMounts()
	if err != nil {
		return err
	}
	if err := agilityConventionalCertsUnchanged(client, dir); err != nil {
		return err
	}
	policy, err := client.Sys().GetPolicy("hal-pki-issuer")
	if err != nil {
		return err
	}
	if policy != "" && strings.Join(strings.Fields(policy), " ") != strings.Join(strings.Fields(pkiIssuerPolicy()), " ") {
		return fmt.Errorf("refusing to remove changed hal-pki-issuer policy")
	}
	for _, mount := range []string{pkiIntMount, pkiRootMount} {
		if m := mounts[mount+"/"]; m != nil {
			if m.Type != "pki" {
				return fmt.Errorf("refusing to remove changed conventional mount %s", mount)
			}
			if err := client.Sys().Unmount(mount); err != nil {
				return err
			}
		}
	}
	if policy != "" {
		if err := client.Sys().DeletePolicy("hal-pki-issuer"); err != nil {
			return err
		}
	}
	if isVaultHSMBuild(client) {
		teardownSoftHSMLayer(client, engine, false)
	}
	fmt.Println("Removed conventional PKI created by crypto agility.")
	return nil
}

func agilityCheckConventionalConsumers(dir string) error {
	owned, err := agilityHasMarker(dir, agilityConventionalMarker)
	if err != nil || !owned || !halKindClusterRunning() {
		return err
	}
	for _, namespace := range []string{"pki-demo", "pki-acme-demo"} {
		out, err := pqcKube(nil, "get", "namespace", namespace, "--ignore-not-found", "-o", "name")
		if err != nil {
			return err
		}
		if strings.TrimSpace(string(out)) != "" {
			return fmt.Errorf("conventional PKI created by agility is still used by %s; disable that integration first", namespace)
		}
	}
	return nil
}

func agilityRemovePQC(client *vaultapi.Client, dir string) error {
	owned, err := agilityHasMarker(dir, agilityPQCMarker)
	if err != nil || !owned {
		return err
	}
	mounts, err := client.Sys().ListMounts()
	if err != nil {
		return err
	}
	pqcDir, err := pqcStateDir()
	if err != nil {
		return err
	}
	_, stateErr := os.Stat(pqcDir)
	if stateErr != nil && !os.IsNotExist(stateErr) {
		return stateErr
	}
	if mounts[pqcRootMount+"/"] == nil && mounts[pqcIntMount+"/"] == nil && os.IsNotExist(stateErr) {
		ns, err := agilityPQCNamespaceExists()
		if err != nil {
			return err
		}
		if !ns {
			return nil // enable failed before PQC created any resources
		}
	}
	if err := pqcDisable(client); err != nil {
		return err
	}
	fmt.Println("Removed PQC PKI workload lab created by crypto agility.")
	return nil
}

func agilityPQCNamespaceExists() (bool, error) {
	if !halKindClusterRunning() {
		return false, nil
	}
	out, err := pqcKube(nil, "get", "namespace", pqcNamespace, "--ignore-not-found", "-o", "name")
	return strings.TrimSpace(string(out)) != "", err
}

// The cluster is shared with other Vault features. Delete it only when this
// run created it and no other user namespace remains.
func agilityRemoveCluster(dir string) error {
	owned, err := agilityHasMarker(dir, agilityClusterMarker)
	if err != nil || !owned || !halKindClusterRunning() {
		return err
	}
	out, err := pqcKube(nil, "get", "namespaces", "-o", "json")
	if err != nil {
		return err
	}
	var namespaces struct {
		Items []struct {
			Metadata struct{ Name string }
		}
	}
	if err := json.Unmarshal(out, &namespaces); err != nil {
		return err
	}
	if len(namespaces.Items) == 0 {
		return fmt.Errorf("refusing to delete cluster: namespace inventory is empty")
	}
	for _, item := range namespaces.Items {
		switch item.Metadata.Name {
		case "default", "kube-system", "kube-public", "kube-node-lease", "local-path-storage":
		default:
			fmt.Printf("KinD cluster preserved (namespace %s is still present).\n", item.Metadata.Name)
			return nil
		}
	}
	if out, err := exec.Command("kind", "delete", "cluster", "--name", "kind").CombinedOutput(); err != nil {
		return fmt.Errorf("delete crypto agility KinD cluster: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	fmt.Println("Removed KinD cluster created by crypto agility.")
	return nil
}

func agilityCleanupDependencies(client *vaultapi.Client, dir string) error {
	if err := agilityRemovePQC(client, dir); err != nil {
		return err
	}
	engine := ""
	conventionalOwned, err := agilityHasMarker(dir, agilityConventionalMarker)
	if err != nil {
		return err
	}
	if conventionalOwned && isVaultHSMBuild(client) {
		engine, err = global.DetectEngine()
		if err != nil {
			return err
		}
	}
	if err := agilityRemoveConventional(client, dir, engine); err != nil {
		return err
	}
	return agilityRemoveCluster(dir)
}
