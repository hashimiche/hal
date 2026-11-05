package global_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"hal/internal/global"
	"hal/internal/global/globaltest"
)

var (
	// A run container started by HAL's TFE (TFE_RUN_PIPELINE_DOCKER_NETWORK=hal-net).
	halRunAgent = globaltest.Container{ID: "run0000000001", Name: "tfe-agent-abc123", Image: "hashicorp/tfe-agent:now", Networks: []string{"hal-net"}}
	// A user's own agent: matches by image and name, but is not on hal-net.
	userAgent = globaltest.Container{ID: "usr0000000010", Name: "my-tfc-agent", Image: "hashicorp/tfc-agent:latest", Networks: []string{"podman"}}
	// Starts with hal-: removed by name with the other hal-* containers.
	halNamedAgent = globaltest.Container{ID: "hal0000000010", Name: "hal-tfe-agent", Image: "hashicorp/tfc-agent:1.30", Networks: []string{"hal-net"}}
	// On hal-net but not an agent.
	unrelatedOnHalNet = globaltest.Container{ID: "oth0000000001", Name: "grafana-sidecar", Image: "grafana/grafana:11", Networks: []string{"hal-net"}}
)

func TestListTFEAgentContainerIDsReturnsAgentOnHalNet(t *testing.T) {
	globaltest.NewLab(t, []globaltest.Container{halRunAgent}, nil)

	got, err := global.ListTFEAgentContainerIDs("docker")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{halRunAgent.ID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
}

func TestListTFEAgentContainerIDsMatchesByImageAndByName(t *testing.T) {
	byImage := globaltest.Container{ID: "run0000000002", Name: "festive_curie", Image: "docker.io/hashicorp/tfc-agent:1.30", Networks: []string{"hal-net"}}
	byName := globaltest.Container{ID: "run0000000003", Name: "TFE-Agent-Run", Image: "example/run:1", Networks: []string{"podman", "hal-net"}}
	globaltest.NewLab(t, []globaltest.Container{byImage, byName}, nil)

	got, err := global.ListTFEAgentContainerIDs("podman")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{byImage.ID, byName.ID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
}

func TestListTFEAgentContainerIDsLeavesUsersOwnAgentAlone(t *testing.T) {
	globaltest.NewLab(t, []globaltest.Container{userAgent}, nil)

	got, err := global.ListTFEAgentContainerIDs("docker")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("returned a user's own agent that is not on hal-net: %v", got)
	}
}

func TestListTFEAgentContainerIDsLeavesAgentWithNoNetworkAlone(t *testing.T) {
	stopped := globaltest.Container{ID: "usr0000000011", Name: "tfe-agent-old", Image: "hashicorp/tfe-agent:now"}
	globaltest.NewLab(t, []globaltest.Container{stopped}, nil)

	got, err := global.ListTFEAgentContainerIDs("docker")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("returned an agent that is on no network: %v", got)
	}
}

func TestListTFEAgentContainerIDsSkipsHALNamedContainers(t *testing.T) {
	globaltest.NewLab(t, []globaltest.Container{halNamedAgent}, nil)

	got, err := global.ListTFEAgentContainerIDs("docker")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("returned a hal-* container, which the hal-* sweep already removes: %v", got)
	}
}

func TestListTFEAgentContainerIDsIgnoresUnrelatedContainerOnHalNet(t *testing.T) {
	globaltest.NewLab(t, []globaltest.Container{unrelatedOnHalNet}, nil)

	got, err := global.ListTFEAgentContainerIDs("docker")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("returned a container that is not a TFE/TFC agent: %v", got)
	}
}

func TestListTFEAgentContainerIDsMixedLabReturnsOnlyHALsRunContainers(t *testing.T) {
	lab := globaltest.NewLab(t, []globaltest.Container{userAgent, halRunAgent, halNamedAgent, unrelatedOnHalNet}, nil)

	got, err := global.ListTFEAgentContainerIDs("docker")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{halRunAgent.ID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
	// Listing must never remove anything, and must read networks with inspect.
	for _, line := range lab.Lines() {
		if strings.Contains(line, " rm ") || strings.Contains(line, " stop ") {
			t.Errorf("discovery ran a destructive command: %s", line)
		}
	}
	if !lab.Ran("docker inspect --format") {
		t.Errorf("networks were not read with inspect; ran: %v", lab.Lines())
	}
}

func TestListTFEAgentContainerIDsInspectsOnlyCandidates(t *testing.T) {
	lab := globaltest.NewLab(t, []globaltest.Container{userAgent, unrelatedOnHalNet}, nil)

	if _, err := global.ListTFEAgentContainerIDs("docker"); err != nil {
		t.Fatal(err)
	}
	for _, line := range lab.Lines() {
		if strings.Contains(line, unrelatedOnHalNet.ID) {
			t.Errorf("inspected a container that is not an agent candidate: %s", line)
		}
	}
}

func TestListTFEAgentContainerIDsNoCandidatesSkipsInspect(t *testing.T) {
	lab := globaltest.NewLab(t, []globaltest.Container{unrelatedOnHalNet}, nil)

	got, err := global.ListTFEAgentContainerIDs("docker")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("ids = %#v, want an empty non-nil slice", got)
	}
	for _, line := range lab.Lines() {
		if strings.HasPrefix(line, "docker inspect") {
			t.Errorf("ran inspect with no candidate: %s", line)
		}
	}
}

func TestListTFEAgentContainerIDsReturnsEngineErrorFromPs(t *testing.T) {
	globaltest.Install(t, func(c globaltest.Call) ([]byte, error) { return globaltest.Errorf(c) })

	got, err := global.ListTFEAgentContainerIDs("docker")
	if err == nil {
		t.Fatalf("engine failure swallowed, ids = %v", got)
	}
	if len(got) != 0 {
		t.Fatalf("ids = %v on error, want none", got)
	}
}

func TestListTFEAgentContainerIDsReturnsEngineErrorFromInspect(t *testing.T) {
	globaltest.Install(t, func(c globaltest.Call) ([]byte, error) {
		switch {
		case strings.HasPrefix(c.String(), "docker ps"):
			return []byte("run0000000001 tfe-agent-abc123 hashicorp/tfe-agent:now\n"), nil
		case strings.HasPrefix(c.String(), "docker inspect"):
			return nil, fmt.Errorf("inspect failed")
		}
		return globaltest.Errorf(c)
	})

	got, err := global.ListTFEAgentContainerIDs("docker")
	if err == nil {
		t.Fatalf("inspect failure swallowed, ids = %v", got)
	}
	if len(got) != 0 {
		t.Fatalf("returned %v although networks could not be read", got)
	}
}

func TestListTFEAgentContainerIDsKeepsSurvivorsWhenOneVanishesBeforeInspect(t *testing.T) {
	// A run container exits between ps and inspect: inspect prints the other one
	// and fails for the missing one. The survivor is still accurate.
	globaltest.Install(t, func(c globaltest.Call) ([]byte, error) {
		switch {
		case strings.HasPrefix(c.String(), "docker ps"):
			return []byte("run0000000001 tfe-agent-a hashicorp/tfe-agent:now\nrun0000000002 tfe-agent-b hashicorp/tfe-agent:now\n"), nil
		case strings.HasPrefix(c.String(), "docker inspect"):
			return []byte("run0000000002|hal-net \n"), fmt.Errorf("no such container: run0000000001")
		}
		return globaltest.Errorf(c)
	})

	got, err := global.ListTFEAgentContainerIDs("docker")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"run0000000002"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
}
