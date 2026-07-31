# Adding a Servora module

Keep module code behind a narrow interface and register it in application
composition rather than importing it from unrelated views or handlers.

- Collectors return typed snapshots and capability/health state.
- Action providers validate identifiers and arguments before entering a
  privileged boundary.
- Alert sources expose a numeric value or discrete event without coupling rule
  evaluation to collection.
- Notifiers accept a rendered event and return a delivery result. Secrets are
  referenced by ID and never embedded in rule records.

Optional dependencies must be detected at runtime and produce an unavailable
capability instead of terminating the process. Add unit tests for validation,
fallback behavior and secret redaction; add the module to both English and
Turkish documentation.

The first release deliberately uses compiled modules. Do not load arbitrary
shared objects or executables as plugins.
