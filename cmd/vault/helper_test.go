package vault

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVaultProdEndpointRespondingRequiresTLS(t *testing.T) {
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer tlsServer.Close()

	if !vaultProdEndpointResponding(tlsServer.URL) {
		t.Fatal("expected responding TLS endpoint to be detected even when Vault is sealed")
	}

	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer httpServer.Close()

	wrongSchemeURL := "https://" + strings.TrimPrefix(httpServer.URL, "http://")
	if vaultProdEndpointResponding(wrongSchemeURL) {
		t.Fatal("plain HTTP endpoint must not be classified as production TLS")
	}
}
