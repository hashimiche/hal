package terraform

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

const (
	tfeTargetPrimary = "primary"
	tfeTargetTwin    = "twin"
	tfeTargetBoth    = "both"
)

var tfeLifecycleTarget string

func normalizeTFETarget(raw string) (string, error) {
	target := strings.ToLower(strings.TrimSpace(raw))
	if target == "" {
		target = tfeTargetPrimary
	}

	switch target {
	case tfeTargetPrimary, tfeTargetTwin, tfeTargetBoth:
		return target, nil
	default:
		return "", fmt.Errorf("invalid --target %q (allowed: primary, twin, both)", raw)
	}
}

func bindTFETargetFlag(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&tfeLifecycleTarget, "target", "t", tfeTargetPrimary, "Terraform scope to act on: primary, twin, or both")
	_ = cmd.RegisterFlagCompletionFunc("target", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{tfeTargetPrimary, tfeTargetTwin, tfeTargetBoth}, cobra.ShellCompDirectiveNoFileComp
	})
}
