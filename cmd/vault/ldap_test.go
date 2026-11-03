package vault

import (
	"strings"
	"testing"

	"hal/internal/labsecrets"
)

// The seed LDIF is generated rather than written out, so these assertions stand
// in for reading it: every account the lab documents must exist, with the
// password the catalog says it has.
func TestSeedLDIFHasEveryDocumentedAccount(t *testing.T) {
	ldif := seedLDIF()

	for _, want := range []string{
		"dn: " + labsecrets.LDAPAdminDN,
		"dn: cn=" + labsecrets.LDAPUserAlice + ",ou=users,dc=hal,dc=local",
		"dn: cn=" + labsecrets.LDAPUserBob + ",ou=users,dc=hal,dc=local",
		"dn: cn=app-service,ou=users,dc=hal,dc=local",
		"dn: cn=lib-service-1,ou=users,dc=hal,dc=local",
		"dn: cn=lib-service-2,ou=users,dc=hal,dc=local",
		"dn: cn=admin,ou=groups,dc=hal,dc=local",
		"dn: cn=reader,ou=groups,dc=hal,dc=local",
		"dn: ou=users,dc=hal,dc=local",
		"dn: ou=groups,dc=hal,dc=local",
	} {
		if !strings.Contains(ldif, want) {
			t.Errorf("seed LDIF is missing %q:\n%s", want, ldif)
		}
	}

	// The two demo users share one password; the service accounts share another.
	if got := strings.Count(ldif, "userPassword: "+labsecrets.LDAPUserPassword); got != 2 {
		t.Errorf("expected alice and bob to carry the demo password, found %d entries", got)
	}
	if got := strings.Count(ldif, "userPassword: "+labsecrets.LDAPServiceAccountPassword); got != 3 {
		t.Errorf("expected three service accounts, found %d", got)
	}
	if !strings.Contains(ldif, "userPassword: "+labsecrets.LDAPAdminSeedPassword+"\n") {
		t.Errorf("the directory admin must be seeded with the catalog password:\n%s", ldif)
	}
}

func TestSeedLDIFPutsEachUserInTheRightGroup(t *testing.T) {
	ldif := seedLDIF()

	admins, readers, found := strings.Cut(ldif, "dn: cn=reader,ou=groups,dc=hal,dc=local")
	if !found {
		t.Fatalf("seed LDIF has no reader group:\n%s", ldif)
	}
	if !strings.Contains(admins, "member: cn="+labsecrets.LDAPUserAlice+",ou=users,dc=hal,dc=local") {
		t.Errorf("alice must be the member of the admin group:\n%s", admins)
	}
	if !strings.Contains(readers, "member: cn="+labsecrets.LDAPUserBob+",ou=users,dc=hal,dc=local") {
		t.Errorf("bob must be the member of the reader group:\n%s", readers)
	}
}

// Entries are separated by a blank line, and ldapadd rejects a file whose last
// entry is not terminated.
func TestSeedLDIFIsWellFormed(t *testing.T) {
	ldif := seedLDIF()

	if !strings.HasSuffix(ldif, "dc=hal,dc=local\n") {
		t.Errorf("the LDIF must end with a terminated entry, got:\n%q", lastLine(ldif))
	}
	for _, block := range strings.Split(ldif, "\n\n") {
		if block = strings.TrimSpace(block); block == "" {
			continue
		}
		if !strings.HasPrefix(block, "dn: ") {
			t.Errorf("every block must open with its dn, got:\n%s", block)
		}
	}
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return lines[len(lines)-1]
}
