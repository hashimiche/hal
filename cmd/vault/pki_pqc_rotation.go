package vault

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	vaultapi "github.com/hashicorp/vault/api"
)

func pqcRotationPolicy(name string) string { return pqcOwner + "-" + name }

// Only issuance for this workload and self-renewal enter the pod. Accessors,
// retained locally, allow disable to revoke every token from interrupted updates.
func pqcConfigureRotation(client *vaultapi.Client, dir string) error {
	ttl, err := time.ParseDuration(pqcCertTTL)
	if err != nil {
		return err
	}
	for _, name := range []string{pqcFrontend, pqcBackend} {
		policy := fmt.Sprintf(`path "%s/issue/%s" { capabilities = ["update"] }
path "auth/token/renew-self" { capabilities = ["update"] }`, pqcIntMount, name)
		if err := client.Sys().PutPolicy(pqcRotationPolicy(name), policy); err != nil {
			return err
		}
		token, err := client.Auth().Token().CreateOrphan(&vaultapi.TokenCreateRequest{
			Policies: []string{pqcRotationPolicy(name)}, NoDefaultPolicy: true,
			Period: "1h", Renewable: boolPointer(true), DisplayName: pqcRotationPolicy(name),
		})
		if err != nil {
			return err
		}
		if token == nil || token.Auth == nil || token.Auth.ClientToken == "" || token.Auth.Accessor == "" {
			return fmt.Errorf("vault did not return a PQC issuance token")
		}
		path := filepath.Join(dir, "rotation-accessors")
		file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			_ = client.Auth().Token().RevokeAccessor(token.Auth.Accessor)
			return err
		}
		_, writeErr := fmt.Fprintln(file, token.Auth.Accessor)
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			_ = client.Auth().Token().RevokeAccessor(token.Auth.Accessor)
			return fmt.Errorf("save rotation token accessor: %v %v", writeErr, closeErr)
		}
		names := []string{name + "." + pqcNamespace + ".svc." + pqcDomain}
		if name == pqcBackend {
			names = append([]string{name, name + "." + pqcNamespace, name + "." + pqcNamespace + ".svc"}, names...)
		}
		data, err := json.Marshal(map[string]interface{}{
			"token": token.Auth.ClientToken, "issue_path": pqcIntMount + "/issue/" + name, "ttl_ms": ttl.Milliseconds(),
			"issue": map[string]interface{}{"common_name": names[0], "alt_names": strings.Join(names, ","), "ttl": pqcCertTTL, "private_key_format": "pkcs8"},
		})
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, name+"-rotation.json"), data, 0600); err != nil {
			return err
		}
	}
	return nil
}

func boolPointer(value bool) *bool { return &value }

func pqcRevokeRotation(client *vaultapi.Client, dir string) error {
	data, err := os.ReadFile(filepath.Join(dir, "rotation-accessors"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, accessor := range strings.Fields(string(data)) {
		if err := client.Auth().Token().RevokeAccessor(accessor); err != nil {
			var response *vaultapi.ResponseError
			alreadyExpired := errors.As(err, &response) && response.StatusCode == 400 && len(response.Errors) == 1 && response.Errors[0] == "invalid accessor"
			if !alreadyExpired {
				return err
			}
		}
	}
	for _, name := range []string{pqcFrontend, pqcBackend} {
		if err := client.Sys().DeletePolicy(pqcRotationPolicy(name)); err != nil {
			return err
		}
	}
	return nil
}

// The loopback proxy gives ngx.fetch verified HTTPS with an explicit server
// name, including HAL's production listener whose certificate names hal-vault.
func pqcVaultProxy(client *vaultapi.Client, engine string) (string, []byte, error) {
	endpoint, err := url.Parse(client.Address())
	if err != nil {
		return "", nil, err
	}
	host := endpoint.Hostname()
	serverName := host
	if host == "localhost" || host == "vault.localhost" || (net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()) {
		host = containerIPOnHalNetOrName(engine, vaultContainer)
		serverName = vaultContainer
	}
	port := endpoint.Port()
	if port == "" {
		if endpoint.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	target := endpoint.Scheme + "://" + net.JoinHostPort(host, port)
	tls := ""
	var ca []byte
	if endpoint.Scheme == "https" {
		caPath := os.Getenv("VAULT_CACERT")
		if caPath == "" {
			caPath = vaultProdCertPath()
		}
		ca, err = os.ReadFile(caPath)
		if err != nil {
			return "", nil, fmt.Errorf("PQC rotation requires Vault CA PEM: %w", err)
		}
		tls = fmt.Sprintf("proxy_ssl_verify on; proxy_ssl_verify_depth 4; proxy_ssl_server_name on; proxy_ssl_name %s; proxy_ssl_trusted_certificate /trust/vault-ca.pem;", serverName)
	}
	// URL values originate in configuration, but must never inject NGINX syntax.
	for _, value := range []string{target, serverName} {
		if strings.ContainsAny(value, "\"'{};$\\\n\r\t ") {
			return "", nil, fmt.Errorf("invalid Vault address for PQC rotation")
		}
	}
	return fmt.Sprintf(`server { listen 127.0.0.1:8081; location /v1/ { proxy_pass %s; %s proxy_connect_timeout 1s; proxy_read_timeout 2s; proxy_send_timeout 2s; } }`, target, tls), ca, nil
}

func pqcRotationConfig(config, proxy string) string {
	config = strings.Replace(config, "http {", `http {
  js_import rotation from /app/rotation.js;
  js_shared_dict_zone zone=identities:1m timeout=24h;
  js_set $identity_bundle rotation.bundle;
  js_set $identity_cert rotation.cert;
  js_set $identity_key rotation.key;
  js_fetch_timeout 2s;
  js_fetch_max_response_buffer_size 256k;
  server { listen 8080; location = /health { js_content rotation.health; } location @rotate { js_periodic rotation.rotate interval=1s; } }
  `+proxy, 1)
	config = strings.ReplaceAll(config, "ssl_certificate /identity/tls.crt;", "ssl_certificate data:$identity_cert;")
	config = strings.ReplaceAll(config, "ssl_certificate_key /identity/tls.key;", "ssl_certificate_key data:$identity_key;")
	return config
}
