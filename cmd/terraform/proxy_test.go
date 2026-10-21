package terraform

import (
	"fmt"
	"strings"
	"testing"
)

// TFE builds absolute URLs from TFE_HOSTNAME, which is portless, so they point at
// :443 — not published on the host. The state viewer fetches
// /api/state-versions/<id>/hosted_json_state, a Rails URL outside /_archivist/, so
// rewriting archivist links alone leaves every state unreadable in the browser.
func TestRenderTFEProxyVhostRewritesEveryPortlessURL(t *testing.T) {
	for _, v := range []tfeProxyVhost{
		primaryTFEProxyVhost(),
		twinTFEProxyVhost("hal-tfe-bis", "tfe-bis.localhost", 9443),
	} {
		conf := renderTFEProxyVhost(v)

		want := fmt.Sprintf("sub_filter 'https://%s/' 'https://%s:%d/';", v.ServerName, v.ServerName, v.HTTPSPort)
		if !strings.Contains(conf, want) {
			t.Errorf("%s vhost: missing host-wide rewrite %q", v.Name, want)
		}
		if strings.Contains(conf, "/_archivist/'") {
			t.Errorf("%s vhost: archivist-only rewrite should be subsumed by the host-wide one", v.Name)
		}
	}
}
