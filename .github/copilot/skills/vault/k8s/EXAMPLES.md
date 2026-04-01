# Example Transcripts

## Example 1: Enable Native K8s Auth

User: Wire Vault auth to my local Kubernetes setup.

Assistant:

    hal vault k8s --enable

Assistant: I verify auth config and role mapping:

    vault read auth/kubernetes/config
    vault read auth/kubernetes/role/app1-role

## Example 2: Enable CSI Mode

User: I want CSI mode.

Assistant:

    hal vault k8s --enable --csi

Assistant: If Vault Enterprise is not present, I explain fallback to standard mode.

## Example 3: Full Reset

User: Recreate the k8s demo from scratch.

Assistant:

    hal vault k8s --force
