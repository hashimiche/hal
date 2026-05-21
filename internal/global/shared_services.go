package global

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type sharedServicesState map[string][]string

func sharedServicesPath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(homeDir, ".hal", "shared-services.json")
}

func loadSharedServicesState() (sharedServicesState, error) {
	path := sharedServicesPath()
	if path == "" {
		return sharedServicesState{}, nil
	}

	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return sharedServicesState{}, nil
		}
		return nil, err
	}

	state := sharedServicesState{}
	if len(body) == 0 {
		return state, nil
	}

	if err := json.Unmarshal(body, &state); err != nil {
		return nil, err
	}

	return state, nil
}

func saveSharedServicesState(state sharedServicesState) error {
	path := sharedServicesPath()
	if path == "" {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	body, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, body, 0o644)
}

func AddSharedServiceConsumer(service, consumer string) error {
	state, err := loadSharedServicesState()
	if err != nil {
		return err
	}

	consumers := state[service]
	for _, c := range consumers {
		if c == consumer {
			return nil
		}
	}
	state[service] = append(consumers, consumer)
	return saveSharedServicesState(state)
}

func RemoveSharedServiceConsumer(service, consumer string) ([]string, error) {
	state, err := loadSharedServicesState()
	if err != nil {
		return nil, err
	}

	consumers := state[service]
	if len(consumers) == 0 {
		return []string{}, nil
	}

	updated := make([]string, 0, len(consumers))
	for _, c := range consumers {
		if c != consumer {
			updated = append(updated, c)
		}
	}

	if len(updated) == 0 {
		delete(state, service)
	} else {
		state[service] = updated
	}

	if err := saveSharedServicesState(state); err != nil {
		return nil, err
	}

	return updated, nil
}

func ClearSharedService(service string) error {
	state, err := loadSharedServicesState()
	if err != nil {
		return err
	}
	delete(state, service)
	return saveSharedServicesState(state)
}

// GetSharedServiceConsumers returns the list of consumers registered for a service key.
// Returns an empty slice when the service has no consumers or the file does not exist.
func GetSharedServiceConsumers(service string) []string {
	state, err := loadSharedServicesState()
	if err != nil {
		return nil
	}
	return state[service]
}

// ResetSharedServicesFile removes ~/.hal/shared-services.json entirely.
// Called by hal delete so the registry does not contain stale consumer entries
// after a full teardown.
func ResetSharedServicesFile() {
	path := sharedServicesPath()
	if path != "" {
		_ = os.Remove(path)
	}
}
