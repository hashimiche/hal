package integrations

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"time"
)

func WaitForGitLab(baseURL string, maxRetries int) error {
	client := http.Client{Timeout: 3 * time.Second}
	for i := 0; i < maxRetries; i++ {
		resp, err := client.Get(baseURL + "/users/sign_in")
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			return nil
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(5 * time.Second)
	}
	return fmt.Errorf("timeout")
}

func GitLabPasswordToken(urlStr, username, password string) (string, error) {
	client := http.Client{Timeout: 5 * time.Second}
	for i := 0; i < 5; i++ {
		resp, err := client.PostForm(urlStr, url.Values{
			"grant_type": {"password"},
			"username":   {username},
			"password":   {password},
		})
		if err == nil && resp.StatusCode == 200 {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			var result map[string]interface{}
			_ = json.Unmarshal(body, &result)
			token, ok := result["access_token"].(string)
			if ok && token != "" {
				return token, nil
			}
			return "", fmt.Errorf("missing access token in response")
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(5 * time.Second)
	}
	return "", fmt.Errorf("failed to retrieve gitlab token")
}

func GitLabPost(urlStr, token string, payload map[string]interface{}) ([]byte, error) {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", urlStr, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return body, fmt.Errorf("gitlab api returned status %d", resp.StatusCode)
	}

	return body, nil
}

func GitLabGet(urlStr, token string) ([]byte, error) {
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return body, fmt.Errorf("gitlab api returned status %d", resp.StatusCode)
	}

	return body, nil
}

func EnsureGitLabCE(engine, version, rootPassword string) (bool, error) {
	if out, err := exec.Command(engine, "inspect", "-f", "{{.State.Running}}", "hal-gitlab").Output(); err == nil {
		if string(bytes.TrimSpace(out)) == "true" {
			return true, nil
		}
	}

	args := []string{
		"run", "-d", "--name", "hal-gitlab",
		"--network", "hal-net",
		"--network-alias", "gitlab.localhost",
		"-p", "8080:8080",
		"--shm-size", "256m",
		"--privileged",
		"-e", fmt.Sprintf("GITLAB_OMNIBUS_CONFIG=external_url 'http://gitlab.localhost:8080'; nginx['listen_port'] = 8080; nginx['listen_addresses'] = ['0.0.0.0', '[::]']; puma['port'] = 8081; gitlab_rails['initial_root_password'] = '%s';", rootPassword),
		fmt.Sprintf("gitlab/gitlab-ce:%s", version),
	}

	if out, err := exec.Command(engine, args...).CombinedOutput(); err != nil {
		return false, fmt.Errorf("failed to start GitLab: %s", string(out))
	}

	return false, nil
}
