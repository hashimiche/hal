summary: Consul in this lab, one standalone server container that Vault, Boundary and Nomad can be pointed at and that the observability stack can scrape.
read: hal consul status | hal consul logs | hal consul obs status
actions: hal consul create | hal consul update
docs:
- Consul architecture: https://developer.hashicorp.com/consul/docs/architecture#introduction
- Starting the agent: https://developer.hashicorp.com/consul/docs/fundamentals/agent#start-the-consul-agent
- Metrics endpoint: https://developer.hashicorp.com/consul/api-docs/agent#view-metrics

# Consul in this lab

HAL runs one Consul container, `hal-consul`, on the `hal-net` Docker network.
`hal consul create` starts it, `hal consul update` removes the container and starts a
new one from the flags you pass, and `hal consul delete` removes it. Consul keeps a
catalog of services and a key/value store. Here it is a base that other products can
point at, and it has no scenario commands of its own.

## Modes and editions

- **There is one shape.** The container runs `agent -server -ui` with
  `-bootstrap-expect=1`, the node name `hal-server` and `-client=0.0.0.0`: a single
  server that elects itself, with the web UI. It is not Consul's `-dev` mode.
- **HAL mounts no volume.** What Consul holds lives with the container, and a
  replaced container starts empty.
- **HAL passes no option for ACLs or TLS.** The API is plain HTTP and needs no
  secret, so `hal creds status` has no Consul entry.
- There is no edition switch. `--consul-image` and `--consul-tag` change the image,
  `hashicorp/consul` by default. `hal consul create --help` lists the defaults.

## Reaching it

The UI and the API answer at `consul.localhost:8500`, a port HAL publishes on the
host. Other containers on the lab network reach the server as `hal-consul:8500`.

## Pointing other products at it

`--join-consul` exists on `hal vault create`, `hal boundary create` and
`hal nomad create`. It is a pre-check and an address, and not a cluster join.

- **The pre-check.** The command refuses to run unless `hal-consul` is running.
- **The address.** Vault and Boundary get `CONSUL_HTTP_ADDR=http://hal-consul:8500`
  in their container. HAL configures no Consul storage and no service registration
  for them. Nomad runs in a Multipass VM that is not on the Docker network, so its
  systemd unit gets the same variable with the address of the VM's default gateway,
  which HAL's script treats as the host, on port 8500. Nomad takes its Consul
  address from that variable.
- **The "tethered" line** that these commands print reports the pre-check. It does
  not mean that the product became a member of a Consul cluster.
- The flag is not remembered. To add or drop it, run the product's `update` again
  with or without `--join-consul`.

Consul is not owned by any of the three. It is not in the registry of shared
services, none of them registers as a consumer, and `hal consul delete` does not look
at who was pointed at it. After it goes, the variable in the other products points at
nothing.

## Observability

`hal consul obs create` needs Consul and the obs stack to run (see the obs topic). It
registers `hal-consul:8500` as the Prometheus target named `consul` and downloads
Grafana dashboard 13396. Prometheus asks Consul for `/v1/agent/metrics` in Prometheus
format. Treat the metrics as unconfirmed. Consul's upstream code answers that request
with an error unless its `prometheus_retention_time` is positive, and HAL sets no
telemetry option. Check the Prometheus targets page before relying on them.

## Checking and removing

- `hal consul status` reports the state of the container, and the bare `hal consul`
  runs it too. `hal consul obs status` reports the target file and the dashboard file.
- `hal consul logs` reads the most recent lines of the container and never follows.
- `hal consul update` removes `hal-consul` and runs it again. Keys and services are
  gone, and the other products still hold the same address, so nothing needs
  re-pointing.
- `hal consul delete` removes the container, removes the network when nothing else
  uses it, and removes the Consul Prometheus target file. It leaves a downloaded
  dashboard file in place. The lesson does not offer this command as an action: the
  learner types it in a terminal.
