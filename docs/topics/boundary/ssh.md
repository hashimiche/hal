summary: Boundary proxies SSH sessions to a Multipass Ubuntu VM through a TCP target and two lab users.
read: hal boundary ssh | hal boundary status | hal creds status
actions: hal boundary ssh enable | hal boundary ssh update | hal boundary ssh disable
docs:
- Boundary targets: https://developer.hashicorp.com/boundary/docs/domain-model/targets#target-types
- Roles and grant strings: https://developer.hashicorp.com/boundary/docs/rbac#grant-strings

# Boundary SSH target

## The mechanism

An SSH bastion host or a VPN gives a person a route to the private network, and then
hands out the key to the machine. Boundary replaces both with a session. A person
signs in to Boundary, a role decides which targets that person may open a session
to, and a worker proxies the connection to the target's address and port. The person
needs a route to Boundary, not to the machine.

A target in Boundary names where to connect. Hosts are grouped in a host set, host
sets live in a host catalog, and the target points to the host set. There are three
target types. A `tcp` target only proxies the connection. An `ssh` or `rdp` target
must have an injected credential, which Boundary puts into the session so that the
person never sees it.

## How this lab stages it

- **The machine is a Multipass VM**, `hal-boundary-ssh`, with Ubuntu 24.04, one CPU
  and 512 MB of memory by default. `--ubuntu-image`, `--cpus` and `--mem` change
  that. It needs Multipass on the host, and the command stops with an error without
  it.
- **The target is a plain `tcp` target on port 22.** It is named
  `ubuntu-ssh-secure-access`. Nothing is injected or brokered: Boundary opens the
  tunnel, and the SSH client still authenticates to the VM with the key that
  Multipass installed for the `ubuntu` user. HAL prints that as "Default ubuntu
  multipass key".
- **Everything sits in its own scopes.** An organization `hal-academy-ssh` holds a
  project `ssh-infrastructure`. The project has a static host catalog `ssh-catalog`,
  a host `ubuntu-ssh-target` whose address is the VM's IP, a host set
  `ssh-host-group` and the target.
- **Two users sign in through the auth method `ssh-lab-auth`**, a password method
  in the organization scope: `ssh-operator` and `ssh-auditor`. Their passwords are
  lab secrets, so `hal creds status` is where to read them. They are separate from
  the dev admin of the controller.
- **The roles are in the project.** `ssh-operator-role` carries the grant
  `ids=*;type=*;actions=*`, which is every action on every resource of the project.
  `ssh-auditor-role` carries `authorize-session` and `read` on the one target. The
  `authorize-session` action is what lets a user open a session, so the auditor can
  connect as well, not only look.

## What to expect

- `enable` does not check that the controller runs. The VM is launched first and the
  Boundary objects come after. With Boundary down, the VM starts and the command
  then reports that it failed to configure the Boundary API. Run
  `hal boundary create`, then `hal boundary ssh update`.
- `enable` also starts by removing any `hal-boundary-ssh` VM that exists, so it
  always ends on a fresh VM, and the host in Boundary points at its new address.
- Boundary object names are the identity. Setup is written to reuse an object that
  already has the expected name, and `disable` finds objects by name to delete them.
  HAL does not record which objects it created, so an organization you made yourself
  under the name `hal-academy-ssh` would be treated as HAL's. Keep those names for
  the lab.
- HAL does not test that the worker, which runs inside the `hal-boundary` container,
  can reach the VM's address. If a session opens and the connection hangs, that route
  is the first thing to check.
- The state is in the controller's backend. If `hal boundary update` or `create`
  replaced the backend, the objects are gone and `hal boundary ssh update` rebuilds
  them. The VM is untouched until then.

## Commands

- `hal boundary ssh` with no action is read-only. It says whether the VM is not
  deployed, active with its IP, or offline, and names the next step. Passing `status`
  does the same. `hal boundary status` shows the SSH VM next to the rest of
  Boundary.
- `enable` creates the VM and the Boundary objects, then prints a Boundary UI
  address, the scope and the auth method to choose, and a `boundary connect ssh`
  line that names the target and the auth method. That line needs the Boundary CLI
  on your machine, which HAL does not install. The UI is at `boundary.localhost:9200`.
- `update` removes what `disable` removes, then runs `enable` again. Use it when the
  status says the VM is offline, or after the controller was replaced.
- `disable` deletes the roles, users, accounts, auth method, target, host set, host,
  catalog and both scopes from Boundary, by name, then deletes the VM. If the
  controller cannot be reached, it prints a warning for the Boundary part and still
  deletes the VM. HAL only deletes a Multipass VM whose name starts with `hal-`.
