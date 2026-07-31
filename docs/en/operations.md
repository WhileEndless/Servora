# Servora operations

## Telegram

Create a bot with Telegram's BotFather, add it to the destination group if
needed, and obtain the chat ID. In **Alerts → Telegram**, store a named
destination and send a test message. The token is write-only after submission.
Select one or more named destinations while creating an alert rule.

## Scheduled jobs

Add an absolute executable path to `JOB_EXECUTABLES`, restart the agent, then
create a schedule in the UI. Schedules accept systemd `OnCalendar` expressions.
Generated units are prefixed `system-maintenance-job-`, do not overlap, use a
one-hour timeout and are the only schedules the UI may mutate.

Existing classic cron jobs and unmanaged timers are inventory-only.

## Troubleshooting

```bash
make status
make logs
journalctl -u system-maintenance-agent.service --since today
journalctl -u system-maintenance-monitor.service --since today
```

If login fails, confirm group membership with `id USER` after opening a new
login session. If the UI cannot collect data, inspect Unix-socket ownership and
agent logs. Docker visibility requires a running Docker daemon; the web service
is deliberately not added to the Docker group.

The Docker page distinguishes an unreachable daemon from a connected daemon
with no containers. Verify the latter with `docker ps -a`.

## Package inventory

**Packages → Inventory** lists APT/dpkg or DNF/RPM packages and update
candidates. The first scan establishes a baseline; later installs, removals,
and version changes are retained under **Changes** for one year. Package
details provide a searchable installed-file list.

**Check for updates** installs no packages. It only refreshes repository
metadata; when the package manager is locked, the last successful data remains
visible with an explicit error. The `PACKAGE_*` settings in `monitor.conf`
control scan, metadata refresh, and event retention intervals.

Core telemetry defaults to one-second updates. `SAMPLE_INTERVAL` accepts values
between `500ms` and `1m`; restart the services after changing it.

## Network history and integrity

The Network page starts with per-process totals. Switch to **Groups** to combine
related processes such as SSH, search by process/group/user, change the time
range, or open a row for its timeline and remote endpoints.

The mode badge must read **EXACT eBPF** when completeness matters. If it reads
**DEGRADED**, inspect the agent log and confirm the BPF object exists:

```bash
make logs
ls -l /opt/system-maintenance/lib/network_accounting.bpf.o
```

Build dependencies and upgrade on Debian/Ubuntu:

```bash
sudo apt-get update
sudo apt-get install -y clang llvm libbpf-dev bpftool
make upgrade
```

Retention defaults to ten days and is configurable from **Settings → Network
history**. The same panel reports database usage and provides a typed-confirmation
clear action. Any non-zero dropped-byte value is shown as a data-integrity error.
Clearing network history also resets the live eBPF flow maps and dropped-byte
integrity counter; the next sample starts a new accounting epoch.

## CPU, memory and disk-I/O history

Open **Processes → Resource analysis** to search and group historical CPU,
resident memory, disk reads and disk writes. CPU and memory store average and
peak values; disk values are counter deltas, so enabling disk collection does
not count I/O that occurred while it was disabled.

Opening a resource row also joins its retained network destinations and, for
matching processes that are still alive, reads current executable, working
directory, cgroup and open-file paths on demand. Open paths are live evidence,
not a historical per-file byte claim. Continuous file-level I/O attribution is
intentionally not enabled because syscall-level collection can impose
significant event volume on storage-heavy hosts.

**Settings → Historical resource collectors** controls network, CPU, memory and
disk-I/O persistence independently. Disabling persistence does not disable live
telemetry. Resource storage size and row count are visible there and resource
history has its own typed-confirmation clear action. All series share the
configured 1–365 day retention.

Alert events use severity-specific styling and include context: CPU/memory/load
events show top processes, disk-capacity events show the most-used mount, and
network-total events show top attributed processes.
