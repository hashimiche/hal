package terraform

import "hal/internal/logs"

// terraformLogsSpec lists what `hal terraform logs` can read: the containers
// `hal terraform delete` removes (tfePrimaryContainers, tfeSharedBackendContainers)
// plus the twin instance, which `--target twin` of the other commands manages.
// The TFE core is the default. GitLab and Authentik are shared singletons that
// Vault uses too, so they are not listed.
//
// The twin is addressed by component name rather than by --target: the shared
// backend (db, redis, s3, proxy) serves both instances, so a "twin" view of it
// would be the same container.
var terraformLogsSpec = logs.Spec{
	Product: "terraform",
	Components: []logs.Component{
		logs.Container(tfeCoreContainer, "TFE primary"),
		logs.Container(tfeDBContainer, "shared PostgreSQL"),
		logs.Container(tfeRedisContainer, "shared Redis"),
		logs.Container(tfeS3Container, "shared object storage"),
		logs.Container(tfeProxyContainer, "shared ingress proxy"),
		logs.Container(tfeAgentPrimaryContainer, "agent, primary"),
		logs.Container(tfeAPIPrimaryContainer, "api-workflow helper, primary"),
		logs.Container(defaultTFETwinContainer, "TFE twin"),
		logs.Container(tfeAgentTwinContainer, "agent, twin"),
		logs.Container(tfeAPITwinContainer, "api-workflow helper, twin"),
	},
}

func init() {
	Cmd.AddCommand(logs.New(terraformLogsSpec))
}
