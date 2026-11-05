package boundary

import "hal/internal/logs"

// boundaryLogsSpec lists what `hal boundary logs` can read: the containers
// `hal boundary delete` removes (boundaryEcosystem), controller first. The
// Multipass SSH target VM (hal-boundary-ssh) is a demo target, not a service, so
// it is not a log source.
var boundaryLogsSpec = logs.Spec{
	Product: "boundary",
	Components: []logs.Component{
		logs.Container(boundaryContainer, "controller and worker"),
		logs.Container(boundaryBackendContainer, "PostgreSQL backend"),
		logs.Container(boundaryMariaDBContainer, "MariaDB target"),
	},
}

func init() {
	Cmd.AddCommand(logs.New(boundaryLogsSpec))
}
