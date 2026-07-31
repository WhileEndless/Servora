# Servora architecture

The product has two runtime trust zones.

`system-maintenance-monitor` runs as the unprivileged `system-maintenance`
account. It serves the embedded Vue application, authenticates through PAM,
owns SQLite data and evaluates alert rules.

`system-maintenance-agent` runs as root. Its Unix socket is accessible only to
the dedicated agent group. The protocol accepts typed metric reads and a fixed
set of validated actions; it does not accept command strings. Process signals,
Docker actions, power operations and managed systemd unit changes are performed
here.

Backend functionality is separated into collector, action, alert-source and
notifier responsibilities. New compiled modules must report their identifier,
version, health and capabilities. Optional module failure must not prevent core
Linux metrics from being collected.

The Vue frontend follows the same boundary: view components do not call
`fetch` directly. `ApiClient` owns HTTP/CSRF behavior and `MonitorStore` owns
observable session, telemetry and navigation state.

## Network accounting

The privileged agent attaches CO-RE eBPF entry/return probes to the TCP and UDP
send/receive paths. Only positive return values are added, so counters represent
successful application bytes. Flow keys contain PID, command, protocol and
remote endpoint; packet payload is never inspected or copied.

Kernel counters are drained atomically into SQLite and aggregated into one-minute
buckets. A 262,144-key non-evicting map avoids silent LRU loss. If capacity is
exhausted, the kernel records dropped events and bytes in a separate per-CPU
counter and the UI raises a visible integrity error.

If the object cannot be loaded, the agent enters `socket-counter-fallback`.
That mode samples `ss` counters and is explicitly marked degraded because it
cannot guarantee capture of short-lived transfers.
