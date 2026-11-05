package nomad

import "hal/internal/logs"

// nomadLogsSpec lists what `hal nomad logs` can read. Nomad runs as a systemd
// unit inside a Multipass VM, not in a container, so its logs are the unit's
// journal.
var nomadLogsSpec = logs.Spec{
	Product:    "nomad",
	Components: []logs.Component{logs.MultipassUnit(nomadInstance, "nomad", "systemd journal inside the Multipass VM")},
}

func init() {
	Cmd.AddCommand(logs.New(nomadLogsSpec))
}
