package global

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestKubectlAndHelmArePinnedToHALCluster(t *testing.T) {
	if KubeContext != "kind-kind" {
		t.Fatalf("KubeContext = %q, want kind-kind", KubeContext)
	}

	k := Kubectl("delete", "namespace", "app1", "--ignore-not-found")
	if want := []string{"kubectl", "--context", "kind-kind", "delete", "namespace", "app1", "--ignore-not-found"}; !reflect.DeepEqual(k.Args, want) {
		t.Errorf("Kubectl args = %q, want %q", k.Args, want)
	}

	h := Helm("uninstall", "cert-manager", "-n", "cert-manager")
	if want := []string{"helm", "--kube-context", "kind-kind", "uninstall", "cert-manager", "-n", "cert-manager"}; !reflect.DeepEqual(h.Args, want) {
		t.Errorf("Helm args = %q, want %q", h.Args, want)
	}
}

// TestNoUnpinnedKubectlOrHelm walks the module source and fails on any
// exec.Command or exec.CommandContext whose program is "kubectl" or "helm" and
// whose next argument is not a context flag. Without it, the next kubectl call
// written with exec.Command would act on whatever cluster the learner's
// current-context points to.
func TestNoUnpinnedKubectlOrHelm(t *testing.T) {
	root := filepath.Join("..", "..")
	contextFlag := map[string]string{"kubectl": "--context", "helm": "--kube-context"}

	var found []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		// The helpers are the one place that may name the programs.
		if filepath.Base(path) == "kubectl.go" && filepath.Base(filepath.Dir(path)) == "global" {
			return nil
		}

		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "exec" || (sel.Sel.Name != "Command" && sel.Sel.Name != "CommandContext") {
				return true
			}
			args := call.Args
			if sel.Sel.Name == "CommandContext" {
				if len(args) == 0 {
					return true
				}
				args = args[1:]
			}
			prog, ok := stringLit(args, 0)
			if !ok {
				return true
			}
			flag, watched := contextFlag[prog]
			if !watched {
				return true
			}
			if next, ok := stringLit(args, 1); ok && next == flag {
				return true
			}
			found = append(found, fset.Position(call.Pos()).String())
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, pos := range found {
		t.Errorf("%s: kubectl/helm run without HAL's context, use global.Kubectl or global.Helm", pos)
	}
}

func stringLit(args []ast.Expr, i int) (string, bool) {
	if i >= len(args) {
		return "", false
	}
	lit, ok := args[i].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}
