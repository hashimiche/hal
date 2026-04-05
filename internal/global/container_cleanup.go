package global

import (
	"fmt"
	"os/exec"
	"strings"
)

func ListTFEAgentContainerIDs(engine string) ([]string, error) {
	out, err := exec.Command(engine, "ps", "-a", "--format", "{{.ID}} {{.Names}} {{.Image}}").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}

	ids := []string{}
	seen := map[string]struct{}{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 3 {
			continue
		}
		id := parts[0]
		name := strings.ToLower(parts[1])
		image := strings.ToLower(strings.Join(parts[2:], " "))

		if strings.HasPrefix(name, "hal-") {
			continue
		}

		if strings.Contains(name, "tfe-agent") || strings.Contains(name, "tfc-agent") || strings.Contains(image, "tfe-agent") || strings.Contains(image, "tfc-agent") {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}

	return ids, nil
}