package global

import "os/exec"

// KubeContext is the kubeconfig context of HAL's KinD cluster. kind names the
// context "kind-<cluster>", and HAL's cluster is always KindClusterName.
//
// HAL never relies on the kubeconfig's current-context: a learner who runs
// `kubectl config use-context` for another cluster, or `kind create cluster
// --name other`, would otherwise have HAL read, create and delete namespaces,
// cluster role bindings and Helm releases in that cluster.
const KubeContext = "kind-" + KindClusterName

// Kubectl returns a kubectl command pinned to HAL's KinD cluster. Every kubectl
// call in HAL goes through it, so no call can reach another cluster. The guard
// test in kubectl_test.go fails on a raw exec.Command("kubectl", ...).
func Kubectl(args ...string) *exec.Cmd {
	return exec.Command("kubectl", append([]string{"--context", KubeContext}, args...)...)
}

// Helm returns a helm command pinned to HAL's KinD cluster, like Kubectl.
// `helm repo add` and `helm repo update` go through it too: they only touch the
// local repository cache, and helm ignores --kube-context for them.
func Helm(args ...string) *exec.Cmd {
	return exec.Command("helm", append([]string{"--kube-context", KubeContext}, args...)...)
}
