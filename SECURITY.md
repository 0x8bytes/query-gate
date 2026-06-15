# Security Policy

QueryGate is a database access gateway. Treat every deployment as security-sensitive.

## Reporting Vulnerabilities

Please do not open public issues for suspected vulnerabilities. Report them privately
to the project maintainer through the repository security advisory flow, or by email
if the repository lists a maintainer contact.

Include:

- affected version or commit
- deployment mode
- reproduction steps
- expected and actual impact
- any logs that do not expose secrets

## Deployment Guidance

- Use read-only database accounts for the query path.
- Treat `config.yaml`, `jwt_secret`, DSNs, API keys, and SQLite data files as secrets.
- Do not commit `config.yaml`, `*.db`, `.env`, or production logs.
- Set a strong `auth.jwt_secret` in production so sessions survive restarts.
- Restrict `auth.ip_whitelist` when the service is not behind a trusted private network.
- Use TLS in front of the service.
- Keep `exec_dsn` disabled unless you truly need controlled write operations.
- If `exec_dsn` is enabled, use a separate database account with minimal write scope.
- Give `super_admin` API keys only to trusted operators.

## Security Model

The database account is the hard boundary. QueryGate's guard layer reduces mistakes
and common unsafe patterns, but it is not a substitute for database permissions.

The read query path applies query guard checks and sensitive-column filtering. The
exec path intentionally does not apply query guard checks; it is protected by role
checks, separate `exec_dsn` configuration, and database-level permissions.
