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

```bash
make help
make test
make bpf
```

## 2. Install and start

Use every IP address or DNS name clients will enter in the browser:

```bash
make setup HOSTS="192.168.2.10,monitor.example.lan"
```

This builds the Vue application and Go binaries, creates the service accounts
and groups, installs the PAM/systemd definitions, generates a self-signed
certificate and enables the services. The user invoking `sudo` is authorized
automatically.

For a staged installation:

```bash
make build
make install
make cert-generate HOSTS="192.168.2.10,monitor.example.lan"
make start
```

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
