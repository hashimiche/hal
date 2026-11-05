package global

import (
	"fmt"
	"strings"
)

// agentNetworksInspectFormat prints "<id>|<network> <network> ..." per
// container. Like kindNodeInspectFormat, it reads NetworkSettings.Networks with
// `inspect` rather than `ps --format`, so it works on Docker and Podman alike
// and lists the networks of stopped containers too.
const agentNetworksInspectFormat = `{{.Id}}|{{range $k, $v := .NetworkSettings.Networks}}{{$k}} {{end}}`

// ListTFEAgentContainerIDs returns the IDs of the TFE/TFC agent containers HAL
// may remove on engine, running or not.
//
// A container qualifies only if all three hold:
//   - its name or image contains "tfe-agent" or "tfc-agent";
//   - its name does not start with "hal-" (those are removed with the other
//     hal-* containers, by name);
//   - it is attached to hal-net.
//
// The third condition is the ownership test. HAL starts TFE with
// TFE_RUN_PIPELINE_DOCKER_NETWORK=hal-net, so the run containers TFE spawns
// are expected on hal-net, whereas a TFC/TFE agent a user runs for their own
// purposes normally is not, and must survive `hal delete`. The test errs
// towards leaving a container alone: one whose networks cannot be read is not
// returned.
func ListTFEAgentContainerIDs(engine string) ([]string, error) {
	out, err := runner(nil, engine, "ps", "-a", "--format", "{{.ID}} {{.Names}} {{.Image}}")
	if err != nil {
		return nil, fmt.Errorf("%s ps: %w", engine, err)
	}

	candidates := []string{}
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
			candidates = append(candidates, id)
		}
	}
	if len(candidates) == 0 {
		return []string{}, nil
	}

	args := append([]string{"inspect", "--format", agentNetworksInspectFormat}, candidates...)
	out, err = runner(nil, engine, args...)
	ids := agentIDsOnHalNet(string(out))
	if err != nil && len(ids) == 0 {
		return nil, fmt.Errorf("%s inspect: %w", engine, err)
	}
	// With some output, the error is a container that vanished between ps and
	// inspect; the survivors are still accurate.
	return ids, nil
}

// agentIDsOnHalNet keeps the IDs of the agentNetworksInspectFormat lines whose
// networks include hal-net.
func agentIDsOnHalNet(out string) []string {
	ids := []string{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "|", 2)
		if len(parts) != 2 || parts[0] == "" {
			continue
		}
		for _, network := range strings.Fields(parts[1]) {
			if network == HalNetName {
				ids = append(ids, parts[0])
				break
			}
		}
	}
	return ids
}
