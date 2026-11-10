summary: Nomad in this lab, one dev-mode agent in a Multipass VM, with the option to point it at the lab Consul and to scrape it.
read: hal nomad status | hal nomad logs | hal nomad obs status
actions: hal nomad create | hal nomad update
docs:
- Nomad architecture: https://developer.hashicorp.com/nomad/docs/architecture#high-level-overview
- Agent command options: https://developer.hashicorp.com/nomad/commands/agent#options
- Consul configuration for Nomad: https://developer.hashicorp.com/nomad/docs/configuration/consul#parameters-for-nomad-clients-and-servers

# Nomad in this lab

HAL runs Nomad as a systemd service inside one Multipass VM, `hal-nomad`, and not in
a container. `hal nomad create` launches the VM and installs Nomad, `hal nomad update`
deletes the VM and builds a new one, and `hal nomad delete` removes it. It needs
Multipass on the host, and the command stops with an error without it. Nomad is a
scheduler: servers decide where work runs, and clients run it.

## Modes and editions

- **Dev mode is the only mode.** The service runs `nomad agent -dev -bind 0.0.0.0`
  and restarts on failure. Nomad documents `-dev` as a pre-configured agent that is
  both server and client, for developing and testing. It is a classroom setup, not a
  model of a production cluster.
- **The VM defaults** are Ubuntu 24.04, 2 CPUs and 2G of memory. `--ubuntu-image`,
  `--cpus` and `--mem` change them, and `--version` (`-v`) picks the Nomad release,
  2.0.3 by default. The binary is the release zip from releases.hashicorp.com. There
  is no Enterprise option.
- **The VM has Nomad and nothing else.** HAL installs only `unzip` and the Nomad
  binary, and no container runtime. Nomad's dev mode turns on the `raw_exec` driver,
  according to its upstream source (read on the main branch, not on the pinned
  release).

## Reaching it

`hal nomad create` waits for the API to answer, with up to 45 attempts two seconds
apart, then prints the UI address, `http://<VM IP>:4646`. If the API never answers,
it prints the last lines of the service's journal and tells you to run
`hal nomad delete`. `hal nomad status` shows the VM's
address again. HAL publishes nothing on the host for Nomad, so `localhost` does not
reach it. Nomad has no login in this lab: HAL sets no ACL option, and
`hal creds status` has no Nomad entry. The Nomad command line is in the VM, and HAL
does not install one on your machine.

## Scenarios

Nomad has no scenario commands. `hal nomad job` is listed in `--help` as submitting
sample workloads, but in the code it is an unfinished stub (a `TODO`): it prints
"Job deployed!" and submits nothing. A lesson must not present it as a way to run a
job, and it is not offered here.

## Consul

`hal nomad create --join-consul` refuses to run unless `hal-consul` is running. Then
it sets `CONSUL_HTTP_ADDR` in the Nomad systemd unit to the address of the VM's
default gateway, which HAL's script treats as the host, on port 8500, and that
relies on the Consul container publishing 8500 there. HAL does not test that the VM
can reach it. Nomad's documentation says its Consul address
defaults to that variable. What Nomad then registers in Consul is Nomad's own
behaviour, and HAL does not check it. The "tethered" line that `create` prints
reports the pre-check, not a result. The flag is not remembered, so use
`hal nomad update --join-consul` to add or drop it. See the Consul topic.

HAL wires nothing between Nomad and Vault.

## Observability

`hal nomad obs create` needs the VM and the obs stack to run (see the obs topic). It
reads the VM's current address and registers `<VM IP>:4646` as the Prometheus target
named `nomad`, then downloads Grafana dashboard 15764. Prometheus asks Nomad for
`/v1/metrics` in Prometheus format, and Nomad's dev mode turns Prometheus metrics on
in its upstream source (main branch again). No scrape has been run to confirm it. HAL does not test that the Prometheus container can reach the
VM's address. The target file holds the address from the time it was written, so
after `hal nomad update` run `hal nomad obs update`. `hal nomad obs status` compares
the file with the current address.

## Checking and removing

- `hal nomad status` is the check, and the bare `hal nomad` runs it too. It reports
  the VM state and asks the API for its leader. `hal nomad obs status` reports the
  target file and the dashboard file.
- `hal nomad logs` reads the most recent lines of the `nomad` systemd unit's journal
  inside the VM and never follows.
- `hal nomad update` deletes the VM and runs `create` again, so every job and
  anything else the agent held is gone.
- `hal nomad delete` deletes the VM and removes the Nomad Prometheus target file.
  HAL only deletes a Multipass VM whose name starts with `hal-`. The lesson does not
  offer this command as an action: the learner types it in a terminal.
