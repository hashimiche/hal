package observability

import "hal/internal/logs"

// obsLogsSpec lists what `hal obs logs` can read: the containers
// `hal obs delete` removes (obsEcosystem). Grafana is the default because it is
// the container `hal status` uses to decide that the stack is running.
var obsLogsSpec = logs.Spec{
	Product: "obs",
	Components: []logs.Component{
		logs.Container(obsGrafanaContainer, "dashboards"),
		logs.Container(obsLokiContainer, "log store"),
		logs.Container(obsPrometheusContainer, "metrics store"),
		logs.Container(obsPromtailContainer, "log shipper"),
	},
}

func init() {
	Cmd.AddCommand(logs.New(obsLogsSpec))
}
