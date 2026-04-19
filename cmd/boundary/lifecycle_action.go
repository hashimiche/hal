package boundary

import "fmt"

func parseLifecycleAction(args []string, enable, disable, update *bool) error {
	if len(args) == 0 {
		return nil
	}

	action := args[0]
	switch action {
	case "status":
		return nil
	case "enable":
		*enable = true
	case "disable":
		*disable = true
	case "update":
		*update = true
	default:
		return fmt.Errorf("unknown action %q (expected: status, enable, disable, update)", action)
	}

	return nil
}
