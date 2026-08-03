# Servora installation and first-login flow

## 1. Review prerequisites

The target must use Linux, systemd and PAM. Docker is optional. Exact network
accounting additionally requires kernel BTF plus the eBPF build toolchain.

Debian/Ubuntu:

```bash
sudo apt-get update
sudo apt-get install -y golang-go gcc clang llvm libbpf-dev bpftool \
  libpam0g-dev libsqlite3-dev nodejs npm openssl
```

Go module dependencies, including `github.com/cilium/ebpf`, are pinned in
`go.mod`/`go.sum` and downloaded by the Go toolchain during the build.

Verify the toolchain before building. `make deps-check` reports every missing
command and development header at once, with the command that installs them:

```bash
make deps-check
make help
make test
```

## 2. Install and start

```bash
make setup
```

This builds the Vue application and Go binaries, creates the service accounts
and groups, installs the PAM/systemd definitions, generates a self-signed
certificate and enables the services. The user invoking `sudo` is authorized
automatically.

Before changing anything, the installer asks for the three settings that differ
between machines. Press Enter to accept the value in brackets:

- **Listener** — address and port, `0.0.0.0:8443` by default. If another
  service already holds the port, the installer names that process and offers
  the next free port rather than leaving a service that cannot start. A port
  held by an already-running Servora monitor is not a conflict.
- **Allowed networks** — `ALLOWED_CIDRS`. The default is taken from this host's
  own interfaces, ignoring container and virtual bridges, plus loopback.
- **Certificate hosts** — every IP address or DNS name clients will enter in
  the browser. Asked only when no certificate exists yet.

Answer without a terminal by passing the values in. Any setting given this way
is not prompted for, which is what CI and `curl | sudo bash` installs use:

```bash
make install LISTEN=0.0.0.0:9443 ALLOWED_CIDRS=203.0.113.0/24 \
  HOSTS="203.0.113.10,monitor.example.lan"
```

For a staged installation:

```bash
make build
make install
make start
```

Re-running `make install` is safe. Existing answers come back as the defaults,
and only the keys you change are rewritten in `monitor.conf` — comments and
hand edits survive, and a `.bak` copy is kept.

## 3. Authorize Linux users

The users must already exist in the host account database.

```bash
make admin-add
make admin-add ADMIN_USER=another-user
make admin-list
```

Remove access without deleting the Linux account:

```bash
make admin-remove ADMIN_USER=another-user
```

The application checks group membership on every login, so no service restart
is required. Verify the authoritative group record with:

```bash
getent group system-maintenance-admin
```

## 4. Sign in

Open `https://HOST:8443` from an address allowed by `ALLOWED_CIDRS`. Use the
Linux username and that account's local/PAM password.

An SSH private key is not a web password. An account configured only for
public-key SSH needs a valid PAM authentication method before it can use the
default web login.

Authentication is executed by the privileged agent over its UID-verified Unix
socket. The unprivileged web process does not read `/etc/shadow`; passwords are
not stored or logged.

## 5. Diagnose login failures

```bash
make admin-list
sudo passwd -S USERNAME
sudo journalctl -u system-maintenance-agent.service \
  -u system-maintenance-monitor.service -n 100 --no-pager
```

- `authentication failure`: verify the Linux password and PAM configuration.
- `account is not authorized`: verify administrator-group membership.
- `temporarily_banned`: wait for the displayed ban period after repeated failures.
- Agent unavailable: run `make status` and `make logs`.

After upgrading authentication code or configuration, run `make upgrade`.
Ordinary administrator membership changes never require a restart.

## 6. Certificates and network access

Replace the generated certificate when available:

```bash
make cert-install CERT=/path/fullchain.pem KEY=/path/privkey.pem
```

Edit `/etc/system-maintenance/monitor.conf` to change `ALLOWED_CIDRS`, then run
`make restart`. Do not expose the service publicly without an appropriate
firewall, trusted TLS certificate and reviewed allowlists.

## 7. Configure features

1. Add Telegram destinations under **Alerts → Telegram** and send a test.
2. Add threshold rules under **Alerts → Rules**.
3. Review `SERVICE_ALLOWLIST`, `PROTECTED_SERVICES` and `JOB_EXECUTABLES`.
4. Create managed jobs under **Schedules**.
5. Use **Processes → Watch rule** for persistent process history.
6. Open **Network** and verify the accounting badge reads **EXACT eBPF**.
   A **DEGRADED** badge is operational but does not satisfy lossless accounting.

See the operations and security guides before enabling additional privileged actions.
