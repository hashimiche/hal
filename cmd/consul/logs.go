package consul

import "hal/internal/logs"

// consulLogsSpec lists what `hal consul logs` can read: the single Consul
// container, which is the whole product.
var consulLogsSpec = logs.Spec{
	Product:    "consul",
	Components: []logs.Component{logs.Container(consulContainer, "")},
}

func init() {
	Cmd.AddCommand(logs.New(consulLogsSpec))
}
