package integrations

import "fmt"

type ProviderEndpoints struct {
	Issuer       string
	JWKSURL      string
	DiscoveryURL string
}

func GitLabCE(baseURL string) ProviderEndpoints {
	return ProviderEndpoints{
		Issuer:       baseURL,
		JWKSURL:      fmt.Sprintf("%s/oauth/discovery/keys", baseURL),
		DiscoveryURL: fmt.Sprintf("%s/.well-known/openid-configuration", baseURL),
	}
}

func KeycloakRealm(baseURL, realm string) ProviderEndpoints {
	realmURL := fmt.Sprintf("%s/realms/%s", baseURL, realm)
	return ProviderEndpoints{
		Issuer:       realmURL,
		JWKSURL:      fmt.Sprintf("%s/protocol/openid-connect/certs", realmURL),
		DiscoveryURL: fmt.Sprintf("%s/.well-known/openid-configuration", realmURL),
	}
}
