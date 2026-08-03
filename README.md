# Servora

<a href="https://github.com/WhileEndless/Servora"><img src="web/public/assets/servora-logo.png" alt="Servora logo" width="140"></a>

Servora is a self-hosted Linux monitoring and management
dashboard. Version **0.0.1** combines the repository's existing package and
Docker maintenance timer with a secure, historical system monitor.

[GitHub](https://github.com/WhileEndless/Servora) · [Türkçe README](README.tr.md) · [Installation](docs/en/installation.md) · [Architecture](docs/en/architecture.md) ·
[Security](docs/en/security.md) · [Operations](docs/en/operations.md)

## Features

- One-second live CPU, load, memory, swap, filesystem and network metrics, plus
  retained history and collector freshness indicators.
- Sortable htop-style process inventory, process drill-down, persistent regex
  watchers and confirmed, controlled signals.
- Optional per-process and grouped CPU, resident-memory and disk-I/O history
  using one-minute aggregates from telemetry already collected through `/proc`.
- systemd service inventory, active duration, resource data and allowlisted actions.
- Installed versions, update candidates, file locations, and change history
  for APT/dpkg and DNF/RPM packages.
- Docker inventory, health, ports, CPU, memory, network and block-I/O statistics.
- A separate local Docker image inventory with tags, digests, sizes, and
  container usage.
- Exact successful TCP/UDP application-byte accounting by process, process
  group and remote endpoint, with searchable history and configurable retention.
- Active SSH sessions and systemd timer/classic cron inventory.
- Safe creation of namespaced systemd timers using allowlisted executables.
- Dashboard alert rules and multiple Telegram destinations.
- PAM authentication using existing Linux accounts, persistent sessions, CSRF
  protection, CIDR restrictions, escalating login bans and an audit trail.
- English-first Vue 3 interface with Turkish localization.
- URL-addressable views with browser back/forward support and a compact sidebar
  showing live CPU, memory, network and uptime telemetry.

Packet payloads are never captured. The application has no arbitrary shell
endpoint. Existing cron entries and unmanaged systemd units remain read-only.

The Network page reports the collector state explicitly. **EXACT eBPF** counts
every successful application byte, including one-byte transfers. **DEGRADED**
uses sampled socket counters and can miss short-lived traffic. The exact
collector also exposes any capacity-related dropped-byte count; non-zero loss is
never hidden. Counters describe application payload bytes accepted by or
returned from the kernel, not Ethernet framing or TCP retransmission overhead.
Binary quantities use IEC units (`KiB`, `MiB`, `GiB`, `TiB`) in traffic and
resource analysis views.

## Requirements

- Linux with systemd and PAM
- Go 1.26.5+, a C compiler, PAM and SQLite development headers
- Node.js 22+ and npm for frontend builds
- OpenSSL for first-run certificate generation
- Kernel BTF/eBPF support for exact network accounting
- Optional: Docker, `ss` fallback and thermal sensors

On Debian/Ubuntu:

```bash
sudo apt-get install golang-go gcc clang llvm libbpf-dev bpftool \
  libpam0g-dev libsqlite3-dev nodejs npm openssl
```

## Build and install

No installation command is run automatically by the repository.

```bash
make test
make build
make install
make cert-generate HOSTS="203.0.113.10,monitor.example.lan"
make start
```

Run `make help` at any time to see all supported workflows.

`make install` adds the invoking `sudo` user to
`system-maintenance-admin`. Group membership becomes active after signing in
again. Add another Linux user with:

```bash
sudo usermod -a -G system-maintenance-admin USERNAME
```

The equivalent documented Make targets are:

```bash
make admin-add                         # current Linux user
make admin-add ADMIN_USER=alice        # another existing Linux user
make admin-list
make admin-remove ADMIN_USER=alice
```

No service restart is required after changing membership. Login uses the
account's Linux password, not its SSH private key. See the
[complete installation and login flow](docs/en/installation.md).

Open `https://HOST:8443`. A generated certificate is self-signed, so install its
certificate in trusted clients or replace it:

```bash
make cert-install CERT=/path/fullchain.pem KEY=/path/privkey.pem
```

## Configuration

The installed monitor configuration is
`/etc/system-maintenance/monitor.conf`. `make install` writes the listener and
the access networks you confirm; without an answer it grants loopback only, so
the service is never exposed to a network you did not name. Review these
settings before exposing it:

- `ALLOWED_CIDRS`
- `SERVICE_ALLOWLIST`
- `PROTECTED_SERVICES`
- `JOB_EXECUTABLES`
- `MAX_DATABASE_MB`

The metrics database is stored in `/var/lib/system-maintenance-monitor`. Raw
metrics are retained for 30 days, five-minute rollups for one year, and the
default database quota is 2 GiB.

Fast host metrics stream every second by default. Connection ownership, Docker
and operating-system inventories use separate background refresh cadences so a
slow optional collector cannot stall the live dashboard. Set
`SAMPLE_INTERVAL=500ms` or higher in `monitor.conf` if another cadence is
required.

## Lifecycle

```bash
make status
make logs
make restart
make stop
make upgrade
make uninstall  # keeps configuration, certificates and history
make purge      # permanently removes preserved application data
```

The original maintenance timer remains available:

```bash
systemctl list-timers system-maintenance.timer
sudo systemctl start system-maintenance.service
journalctl -u system-maintenance.service -f
```

## Development

```bash
cd web
npm ci
npm run dev

# In another shell
make test
```

Vite is used only during development/build. Its production output is embedded
inside the Go monitor binary.

## Security reporting

Do not publish PAM credentials, session cookies, Telegram bot tokens,
certificates, databases or production configuration in an issue. See
[the security model](docs/en/security.md) before enabling management actions.

## License

Servora is licensed under the [GNU Affero General Public License v3.0](LICENSE).
