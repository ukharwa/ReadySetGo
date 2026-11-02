# ReadySetGo

<!-- readysetgo:begin template -->
A starting point for Go web apps and APIs backed by SQLite or PostgreSQL:
database, accounts, security, background jobs, email, Caddy, Docker,
tooling and deployment, with no business logic. Copy it, run `make init`,
and start on the features.

## Start a new project from this template

1. Copy the template (GitHub's "Use this template", or clone it) into a new
   repository, and install Go 1.27 or newer.
2. Initialise it with the options the project needs:

   ```bash
   make init NAME=myapp DB=sqlite UI=html MODULES=workspaces,admin
   ```

   | Option | Values | Default |
   |---|---|---|
   | `NAME` | the app's name: lowercase letters, digits and hyphens | required |
   | `MODULE` | the Go module path | `github.com/ukharwa/NAME` |
   | `DB` | `sqlite` or `postgres` (see the guide below) | `sqlite` |
   | `UI` | `html` (pages with HTMX) or `api` (JSON for a separate frontend or mobile app) | `html` |
   | `MODULES` | any of `workspaces`, `admin`, `audit`, `uploads`, `metrics`, `errreport`, or `all` | none |

   Init renames everything to `NAME`, keeps only the chosen database, UI mode
   and modules, deletes the template's own tooling, runs `go mod tidy`, and
   leaves a project whose README, `AGENTS.md` and `SCALING.md` describe only
   what it uses. Nothing from an unchosen option remains: CI checks that for
   every combination.
3. Commit, push, and follow the rest of this README, which is now the
   project's own.

Or let a coding agent do steps 2 and 3: open Claude Code, OpenAI Codex or
another agent that reads `AGENTS.md` in the new repository and describe the
app ("a Postgres API for a mobile app, with teams and file uploads").
`AGENTS.md` tells it to check the checkout, ask about anything
missing, point out what the template doesn't support, and confirm the
`make init` command with you before running it.

The modules:

| Module | Adds |
|---|---|
| `workspaces` | Shared workspaces with owners, admins and members, and email invitations, for apps where teams share data |
| `admin` | Admin pages (or endpoints) to find and manage users and retry failed jobs |
| `audit` | Security events (logins, role changes, …) kept in a table, with an admin-only log |
| `uploads` | File uploads to local disk behind a storage interface, with type checks and owner-only downloads |
| `metrics` | Prometheus metrics and Go's profiler on an internal port |
| `errreport` | Error reports to Sentry, GlitchTip or Bugsink |

Every project also starts with the notes example, which `make
remove-example` deletes once it has served as a pattern.

### Which database should I pick?

| | SQLite (default) | PostgreSQL |
|---|---|---|
| Pick it for | one server, a few hundred to a few thousand users, more reads than writes | apps you expect to run on several servers, heavy concurrent writes, or that need Postgres features (full-text search, JSONB) or a managed database |
| Runs as | a file next to the app; nothing else to run | a database server: a container on the VPS, or a managed service |
| How far it goes | one machine. With WAL mode it handles hundreds of requests a second; writes happen one at a time, which only hurts under sustained heavy writing | as far as the apps here are likely to go: a bigger server, a connection pooler and read replicas before anything harder |
| Backups | snapshots by a command, and Litestream streaming every change to S3-compatible storage | `pg_dump` by a script, copied to S3-compatible storage with rclone |
| When it's outgrown | move to PostgreSQL: the queries are portable, so it's a configuration and data-copy job (`SCALING.md`) | `SCALING.md`: pooling, replicas, managed Postgres |

Start with SQLite unless you already know the app needs more than one
server. Choose PostgreSQL from the start when a client requires a managed
database or the app will take many writes from many users at once.

## Working on the template

`tools/readysetgo/README.md` explains the markers init uses. `make
template-check DB=… UI=… MODULES=…` initialises a temporary copy and runs
every check on it; the Template workflow runs all combinations in CI.

---

*The rest of this README becomes the new project's.*
<!-- readysetgo:end -->

<!-- readysetgo:begin ui=html -->
A Go web app: server-rendered pages with HTMX, accounts, background jobs and
email, served behind Caddy.
<!-- readysetgo:end -->
<!-- readysetgo:begin ui=api -->
A Go JSON API: accounts with session cookies or bearer tokens, background
jobs and email, served behind Caddy.
<!-- readysetgo:end -->
`DECISIONS.md` explains why it's built this way, `AGENTS.md` holds the
conventions for working on it, and `SCALING.md` is the playbook for when it
grows.

## What's included

- **Accounts:** registration, login with "remember me", logout, email
  verification, password reset, changing email and password, and account
  deletion. Passwords are hashed with argon2id; sessions live in the
  database.
<!-- readysetgo:begin ui=api -->
  Browsers use a session cookie; other clients get a bearer token from
  `POST /api/auth/token`.
<!-- readysetgo:end -->
- **Security:** CSRF protection, `__Host-` cookies, strict security headers
  (Content-Security-Policy included), rate limits on logins and emails,
  and security events (failed logins, role changes, …) in the logs.
- **Roles:** `user` and `admin`; `user promote EMAIL` makes the first
  admin.
- **Background jobs** in the database, safe with several instances, and
  scheduled tasks that run once per interval.
- **Email** through SMTP, sent by a job; Mailpit catches it in development.
<!-- readysetgo:begin db=sqlite -->
- **SQLite** in WAL mode, with snapshot backups and optional Litestream
  replication.
<!-- readysetgo:end -->
<!-- readysetgo:begin db=postgres -->
- **PostgreSQL 18**, with read-replica support and a backup script.
<!-- readysetgo:end -->
<!-- readysetgo:begin db=sqlite -->
- **Caddy** in front, with automatic HTTPS and compression.
<!-- readysetgo:end -->
<!-- readysetgo:begin db=postgres -->
- **Caddy** in front, with automatic HTTPS, compression and load balancing
  across app instances.
<!-- readysetgo:end -->
- **Docker Compose** for development services and production; a small,
  non-root image published to GitHub's container registry by CI.
- **Tooling:** Make, live reload (air), golangci-lint, sqlc, goose
  migrations, and GitHub Actions for lint, tests and images.
<!-- readysetgo:begin module=workspaces -->
- **Workspaces:** shared workspaces with owners, admins and members, and
  emailed invitations.
<!-- readysetgo:end -->
<!-- readysetgo:begin module=admin -->
- **Admin:** find and manage users, and retry failed jobs.
<!-- readysetgo:end -->
<!-- readysetgo:begin module=audit -->
- **Audit log:** every security event kept in a table, with an admin-only
  log.
<!-- readysetgo:end -->
<!-- readysetgo:begin module=uploads -->
- **Uploads:** users' files on local disk behind a storage interface, with
  type checks and owner-only downloads.
<!-- readysetgo:end -->
<!-- readysetgo:begin module=metrics -->
- **Metrics:** Prometheus metrics and Go's profiler on an internal port.
<!-- readysetgo:end -->
<!-- readysetgo:begin module=errreport -->
- **Error reports** to Sentry, GlitchTip or Bugsink.
<!-- readysetgo:end -->
<!-- example:begin -->
- **The notes example:** a small feature showing the pattern from migration
  to test. `make remove-example` deletes it.
<!-- example:end -->

## Prerequisites

- Go 1.27 or newer
- Docker with Compose, for the development services and the production
  stack
- `make`
- For changing SQL: `make tools` installs the pinned sqlc, air and
  golangci-lint into `bin/` (sqlc needs a C compiler)

## Run it locally

```bash
cp .env.example .env
```

```bash
make services
```

```bash
make run
```

Then open <http://localhost:8080>. `make seed` adds two users to log in
with, `admin@example.com` and `user@example.com`, both with the password
`dev-password-123`. `make dev` runs the app with live reload instead, and
`make help` lists every command.

`make services` starts:

- Mailpit, which catches the app's email: read it at <http://localhost:8025>
<!-- readysetgo:begin db=postgres -->
- Postgres
<!-- readysetgo:end -->

If another project already uses one of these ports (or the app's 8080),
change it in `.env`; AGENTS.md ("Local ports") lists the settings that move
together.

`make up` runs the production stack (the app behind Caddy) on your machine
at https://localhost, with Caddy's own certificate authority.

## Tests

```bash
make test
```

runs every test with the race detector, against a real database: each test
gets its own.
<!-- readysetgo:begin db=postgres -->
They need `make services` running and `TEST_DATABASE_URL` set (it is, in
`.env.example`).
<!-- readysetgo:end -->
`make check` runs everything CI runs: lint, `go mod tidy`, generated code,
known vulnerabilities and the tests. Run it before pushing.

## The database

<!-- readysetgo:begin db=sqlite -->
The database is a SQLite file (`DATABASE_URL`, `data/readysetgo.db` in
development, `/data/readysetgo.db` in the container). It runs in WAL mode
with one writer at a time, which suits one server with modest writes very
well. It can't be shared by two servers: when the app needs more than one,
`SCALING.md` describes the move to a database server.
<!-- readysetgo:end -->
<!-- readysetgo:begin db=postgres -->
The database is PostgreSQL 18 (`DATABASE_URL`). Each instance keeps a pool
of up to `DB_MAX_OPEN_CONNS` connections (25), and `DATABASE_READ_URL` can
point reads at a replica.
<!-- readysetgo:end -->

Migrations live in `migrations/` and are embedded in the binary; the app
applies pending ones when it starts (`MIGRATE_ON_START`).

```bash
make migration NAME=add_things
```

creates the next one; write its `-- +goose Up` and `-- +goose Down`
sections, then run `make generate` after changing any SQL. `make migrate`
applies pending migrations without starting the app. Changes follow
expand/contract: add the new column or table, switch the code over in a
later deploy, and only then remove the old one, so a deploy never breaks
the running version.

## Deploying to a VPS

Any Linux server with Docker works; 1 CPU and 1 GB of memory is plenty to
start.

1. Point the domain's DNS at the server and open ports 80 and 443 (TCP,
   and UDP 443 for HTTP/3).
2. Install Docker Engine with the Compose plugin on the server.
3. Create the deployment folder (`/srv/readysetgo` by default) and in it a
   `.env` (`chmod 600`: it holds the secrets) with the production values of
   everything in `.env.example`: at least `APP_ENV=prod`, `APP_DOMAIN`, the
   `SMTP_` settings
<!-- readysetgo:begin db=postgres -->
   and a strong `POSTGRES_PASSWORD`.
<!-- readysetgo:end -->
<!-- readysetgo:begin db=sqlite -->
   and, for backups off the server, the `LITESTREAM_` settings.
<!-- readysetgo:end -->
4. CI pushes an image to `ghcr.io` on every push to `main`. If the
   repository is private, log the server in to read it:
   `docker login ghcr.io` with a token that can read packages.
5. From your machine:

   ```bash
   make deploy DEPLOY_HOST=you@server
   ```

   It copies `compose.yaml` and `deploy/` to the server, pulls the image and
   restarts. Run it again for every release. Caddy gets the HTTPS
   certificate by itself.
6. Make yourself an admin once you've registered:

   ```bash
   docker compose exec app /app user promote you@example.com
   ```

7. Schedule backups (below), and point an uptime monitor such as Uptime
   Kuma or UptimeRobot at `https://your-domain/healthz`.
<!-- readysetgo:begin module=errreport -->
8. Set `SENTRY_DSN` in the server's `.env` to a Sentry, GlitchTip or
   Bugsink project to get every logged error as a report.
<!-- readysetgo:end -->

`/healthz` answers while the process runs; `/readyz` also checks the
database, and fails during shutdown so Caddy stops sending requests first.

<!-- readysetgo:begin module=metrics -->
### Collecting metrics

Set `METRICS_ADDR=:9090` in the server's `.env`. Only containers on the
stack's network can reach that port. Add a Prometheus service there (in a
`compose.override.yaml` next to `compose.yaml`), scraping every app
instance:

