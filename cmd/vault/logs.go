package vault

import "hal/internal/logs"

// vaultLogsSpec lists what `hal vault logs` can read: the containers
// `hal vault delete` removes (vaultEcosystem), Vault itself first.
//
// GitLab and Authentik are deliberately absent. They are shared singletons that
// Terraform uses too, so neither product owns them (see the note on
// vaultEcosystem in delete.go).
var vaultLogsSpec = logs.Spec{
	Product: "vault",
	Components: []logs.Component{
		logs.Container(vaultContainer, "Vault server"),
		logs.Container(openLDAPContainer, "hal vault ldap"),
		logs.Container(phpLDAPAdminContainer, "hal vault ldap UI"),
		logs.Container(vaultMariaDBContainer, "hal vault database, shared with agentic-iam"),
		logs.Container(vaultOracleContainer, "hal vault database (Oracle)"),
		logs.Container(agenticIAMChatContainer, "hal vault agentic-iam"),
		logs.Container(agenticIAMAgentContainer, "hal vault agentic-iam"),
	},
}

func init() {
	Cmd.AddCommand(logs.New(vaultLogsSpec))
}
