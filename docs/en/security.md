# Servora security model

- Only members of `system-maintenance-admin` pass the post-PAM authorization check.
- Passwords are handed to PAM and never stored. Session cookies contain opaque
  random values; only their SHA-256 hashes are persisted.
- Mutations require an authenticated session, same-origin request and CSRF token.
- Five failed logins in 15 minutes trigger a 30-minute IP ban. Repeated bans
  increase exponentially.
- The direct peer must be inside `ALLOWED_CIDRS`. Forwarded client headers are
  not trusted by default.
- Service and scheduled executable actions use explicit configuration
  allowlists. Critical services, PID 1 and the agent are protected.
- Telegram tokens live in permission-restricted files. API responses and audit
  parameters never return them.
- Reboot and shutdown require an exact confirmation value at both UI and agent.

Treat membership in the administrator group as host-administrator access. Keep
the panel on a trusted LAN/VPN, use a trusted TLS certificate, and keep the
allowlists short.