```yaml
services:
  prometheus:
    image: prom/prometheus # pin the current version
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml:ro
```

```yaml
# prometheus.yml
scrape_configs:
  - job_name: app
    dns_sd_configs:
      - names: [app]
        type: A
        port: 9090
```

Point Grafana (or Grafana Cloud, with Prometheus remote-write) at it for
dashboards. `http://app:9090/debug/pprof/` serves Go's profiler on the same
port.
<!-- readysetgo:end -->

## Backups

<!-- readysetgo:begin db=sqlite -->
`deploy/backup.cron.example` takes a snapshot every night (the app's
`backup` command, which keeps the newest 7 in the data volume). For copies
off the server, turn on Litestream: set `COMPOSE_PROFILES=litestream` and
the `LITESTREAM_` settings in `.env`, and it streams every change to
S3-compatible storage (Backblaze B2, Cloudflare R2, Hetzner, AWS S3).

Restore a snapshot (it replaces the database):

```bash
docker compose stop app
docker compose run --rm app restore -from /data/backups/FILE.db
docker compose start app
```

Restore from Litestream, to the latest change it has:

```bash
docker compose stop app
docker compose run --rm litestream restore -o /data/restored.db /data/readysetgo.db
docker compose run --rm app restore -from /data/restored.db
docker compose start app
```

