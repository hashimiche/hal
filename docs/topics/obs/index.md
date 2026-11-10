summary: The shared Prometheus, Loki, Promtail and Grafana stack that the other products register their metrics with.
read: hal obs status | hal obs logs
actions: hal obs create | hal obs update
docs:
- Prometheus file-based discovery: https://prometheus.io/docs/prometheus/latest/configuration/configuration/#file_sd_config
- Grafana dashboard provisioning: https://grafana.com/docs/grafana/latest/administration/provisioning/#dashboards
- Promtail agent: https://grafana.com/docs/loki/latest/send-data/promtail/#promtail-agent

# Observability in this lab

HAL runs four containers on the `hal-net` Docker network: `hal-prometheus` for
metrics, `hal-loki` for logs, `hal-promtail` to ship logs and `hal-grafana` for
dashboards. `hal obs create` starts all four, and `hal obs delete` removes them. The
stack does not belong to a product. Vault, Consul, Nomad, Boundary and Terraform
Enterprise each register with it through their own `hal <product> obs create`.

## Modes and editions

- **There is one shape.** HAL mounts no data volume: Prometheus and Loki keep their
  metrics and logs inside their containers, and a replaced container starts empty.
  The mounts are the generated configuration, the `targets` directory and the
  `dashboards` directory, and Promtail's read-only view of the Vault audit log.
- **The images are the stock ones.** `hal obs create --help` lists the image and tag
  flags for each of the four. Prometheus and Grafana default to the `main` tag, which
  moves, so two runs weeks apart may not get the same version. Promtail defaults to
  3.6. Grafana's documentation says Promtail reached end of life on 2 March 2026.
- **Configuration is generated** under `~/.hal/obs` on the host: the Prometheus,
  Loki, Promtail and Grafana files, a `targets` directory and a `dashboards`
  directory. HAL bind-mounts them into the containers.
- Before it starts, `create` may ask a yes or no question when the container engine
  looks too small for the stack. Anything other than `y` or `yes` stops it.

## Reaching it

- Grafana answers at `grafana.localhost:3000`, Prometheus at
  `prometheus.localhost:9090` and Loki at `loki.localhost:3100`. Promtail publishes
  nothing.
- **Grafana has no login.** HAL turns on anonymous access with the Admin role, and
  the port is published with Docker's default binding. Anyone who can reach port 3000
  on the host can administer Grafana, so this is a lab setup. There is nothing in
  `hal creds status` for it.
- Grafana comes with two data sources, Prometheus (the default) and Loki.

## How products register

This is the one mechanism the stack has. The generated Prometheus configuration has
one scrape job per product, each reading a file in the `targets` directory:
`vault.json`, `consul.json`, `nomad.json`, `boundary.json` and `terraform.json`. The
scrape interval is 15 seconds, and each job has its own metrics path.

`hal <product> obs create` writes that file with the product's address, but only
once Prometheus and Grafana run, and it refuses otherwise. Prometheus watches the
files and, according to its documentation, applies a change at once, with a periodic
re-read as the fallback, so HAL does not reload it. If a new target is slow to
appear, that fallback is the likely wait. For Vault, Consul,
Nomad and Terraform Enterprise it also downloads a Grafana dashboard from grafana.com
into the `dashboards` directory, and Grafana provisions it into a folder named `HAL`.
Boundary has no dashboard. If the download fails, the command prints a warning and
goes on.

What the stack scrapes depends on the product's own metrics, which HAL does not
check. The Consul and Boundary pages call it unconfirmed for that reason. The
Prometheus targets page is the place to see what is up.

Promtail has one job. It reads the Vault audit log from the `hal-vault-logs` volume,
mounted read-only. No other product's logs are shipped. For container logs, use
`hal <product> logs`.

## Custom scrape jobs

`hal obs update` with `--job-name` (and `--metrics-path`), or with
`--scrape-config-path`, adds one job to the Prometheus configuration and sends the
running Prometheus a reload signal. It does not touch the containers. A named job
gets a target file holding the placeholder `host:port`, which you edit by hand.

## Checking and removing

- `hal obs status` reports the state of the four containers, and the bare `hal obs`
  runs it too. `hal obs logs` reads the most recent lines of one container, Grafana
  by default, and `--component` picks `loki`, `prometheus` or `promtail`. It never
  follows.
- **`hal obs update` without a job flag rebuilds the stack.** It removes the four
  containers and wipes `~/.hal/obs`, so every product target file and dashboard
  file goes with it, then runs `create`. Each product's `obs create` has to be run
  again.
- **`hal obs create` on a stack that already runs fails**, after it has rewritten
  the generated configuration files, which loses a custom job in `prometheus.yml`.
  Use `update` to change a running stack.
- `hal obs delete` removes the four containers, wipes `~/.hal/obs`, and removes the
  network when nothing else uses it. A product's `obs create` then refuses until the
  stack is back. The lesson does not offer this command as an action: the learner
  types it in a terminal.
- The `delete` commands of Vault, Consul, Nomad and Boundary remove only that
  product's target file. They do not touch the stack or the dashboard files.
