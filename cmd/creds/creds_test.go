package creds

import (
	"encoding/json"
	"strings"
	"testing"

	"hal/internal/labsecrets"
)

// The MCP credentials tool documents data.{any_active,services[]} with the
// scenarios of each product nested under "scenarios", so the JSON shape of
// ActiveCredentials is part of that contract.
func TestActiveCredentialsJSONShape(t *testing.T) {
	active := ActiveCredentials{
		AnyActive: true,
		Services: []labsecrets.Section{{
			Key:     labsecrets.SectionVault,
			Label:   "Vault",
			URL:     "http://vault.localhost:8200",
			Entries: []labsecrets.Entry{{Name: "Root token", Secret: labsecrets.VaultDevRootToken}},
			Sections: []labsecrets.Section{{
				Key:     "vault-userpass",
				Label:   "UserPass — demo user",
				Entries: []labsecrets.Entry{{Username: labsecrets.VaultUserPassUsername, Secret: labsecrets.VaultUserPassPassword}},
			}},
		}},
	}

	out, err := json.Marshal(active)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	for _, want := range []string{
		`"any_active":true`,
		`"services":[`,
		`"service":"vault"`,
		`"scenarios":[`,
		`"service":"vault-userpass"`,
		`"secret":"` + labsecrets.VaultUserPassPassword + `"`,
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("missing %s in %s", want, out)
		}
	}
}

func TestBoundarySectionRendersAdminLogin(t *testing.T) {
	boundary := labsecrets.Section{
		Key:   "boundary",
		Icon:  "🔒",
		Label: "Boundary",
		URL:   boundaryURL,
		Entries: []labsecrets.Entry{
			{Name: "Admin", Username: "admin", Secret: "password"},
			{Username: "ssh-operator", Secret: "password", Note: "Boundary auth method: ssh-lab-auth"},
		},
	}

	var out strings.Builder
	if !labsecrets.RenderSection(&out, labsecrets.Tree{Sections: []labsecrets.Section{boundary}}, "boundary") {
		t.Fatal("boundary section was not rendered")
	}

	for _, want := range []string{"🔒 Boundary", boundaryURL, "Admin : admin", "ssh-operator / password"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out.String())
		}
	}
}