Every few months, practise: copy a snapshot down
(`docker compose cp app:/data/backups/FILE.db .`) and check it opens
(`APP_ENV=dev DATABASE_URL=FILE.db go run ./cmd/readysetgo migrate status`).
<!-- readysetgo:end -->
<!-- readysetgo:begin db=postgres -->
`deploy/backup.cron.example` runs `deploy/backup-postgres.sh` every night:
it writes a compressed dump into `backups/`, keeps the newest 7, and copies
it to S3-compatible storage with rclone when `BACKUP_REMOTE` names an
rclone destination (set one up with `rclone config`).

Restore a dump (it replaces what the database holds):

```bash
docker compose stop app
docker compose exec -T postgres sh -c 'pg_restore -U "$POSTGRES_USER" -d "$POSTGRES_DB" --clean --if-exists' < backups/FILE.dump
docker compose start app
```

Every few months, practise: restore the latest dump into a scratch database
and check it has recent data.

```bash
docker compose exec postgres createdb -U readysetgo drill
docker compose exec -T postgres pg_restore -U readysetgo -d drill < backups/FILE.dump
docker compose exec postgres psql -U readysetgo -d drill -c 'SELECT max(created_at) FROM users'
docker compose exec postgres dropdb -U readysetgo drill
```
<!-- readysetgo:end -->
<!-- readysetgo:begin module=uploads -->

Uploaded files are on the `data` volume (`/data/uploads`), which the
database backups don't include. Back the volume up as well, for example
with `rclone sync` from the volume's folder to the same storage.
<!-- readysetgo:end -->

<!-- readysetgo:begin module=workspaces,module=admin,module=audit,module=uploads,module=metrics,module=errreport -->
## Removing a module

Each module is a folder in `internal/` that the rest of the app reaches only
from `cmd/readysetgo/`. To remove one later, delete its folder, then let the
compiler (`go build ./...`) point at each line that used it: its field and
setup in `app.go`, its routes, and any job it registered. Also remove its
settings from `config` and `.env.example` and its entries in `sqlc.yaml`.
<!-- readysetgo:begin ui=html -->
Take its link out of the nav in `internal/web/templates/layout.html`.
<!-- readysetgo:end -->
If its migration has already run anywhere, add a migration that drops its
tables instead of deleting that one.
<!-- readysetgo:end -->
<!-- example:begin -->

## The notes example

`internal/notes/` is a complete small feature, from migration and queries
to handlers and tests: copy its shape for new features. When it has served
its purpose, before the first deploy:

```bash
make remove-example
```
<!-- example:end -->
