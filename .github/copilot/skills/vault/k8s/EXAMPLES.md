# Example Transcripts

## Example 1: Enable Native K8s Auth

User: Wire Vault auth to my local Kubernetes setup.

Assistant:

    hal vault k8s --enable
    hal vault k8s

Assistant: I verify auth config and role mapping:

    vault read auth/kubernetes/config
    vault read auth/kubernetes/role/app1-role

## Example 2: Enable CSI Mode

User: I want CSI mode.

Assistant:

    hal vault k8s --enable --csi
    hal vault k8s

Assistant: If Vault Enterprise is not present, I explain fallback to standard mode.

Assistant: If Vault Enterprise is present, I confirm CSI resources exist:

    kubectl get csisecrets hal-csi-secrets -n app1

## Example 3: Full Reset

User: Recreate the k8s demo from scratch.

Assistant:

    hal vault k8s --force
