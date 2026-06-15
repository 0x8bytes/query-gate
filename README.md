# QueryGate

QueryGate is a self-hosted database access gateway for AI agents and internal tools.
It exposes a small HTTP API and an embedded admin UI for querying MySQL, PostgreSQL,
and MongoDB through controlled database connections.

The default path is read-oriented: use read-only database accounts, block risky query
patterns, hide sensitive columns, and keep an audit log. Super administrators can
optionally configure a separate `exec_dsn` for controlled write operations.

## Core Concepts

QueryGate is not just a database query UI, and it is not a generic API gateway. Its
core identity is shaped by six ideas:

- **Agent**: Designed for AI agents and developer tools that need a stable, explicit,
  machine-readable way to discover databases, inspect schemas, and run queries.
- **Database Gateway**: Sits between local tools and remote databases, turning direct
  database access into a controlled HTTP service.
- **Query Guard**: Adds a guard layer for the query path, including read-oriented
  checks, sensitive-column blocking, risky keyword filtering, and explicit-column
  query habits.
- **Read/Exec Control**: Separates ordinary read queries from privileged write
  execution. Reads are limited to SELECT-style queries and should use read-only
  database accounts; exec is opt-in, super-admin-only, and routed through a separate
  `exec_dsn`.
- **Audit**: Records query and exec activity so database access can be reviewed,
  debugged, and traced back to the user or API key that performed it.
- **Multi-DB**: Provides one consistent control surface across MySQL, PostgreSQL, and
  MongoDB.

## Features

- Multi-driver database access: MySQL, PostgreSQL, MongoDB
- HTTP API for agent-friendly schema discovery and queries
- Embedded admin UI at `/admin`
- First-run setup flow at `/setup`
- User roles: `super_admin` and `user`
- API keys generated per user
- Sensitive column blacklist
- Query audit logs stored in SQLite
- Optional super-admin-only exec path using a separate write DSN

## Project Layout

```text
cmd/server/        # server entrypoint
internal/          # application code
pkg/               # reusable packages
docs/              # additional project docs
config.example.yaml
railpack.json
```

## Quick Start

```bash
cp config.example.yaml config.yaml
go run ./cmd/server -config config.yaml
```

Then open:

- `http://localhost:8080/setup` to create the first super administrator
- `http://localhost:8080/admin` to manage users, database connections, sensitive columns, and logs
- `http://localhost:8080/` for the public API contract page

## Configuration

`config.yaml` contains runtime settings only. Keep real DSNs and secrets out of git.

Important fields:

```yaml
server:
  port: 8080
  query_timeout: 30s
  max_rows: 10000

auth:
  ip_whitelist:
    - "*"
  jwt_secret: "" # set a strong random value in production

storage:
  sqlite_path: ./querygate.db

log:
  retention_days: 30

databases: []
```

Database connections are usually added from the admin UI and stored in SQLite. Static
seed databases can also be listed under `databases`, but they are read-only from the
admin UI and cannot use `exec_dsn`.

## API

Public:

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `/` | HTML/JSON service contract |
| `GET` | `/health` | health check |

Authenticated query API, using `X-API-Key`:

| Method | Path | Body |
| --- | --- | --- |
| `POST` | `/api/v1/databases/list` | `{}` |
| `POST` | `/api/v1/tables/list` | `{"db":"<alias>"}` |
| `POST` | `/api/v1/tables/detail` | `{"db":"<alias>","names":["table_a"]}` |
| `POST` | `/api/v1/query` | `{"db":"<alias>","query":"SELECT id,name FROM users","limit":1000}` |
| `POST` | `/api/v1/exec` | `{"db":"<alias>","sql":"UPDATE ..."}` |

`/api/v1/query` is a strict read-only path:

- SQL databases only allow `SELECT` and `WITH ... SELECT` style queries.
- `SHOW`, `DESCRIBE`, `DESC`, and `EXPLAIN` are not accepted on the query path. Use
  `/api/v1/tables/list` and `/api/v1/tables/detail` for schema discovery.
- Write, DDL, privilege, transaction, lock, file, and dangerous function keywords are
  rejected before execution.
- MySQL and PostgreSQL queries run inside a read-only transaction as a second guard.

`/api/v1/exec` is only available to `super_admin` API keys and only for databases
configured with an `exec_dsn`.

MongoDB queries use a JSON string in `query`, for example:

```json
{"collection":"events","find":{"type":"signup"}}
```

or:

```json
{"collection":"events","pipeline":[{"$match":{"type":"signup"}}]}
```

MongoDB joins use aggregation pipeline with `$lookup`; `find` is single-collection
only. Pipeline stages that can write or execute unsafe server-side code, such as
`$out`, `$merge`, `$where`, `$function`, and `$accumulator`, are rejected on
`/api/v1/query`.

## Security Model

The database account is the real security boundary. QueryGate expects read queries to
use a database account with the minimum read permissions needed for the task.

The query guard is a convenience layer, not the boundary. It adds:

- `SELECT *` rejection for agent-side query API calls
- sensitive column blacklist checks
- dangerous keyword/function filtering
- multi-statement and write/DDL/privilege/transaction rejection on the query path
- read-only transactions for SQL query execution
- audit logs for query and exec activity

If you enable exec, use a separate `exec_dsn` with narrowly scoped write permissions.
Only super administrators should receive API keys that can call `/api/v1/exec`.

See [SECURITY.md](SECURITY.md) for reporting and deployment guidance.

## Railway Deployment

1. Mount a Railway volume at `/data`.
2. Set `storage.sqlite_path` to `/data/querygate.db`.
3. Add a `CONFIG_YAML` variable containing the full production config.
4. Deploy with Railpack.
5. Open `/setup` after first boot and create the first super administrator.

The included `railpack.json` writes `CONFIG_YAML` to `config.yaml` at startup when the
variable is present.

## Development

```bash
make test
make build
make server
```

Integration tests that require real databases or Docker should be run explicitly:

```bash
go test -tags integration ./...
```

## License

Apache-2.0
