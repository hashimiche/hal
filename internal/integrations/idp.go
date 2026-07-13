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
