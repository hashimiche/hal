package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNegotiateProtocolVersion(t *testing.T) {
	cases := []struct {
		name      string
		requested string
		fallback  string
		want      string
	}{
		{name: "supported latest is echoed", requested: "2025-11-25", fallback: mcpProtocolVersionStdio, want: "2025-11-25"},
		{name: "supported legacy is echoed", requested: "2024-11-05", fallback: mcpProtocolVersionStreamableHTTP, want: "2024-11-05"},
		{name: "unknown falls to latest supported", requested: "1999-01-01", fallback: mcpProtocolVersionStdio, want: mcpProtocolVersionLatest},
		{name: "empty uses transport fallback", requested: "", fallback: mcpProtocolVersionStdio, want: mcpProtocolVersionStdio},
		{name: "whitespace uses transport fallback", requested: "   ", fallback: mcpProtocolVersionStreamableHTTP, want: mcpProtocolVersionStreamableHTTP},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := negotiateProtocolVersion(tc.requested, tc.fallback); got != tc.want {
				t.Fatalf("negotiateProtocolVersion(%q,%q)=%q want %q", tc.requested, tc.fallback, got, tc.want)
			}
		})
	}
}

func TestIsAllowedOrigin(t *testing.T) {
	cases := []struct {
		origin string
		want   bool
	}{
		{origin: "http://localhost:3000", want: true},
		{origin: "http://127.0.0.1:8080", want: true},
		{origin: "https://app.localhost", want: true},
		{origin: "http://[::1]:9090", want: true},
		{origin: "https://evil.example.com", want: false},
		{origin: "not-a-url", want: false},
	}
	for _, tc := range cases {
		if got := isAllowedOrigin(tc.origin); got != tc.want {
			t.Fatalf("isAllowedOrigin(%q)=%v want %v", tc.origin, got, tc.want)
		}
	}
}

func TestIsAllowedOriginRespectsEnvAllowlist(t *testing.T) {
	t.Setenv("HAL_MCP_ALLOWED_ORIGINS", "https://trusted.example.com, https://also.example.com")
	if !isAllowedOrigin("https://trusted.example.com") {
		t.Fatalf("expected env-allowlisted origin to be allowed")
	}
	if isAllowedOrigin("https://untrusted.example.com") {
		t.Fatalf("expected non-allowlisted origin to be rejected")
	}
}

func postInitialize(t *testing.T, requestedVersion string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	body := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params":  map[string]interface{}{"protocolVersion": requestedVersion},
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	handleStreamableHTTPRPC(rec, req)
	return rec
}

func TestStreamableHTTPInitializeNegotiatesVersion(t *testing.T) {
	rec := postInitialize(t, "2025-11-25", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Result.ProtocolVersion != "2025-11-25" {
		t.Fatalf("expected negotiated 2025-11-25, got %q", resp.Result.ProtocolVersion)
	}
}

func TestStreamableHTTPAllowsNoOriginNoVersionHeader(t *testing.T) {
	rec := postInitialize(t, "2024-11-05", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for headerless request, got %d", rec.Code)
	}
}

func TestStreamableHTTPRejectsDisallowedOrigin(t *testing.T) {
	rec := postInitialize(t, "2025-11-25", map[string]string{"Origin": "https://evil.example.com"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for disallowed origin, got %d", rec.Code)
	}
}

func TestStreamableHTTPAllowsLoopbackOrigin(t *testing.T) {
	rec := postInitialize(t, "2025-11-25", map[string]string{"Origin": "http://localhost:5173"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for loopback origin, got %d", rec.Code)
	}
}

func TestStreamableHTTPRejectsUnsupportedProtocolHeader(t *testing.T) {
	rec := postInitialize(t, "2025-11-25", map[string]string{"MCP-Protocol-Version": "1999-01-01"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unsupported MCP-Protocol-Version, got %d", rec.Code)
	}
}

func TestStreamableHTTPAcceptsSupportedProtocolHeader(t *testing.T) {
	rec := postInitialize(t, "2025-11-25", map[string]string{"MCP-Protocol-Version": "2025-11-25"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for supported MCP-Protocol-Version, got %d", rec.Code)
	}
}
