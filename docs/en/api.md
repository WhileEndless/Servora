# Servora API conventions

All endpoints are below `/api/v1`. JSON is the default representation. List
responses use an `items` array. Errors have a stable shape:

```json
{"error":{"code":"authentication_required","message":"Authentication required"}}
```

`POST /auth/login` and `GET /health` are public. Every other route needs the
secure session cookie. POST and DELETE calls also require the session's
`X-CSRF-Token` and an HTTPS `Origin` matching the request host.

Primary resources are `overview`, `history`, `processes`, `watches`, `network`,
`ssh`, `docker`, `services`, `schedules`, `alert-rules`, `alerts`,
`notification-targets`, `activities`, `modules` and `actions`. Live snapshots
are delivered as server-sent events from `/stream`.

Fast CPU, memory, process and interface metrics stream every second by default.
Connection ownership refreshes every second, Docker every five seconds,
and systemd/SSH/timer inventory every fifteen seconds. Slow inventories are
cached and never block fast telemetry.

`GET /docker` returns availability, refresh time, collector errors, container
items and a daemon summary. The summary includes engine version, storage
driver, image count and running/stopped/paused container counts. An available
daemon with an empty `items` array is a valid empty inventory.

`GET /docker/images` returns one record per local physical image ID with its
repository/tag references, digests, size, and consuming containers. It does not
query remote registries.

`GET /packages` provides searched, filtered, sorted, and paginated system
packages. `GET /packages/{id}/files` pages installed paths and
`GET /package-events` returns install, removal, and version-change history.
`POST /packages/refresh` installs nothing; it queues a repository metadata
refresh and records the request in the audit log.

`GET /processes/{pid}` returns executable and working-directory information,
cgroup, kernel status, limits, children, namespaces, an open-file summary and
owned network connections.

`GET /network-usage` accepts `from`, `to`, `group_by=process|group` and `q`.
It returns byte totals, destination counts, activity bounds and storage usage.
`GET /network-usage/detail` accepts the same range plus
`selector=process|group|pid` and `value`, then returns endpoint totals and a
time-bucketed activity series. The live process detail panel uses the PID
selector once per second while it is open; no extra requests run after closing
the panel.

`GET /resource-usage` accepts the same range, grouping and search parameters.
It returns average/peak CPU and resident memory plus disk read/write deltas.
`GET /resource-usage/detail` returns the selected process or group timeline.

`GET /settings/network` returns retention and storage information.
`PATCH /settings/network` accepts `{"retention_days":10}` (1–365).
`DELETE /settings/network` requires
`{"confirm":"DELETE NETWORK HISTORY"}` and permanently clears stored flow
history. Mutating calls require CSRF protection and are audited.
Collector booleans are `network_enabled`, `cpu_enabled`, `memory_enabled` and
`disk_io_enabled`. `DELETE /settings/resources` requires the exact phrase
`DELETE RESOURCE HISTORY`.

The API intentionally has no generic command execution route. Clients must use
the typed action identifiers accepted by the privileged agent.
