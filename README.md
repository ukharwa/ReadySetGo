# ReadySetGo

<!-- readysetgo:begin template -->
**Ready, set, Go: a production-ready Go starter with auth, SQL and Caddy,
built to scale from one server to many.**

[![CI](https://github.com/ukharwa/ReadySetGo/actions/workflows/ci.yml/badge.svg)](https://github.com/ukharwa/ReadySetGo/actions/workflows/ci.yml)
[![Template](https://github.com/ukharwa/ReadySetGo/actions/workflows/template.yml/badge.svg)](https://github.com/ukharwa/ReadySetGo/actions/workflows/template.yml)

Every new app needs the same foundation before the interesting part
starts: accounts, a database, security headers, email, background jobs,
HTTPS, Docker, CI, backups. ReadySetGo is that foundation, done properly
and tested, with no business logic in the way. Pick a database and a UI
style, run one command, and start on your features.

It's plain Go: the standard library first, a handful of small,
well-maintained libraries, and no framework to learn.

## What you get

- 🔐 **Accounts done right.** Sign-up, login with "remember me", email
  verification, password reset, email and password changes, a name and
  time zone, a list of signed-in devices to sign out, and account deletion. Argon2id hashing, database-backed sessions, and bearer tokens
  for API clients.
- 🗄️ **SQLite or PostgreSQL.** Typed queries with sqlc, migrations with
  goose, UUIDv7 keys, and backups for either (Litestream or `pg_dump`).
- 🖥️ **Pages or an API.** Server-rendered HTML with HTMX, or a JSON API for
  a React app or mobile client. You choose; the other is removed.
- 🛡️ **Secure by default.** CSRF protection, strict security headers and
  Content-Security-Policy, `__Host-` cookies, rate limits, and a log of
  security events.
- ⚙️ **Background jobs and email.** A database-backed job queue with
  scheduled tasks, safe on many instances, and email sent through it.
- 🚀 **Ready to deploy.** A small non-root Docker image, Compose for
  development and production, Caddy with automatic HTTPS, and one-command
  deploys to any VPS.
- 📈 **Ready to grow.** It runs on one server today, and moving to several
  is configuration, not a rewrite: checked with two instances behind
  Caddy. [`SCALING.md`](SCALING.md) is the playbook.
- 🧰 **Tooling included.** Make, live reload, golangci-lint, real-database
  tests, and GitHub Actions for lint, tests and images.
- 🤖 **Works with coding agents.** [`AGENTS.md`](AGENTS.md) teaches Claude
  Code, OpenAI Codex and others how the project works, and walks them
  through setting up a new one.

Optional modules add what some apps need and others don't:

| Module | Adds |
|---|---|
| `workspaces` | Teams that share data: owners, admins and members, with email invitations |
| `admin` | Admin pages (or endpoints) to manage users and retry failed jobs |
| `audit` | A permanent log of security events, with an admin view |
| `uploads` | File uploads with type checks and owner-only downloads |
| `metrics` | Prometheus metrics and Go's profiler on an internal port |
| `errreport` | Error reports to Sentry, GlitchTip or Bugsink |
| `twofactor` | Two-factor login with an authenticator app, and recovery codes |
| `social` | Logging in with Google or GitHub |

Every project also starts with a small notes feature that shows the whole
pattern, from migration to test. `make remove-example` deletes it when
you're ready.

## Quick start

You need Go 1.27 or newer and Docker.

1. Click **Use this template** on GitHub to create your repository, and
   clone it.
2. Choose your setup:

   ```bash
   make init NAME=myapp MODULE=github.com/you/myapp DB=sqlite UI=html MODULES=workspaces,admin
   ```

3. Run it:

   ```bash
   cp .env.example .env
   ```

   ```bash
   make services
   ```

   ```bash
   make run
   ```

   Open <http://localhost:8080>, and `make seed` adds two users to try it
   with.

That's it. `make init` keeps only what you chose, renames everything to
your app, and leaves a README, `AGENTS.md` and `SCALING.md` that describe
your project and nothing else. CI checks that for every combination of
options.

### Or let your coding agent do it

Open Claude Code, Codex or another agent in your new repository and
describe the app: "a Postgres API for a mobile app, with teams and file
uploads". `AGENTS.md` has it check your setup, ask about anything missing,
tell you what the template doesn't do, and confirm the `make init` command
with you before running it.

With Claude Code you can skip the clone too. Add the plugin once:

```bash
claude plugin marketplace add ukharwa/ReadySetGo
```

```bash
claude plugin install readysetgo@readysetgo
```

Then, in a session anywhere, run `/readysetgo:new` with a description of
the app. It creates the repository from the template (with `gh`, or as a
plain clone), then sets it up the same way.

## Choose your setup

| Option | What it sets | Default |
|---|---|---|
| `NAME` | your app's name: lowercase letters, digits and hyphens | required |
| `MODULE` | the Go module path, usually `github.com/you/NAME` | required |
| `DB` | `sqlite` or `postgres` | `sqlite` |
| `UI` | `html` (pages with HTMX) or `api` (JSON for a separate frontend or mobile app) | `html` |
| `MODULES` | any of the modules above, comma-separated, or `all` | none |

### Which database?

| | SQLite (default) | PostgreSQL |
|---|---|---|
| Best for | one server and a few thousand users, more reads than writes | several servers, heavy concurrent writes, or Postgres features such as full-text search and JSONB |
| Runs as | a file next to the app: nothing else to run | a database server: a container on your VPS, or a managed service |
| How far it goes | one machine: hundreds of requests a second, with writes one at a time | as far as you're likely to need: bigger servers, connection pooling and read replicas |
| Backups | snapshots, plus Litestream streaming every change to S3-compatible storage | `pg_dump`, copied to S3-compatible storage with rclone |
| Outgrown? | move to PostgreSQL: the queries are portable, so it's configuration and a data copy | `SCALING.md` covers pooling, replicas and managed Postgres |

Start with SQLite unless you already know the app needs more than one
server. Choose PostgreSQL from the start when a client requires a managed
database, or many users will be writing at once.

## Learn more

- [`DECISIONS.md`](DECISIONS.md): every choice, why it was made, and what
  else was considered.
- [`SCALING.md`](SCALING.md): what to do as the app grows, cheapest step
  first.
- [`AGENTS.md`](AGENTS.md): the conventions, for you and your coding agent.
- [`tools/readysetgo/README.md`](tools/readysetgo/README.md): working on
  the template itself. `make template-check DB=… UI=… MODULES=…` tests one
  combination; CI tests them all.

ReadySetGo is MIT licensed ([`LICENSE`](LICENSE)). `make init` removes
that file, so the app you build from it is yours to license as you choose.

---

*Everything below this line becomes your project's README.*
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
  verification, password reset, changing email and password, a display
  name and time zone (times are shown in it), signing out other devices,
  and account deletion. Passwords are hashed with argon2id; sessions live in the
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
- **Open or invite-only sign-up:** `REGISTRATION=invite` lets in only
  people with an invitation, which `user invite EMAIL` sends.
<!-- readysetgo:begin module=admin -->
  Admins can send them from the users page too.
<!-- readysetgo:end -->
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
<!-- readysetgo:begin module=twofactor -->
- **Two-factor login:** codes from any authenticator app, single-use
  recovery codes, and secrets encrypted with `TWO_FACTOR_KEY`.
  `user reset-two-factor EMAIL` helps someone who lost both.
<!-- readysetgo:end -->
<!-- readysetgo:begin module=social -->
- **Logging in with Google or GitHub**, linked to accounts by verified
  email address. People who sign up that way have no password until they
  set one with a reset link.
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
<!-- readysetgo:begin module=twofactor -->
   Add a `TWO_FACTOR_KEY` too (`openssl rand -base64 32`), and keep a copy
   somewhere safe: losing it turns off everyone's authenticator codes.
<!-- readysetgo:end -->
<!-- readysetgo:begin module=social -->
   For logging in with Google or GitHub, add their `_CLIENT_ID` and
   `_CLIENT_SECRET`, and register the callback URL `.env.example` gives
   with each, on your real domain.
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

<!-- readysetgo:begin module=workspaces,module=admin,module=audit,module=uploads,module=metrics,module=errreport,module=twofactor,module=social -->
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
<!-- readysetgo:begin module=twofactor -->
Two-factor login also has code in `internal/auth`: delete `secondfactor.go`
there and follow the compiler.
<!-- readysetgo:begin ui=html -->
Delete `templates/login_code.html` too, and the two-factor card in
`templates/account.html`.
<!-- readysetgo:end -->
<!-- readysetgo:end -->
<!-- readysetgo:begin module=social -->
Social login also has code in `internal/auth`: `social.go`, and the lines
that handle accounts without a password.
<!-- readysetgo:begin ui=html -->
The login and sign-up pages' buttons, `Provider` in `internal/web` and a
block in `app.js` go with it.
<!-- readysetgo:end -->
First make sure everyone who signed up through a provider has set a
password: without the module, they'd have no way in.
<!-- readysetgo:end -->
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
