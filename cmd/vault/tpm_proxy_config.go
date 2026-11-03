package vault

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// vaultTPMAppPattern limits the proxy user, role suffix, and socket name to
// a single Linux account name.
var vaultTPMAppPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}$`)

var vaultVersionPattern = regexp.MustCompile(`(\d+)\.(\d+)`)

// vaultTPMRoleName is the auth/tpm role the proxy attests against.
// The proxy config names it "${APP}-app".
func vaultTPMRoleName(app string) string {
	return app + "-app"
}

// vaultTPMProxyConfig renders the Vault Proxy config that enrolls a TPM
// certificate, auto-auths, and serves the API on a Unix socket with that
// token forced in.
//
// vaultAddr is https://127.0.0.1:8200 from the guest's point of view. The
// guest forwards that port to the host Vault. The prod listener certificate
// includes 127.0.0.1, so the proxy can verify it.
func vaultTPMProxyConfig(app, vaultAddr, caCert, swtpmSocket, deviceCN string) string {
	role := vaultTPMRoleName(app)
	return fmt.Sprintf(`vault {
  address = %q
  ca_cert = %q
}

tpm_cert "host_identity" {
  mount_path = "auth/tpm"
  role_name  = %q

  tpm {
    device_path = %q
  }

  renew {
    percent_renew_before_expiry = 20
    min_backoff                 = "5s"
    max_backoff                 = "5m"
  }

  csr {
    CN = %q
  }

  destination {
    path  = %q
    umask = "077"
  }
}

auto_auth {
  method "tpm" {
    mount_path = "auth/tpm"
    config = {
      tpm_cert  = "host_identity"
      role_name = %q
    }
  }
}

api_proxy {
  use_auto_auth_token = "force"
}

listener "unix" {
  address      = %q
  socket_mode  = "600"
  socket_user  = %q
  socket_group = %q
  tls_disable  = true
}
`,
		vaultAddr,
		caCert,
		role,
		swtpmSocket,
		deviceCN,
		vaultTPMCertDir(app),
		role,
		vaultTPMProxySocket(app),
		app,
		app,
	)
}

func vaultTPMCertDir(app string) string {
	return "/var/lib/vault-agent/" + app + "/tpm"
}

// vaultTPMCLIStateDir is where `vault tpm attest` writes the certificate and
// key blobs used by `vault login -method=tpm`. It is separate from the proxy
// certificate directory so the two demos do not share state.
func vaultTPMCLIStateDir(app string) string {
	return "/var/lib/vault-cli/" + app + "/tpm"
}

func vaultTPMTLSDir(app string) string {
	return "/var/lib/vault-agent/" + app + "/tls"
}

func vaultTPMCAPath(app string) string {
	return vaultTPMTLSDir(app) + "/vault-ca.pem"
}

func vaultTPMProxySocket(app string) string {
	return vaultTPMProxySocketDir + "/" + app + ".sock"
}

func vaultTPMProxyConfigPath() string {
	return "/etc/vault-proxy/tpm.hcl"
}

// vaultSupportsTPMAuth reports whether a Vault health version can serve the
// Enterprise TPM auth method added in 2.2.
func vaultSupportsTPMAuth(version string) bool {
	if !strings.Contains(version, "ent") {
		return false
	}
	major, minor, ok := vaultMajorMinor(version)
	if !ok {
		return false
	}
	return major > 2 || (major == 2 && minor >= 2)
}

func vaultMajorMinor(version string) (int, int, bool) {
	m := vaultVersionPattern.FindStringSubmatch(version)
	if m == nil {
		return 0, 0, false
	}
	major, err1 := strconv.Atoi(m[1])
	minor, err2 := strconv.Atoi(m[2])
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return major, minor, true
}

func validateVaultTPMApp(app string) error {
	if !vaultTPMAppPattern.MatchString(app) {
		return fmt.Errorf("app %q must be a lowercase Linux account name", app)
	}
	return nil
}
