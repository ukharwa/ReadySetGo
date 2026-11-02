# Decisions

This file records the decisions behind this project's foundation: what was
chosen, why, and what else was considered. Where the owner overrode a
recommendation, the concern is noted once and the decision then stands.

The decisions were made on 2026-10-30, in the design interview for the
kickstart template.

<!-- kickstart:begin template -->
> **Template note.** Blocks marked `kickstart:begin … kickstart:end` are
> handled by `make init`: blocks for the options you don't choose are
> removed, and the markers themselves are always removed.
<!-- kickstart:end -->

## Guiding principles

- **Small by default, ready to scale.** Everything is as simple as one
  server needs, but nothing blocks running several instances later. Moving
  from one server to many should be configuration and swapping
  implementations, not restructuring.
- **Boring technology.** Go's standard library first, then small,
  well-maintained libraries. No frameworks, ORMs or dependency-injection
  containers.
- **Idiomatic everywhere.** Go code follows Effective Go, Go Code Review
  Comments and Google's Go style guide. Every tool (Postgres, SQLite, goose,
  sqlc, Compose, Caddy, Make, GitHub Actions) is used the way its own docs
  recommend.
- **Seams only where scaling pressure lands.** Small interfaces exist for
  sessions, rate limiting, background jobs, email and file storage, and
  nowhere else.

## Database

<!-- kickstart:begin template -->
### Supported databases: SQLite and PostgreSQL

SQLite covers single-server apps with no database server to run; Postgres is
where an app goes when it grows. **Considered:** MariaDB/MySQL. Dropped,
because MariaDB was habit rather than existing infrastructure; its schema
changes can't be rolled back inside a transaction, so a failed migration
leaves a half-changed schema; sqlc supports it less well; and MySQL and
MariaDB have diverged enough that supporting both means testing both.
**Overridden:** yes. The owner picked the two-database option over keeping
MariaDB, which matched the advice given that MariaDB was habit.

### One database per project, chosen at init

`make init DB=sqlite|postgres` removes everything for the other database, so
a project carries one driver, one set of migrations and one Compose setup.
Moving databases later is a data migration either way. **Considered:**
switching at runtime through `DATABASE_URL`. Every project would carry both
drivers and query sets, and it invites "SQLite in dev, Postgres in prod",
which hides differences until production.

### Default database: SQLite

For a few hundred to a few thousand users on one VPS it is fast, needs no
server and backs up as a file. **Considered:** Postgres as the default, which
avoids a database move under pressure if an app suddenly grows, at the cost
of a database container for every small app.
<!-- kickstart:end -->

<!-- kickstart:begin db=sqlite -->
### Database: SQLite

Chosen at init. A single server with modest writes runs very well on SQLite
in WAL mode, and backups are a file. Its ceiling is one machine: two app
servers can't share it. `SCALING.md` covers moving to Postgres when that
happens. **Considered:** PostgreSQL.

### Driver: `modernc.org/sqlite`

Pure Go with no CGO, so builds are static, cross-compile easily and the
server needs no SQLite installed. It is somewhat slower than the CGO driver,
which doesn't matter at this write volume. **Considered:** `mattn/go-sqlite3`
(fastest and most proven, but needs CGO and a C toolchain) and
`ncruces/go-sqlite3` (CGO-free via WebAssembly; younger).

### Connection settings

WAL journal mode, `busy_timeout=5000`, foreign keys on, `synchronous=NORMAL`
(safe if the app crashes; a power loss can lose the last few commits). Two
pools on the same file: a write pool with one connection whose transactions
take the write lock up front (`BEGIN IMMEDIATE`), and a read pool with
several connections. This avoids `SQLITE_BUSY` errors between concurrent
writers. **Considered:** a single one-connection pool, which is simpler but
lets one slow read block every write.

### Shutdown

`Close` refreshes the query planner's statistics (`PRAGMA optimize`) and
relies on SQLite folding the write-ahead log back into the database file
when the last connection closes. **Considered:** an explicit
`wal_checkpoint(TRUNCATE)`, which would briefly block the server's writers
every time another process (such as `app backup`) exits.

### Backups

`app backup` writes a consistent, timestamped copy with `VACUUM INTO` and
keeps the last N, with no `sqlite3` tool needed in the image. `app restore`
puts a snapshot back: it checks the snapshot with SQLite's integrity check
and swaps the file in atomically, running as the app's own user (a plain
`cp` from another container leaves the file owned by root, and the app then
can't open it). Litestream 0.5 can stream changes continuously to
S3-compatible storage, configured by one `LITESTREAM_REPLICA_URL`; its
restore produces a file that `app restore` then puts in place. Backups are
scheduled by host cron, and the README has a restore drill. Copying the
`.db` file while the app runs is not safe in WAL mode.
<!-- kickstart:end -->

<!-- kickstart:begin db=postgres -->
### Database: PostgreSQL 18

Chosen at init. Version 18 is supported by every managed host; nothing here
needs anything newer than about version 15. **Considered:** SQLite, which
would have meant one server only.

### Driver: pgx v5 through `database/sql`

pgx is the standard Go Postgres driver; using its `database/sql` adapter
keeps the generated query code identical in shape to the SQLite version.
**Considered:** `lib/pq`, which is in maintenance mode and recommends pgx.

### Connection settings

A pool of 25 open connections, 5-minute idle timeout and 30-minute lifetime,
all overridable. Postgres allows 100 connections by default, leaving room
for 2 or 3 instances; `SCALING.md` covers pgbouncer beyond that. Reads and
writes use separate handles that point at the same pool until
`DATABASE_READ_URL` names a replica.

### Backups

`deploy/backup-postgres.sh` runs `pg_dump -Fc` inside the database
container (so it needs no Postgres tools on the server), keeps the last N
and, when `BACKUP_REMOTE` is set, pushes the dump to S3-compatible storage
with rclone. `pg_restore --clean` puts one back. Backups are scheduled by
host cron, and the README has a restore drill. Managed Postgres hosts offer
point-in-time restore instead.
<!-- kickstart:end -->

### Data access: `database/sql` + sqlc

Queries are plain SQL; sqlc generates typed Go functions from them, with no
runtime reflection or query builder. Each feature gets its own generated
package (`internal/<feature>/store`) containing only the structs its own
queries use, so a feature or module can be removed by deleting its folder,
with no regeneration. Queries that work the same on every database are
shared; the few that can't be (job claiming) live in per-database files. **Considered:** sqlx (one query string for all
databases and easy dynamic queries, but column mismatches only show up at
runtime) and ORMs such as GORM, ent or Bun (database differences handled for
you, at the cost of hidden behaviour: GORM's struct updates silently skip
zero values, N+1 queries are easy to miss, and its auto-migrations don't
suit zero-downtime deploys). Known weak spot: queries built at runtime are
awkward in sqlc; the rare one is hand-written with `database/sql`.

### Primary keys: UUIDv7, generated in Go

IDs can't be guessed or counted from URLs, which turns a missing permission
check from trivial to exploit into hard. The ID is known before the insert,
and UUIDv7's time prefix keeps index inserts in order. Uses
`github.com/google/uuid`, because the standard library has no UUID type.
**Considered:** auto-increment integers with `RETURNING`, which are smaller
and easier to read but would later need a second "public ID" column.

### Migrations: goose v3, embedded, run on startup

Plain SQL migrations embedded in the binary; Go migrations are available for
data backfills. Migrations run on startup (`MIGRATE_ON_START=true`) and
through `app migrate`. On Postgres an advisory lock makes concurrent
instances safe: waiting instances retry every second for up to 10 minutes,
matching the migration timeout (goose's default, every 5 seconds for 5
minutes, could give up behind a long migration). A failed migration stops
the app starting. Migrations follow
expand/contract (add first, switch code, remove later) for zero-downtime
deploys. Files use goose's sequential numbering, one per feature or module.
**Considered:** golang-migrate (a failed migration leaves a "dirty" state to
fix by hand), Atlas (schema diffing; heavier, some features paid), and
migrations only as an explicit deploy step (cleaner privilege separation;
documented in `SCALING.md` as a later step).

### Seed data: `app seed`

Uses the real queries and password hashing, only runs when `APP_ENV=dev`,
and is safe to rerun. **Considered:** SQL seed files, which can't hash
passwords and drift away from the real queries.

### Off-server backups: S3-compatible storage

A backup on the same VPS dies with the VPS. **Considered:** provider
snapshots plus local dumps, which keep everything with one provider.

### Conventions

- Timestamps are UTC and set from Go, not by database defaults. (SQLite's
  `CURRENT_TIMESTAMP` writes a different text format than the Go driver,
  and mixed formats break sorting.)
- Emails and usernames are trimmed and lowercased in Go before saving or
  lookup, with a unique index on the cleaned value. No `CITEXT` or
  `COLLATE NOCASE`.
- Strings are `TEXT`; length limits are checked in Go.
- Money is stored as whole cents, never floats.
- Foreign keys are always declared with an explicit `ON DELETE` rule.

## Core

### Go 1.27

The current stable release. Go only supports its two newest releases, so a
template starts on the newest.

### Module path: chosen at init

`make init MODULE=…`, defaulting to `github.com/ukharwa/<NAME>`, because
some apps live under a different GitHub account.

### Routing: standard library `net/http` ServeMux

Method and path-variable matching has been built in since Go 1.22, and
conflicting routes panic at startup. Middleware is plain function wrapping.
**Considered:** chi, which adds route groups and middleware stacks but is
mostly convenience since Go 1.22; switching later keeps handlers unchanged.

<!-- kickstart:begin template -->
### UI mode: chosen at init

`make init UI=html|api` keeps one handler layer. Authentication logic lives
in packages with no HTML or JSON in them; each mode is a thin layer on top.
**Overridden:** yes. The recommendation was HTML pages plus small JSON
helpers in every project; the owner chose an init option because they build
both kinds of app. This fits the init-time pruning model, so no concern.
<!-- kickstart:end -->

<!-- kickstart:begin ui=html -->
### Pages: server-rendered `html/template` + HTMX

One codebase, no frontend build step, and cookie sessions plus CSRF
protection stay simple. HTMX (2.0.11, checked against npm's published
SHA-512) is copied into the repo and embedded rather than loaded from a CDN,
and configured not to inject styles or use `eval`, so the
Content-Security-Policy stays strict. Every template is parsed at startup
and a test renders every page, which catches template errors early. Forms
post, then redirect, carrying a one-off "flash" message in a short-lived
cookie. Error pages are a standalone template, so they render even when the
rest of the app can't. **Considered:** templ (type-checked components, but
a code-generation step and a dependency).

### Static assets: embedded, cache-busted

Templates and static files are embedded with `embed` and read from disk in
dev. URLs carry a content hash (`/static/app.css?v=<hash>`), so browsers
cache for a year and never see stale files. CSS is a small hand-written
stylesheet with no Node or build step. **Considered:** Pico CSS (polished
instantly, but its look has to be undone later) and Tailwind's standalone
binary (utility classes, but a CSS build step).
<!-- kickstart:end -->

<!-- kickstart:begin ui=api -->
### API: JSON

JSON handlers under `/api/`. Errors use RFC 9457 problem details
(`application/problem+json`), with the request ID and per-field errors as
extensions. Links in verification and reset emails point at `APP_URL`, the
frontend's address; the frontend sends their token to the API. A browser
frontend is served from the same domain as the API (Caddy sends `/api/*` to
Go), so there is no CORS setup and cookies stay `SameSite=Lax`.
**Considered:** a separate `APP_FRONTEND_URL` setting, which is redundant
when the frontend and API share a domain.
<!-- kickstart:end -->

### Configuration: environment variables only

The app reads only real environment variables; `.env` is loaded by Make and
Compose. Dev and production behave the same, there's no dependency, and a
stray `.env` on a server can't silently override production settings.
`config.Load` checks everything at startup and reports every problem at
once. **Considered:** `godotenv` in the app (convenient for `go run` and
debuggers, with the stray-file risk), `caarlos0/env` (struct tags, one more
dependency), and viper or koanf (far more than environment variables need).

### Logging: `log/slog`

JSON in production, text in dev. Every request gets an ID (or keeps Caddy's
`X-Request-ID` if well-formed), which every log line carries, the response
returns and error pages show. The app writes one access-log line per
request; Caddy logs errors only. Passwords, tokens, cookies, emails and
query strings are never logged, because reset links carry tokens in URLs.
**Considered:** zerolog and zap, which are faster, but logging speed won't
be the bottleneck.

### One binary with subcommands

`serve`, `worker`, `migrate`, `seed` and `user`,
<!-- kickstart:begin db=sqlite -->
plus `backup` and `restore` for the database file,
<!-- kickstart:end -->
parsed with the standard library, so the server, worker and migrations can
run as one process now and separately later. No cobra.

## Authentication

### Sessions stored in the database

A random 128-bit token in an HttpOnly cookie; the database stores only its
SHA-256 hash, so a leaked database or backup can't be used to log in.
Sessions expire after inactivity and after a fixed maximum age: a browser
session after 24 idle hours or 7 days, a "remember me" session after 30
days. Every login issues a new token, logout deletes the session, and
password changes log out other sessions. "Last seen" is written at most
every 5 minutes. The `SessionStore` interface in the auth package is the
seam; its default keeps sessions in the database and reads them from the
primary, never a replica, so a new session is found straight away.

<!-- kickstart:begin ui=api -->
### API clients: cookies for browsers, bearer tokens for everything else

Browser frontends use the same cookie sessions. Mobile apps, CLIs and
scripts use plain random bearer tokens (30 idle days, 90 days at most),
stored hashed in the same table and revocable the same way. The table is the
same in both UI modes, so the schema doesn't depend on the mode.
**Considered:** bearer tokens only, which means a browser app keeps its
token where JavaScript (and any XSS bug) can read it.
<!-- kickstart:end -->

### No JWT

A JWT can't be revoked before it expires without a server-side blocklist,
which is a session store again, and JWT libraries have a history of bugs
where an attacker picks the signing algorithm. JWT suits other services
checking tokens without calling the app, which isn't this setup.

### Password hashing: argon2id

`golang.org/x/crypto/argon2` with OWASP's settings (19 MiB, 2 iterations,
1 thread), stored in the standard `$argon2id$…` format so settings can be
raised later and old hashes upgrade at the next login. A per-instance limit
on concurrent hashes stops a burst of logins exhausting memory.
**Considered:** bcrypt at cost 12 (simpler and proven, but not memory-hard
and limited to 72-byte passwords).

### Password rules

12 to 256 characters, no composition rules (NIST guidance). The minimum is
one setting; NIST's latest guidance asks for 15 when a password is the only
factor. A breached-password check (Have I Been Pwned) is skipped for now.

### Account flows

- Register never reveals whether an email has an account: if it does, the
  owner gets an email instead.
- Login and logout, with "remember me": unchecked ends with the browser and
  expires after a day idle; checked lasts 30 days.
- Email verification doesn't block login; features opt in with
  `auth.RequireVerified`. **Considered:** blocking login until verified, which
  suits apps where fake accounts do damage.
- Password reset: a single-use link valid for 1 hour, the same reply whether
  or not the email exists, and every session logged out afterwards.
- Change password, change email (confirmed at the new address, notice to the
  old), and delete account all ask for the current password again.
- Account deletion is a hard delete. **Considered:** soft delete, which
  means filtering deleted users out of every query forever.
- Verification and reset tokens are random (128 bits), stored hashed,
  single-use and time-limited. They are found by looking up their SHA-256
  hash, so there is no comparison to time: the brief's "constant-time token
  comparison" is met by never comparing raw tokens. (Password hashes, which
  are compared, use `subtle.ConstantTimeCompare`.)
- Emailed links open a page with a button; only submitting it acts. Mail
  scanners that open links therefore can't use them up.
- A password reset also confirms the email address, since opening the link
  proves access to the inbox.
- Register and login take the same time whether or not the account exists:
  register hashes the password either way, and login checks a dummy hash
  for unknown addresses.

### Roles: a `role` column

`user` or `admin`, with `auth.RequireRole("admin")` middleware and
`app user promote|demote|disable|enable|delete` so the first admin never needs a
default password. **Considered:** full role-based permissions (RBAC), which apps
this size don't need yet.

### Left out: two-factor login and social login

Most of these apps don't need them, and neither is small. **Considered:** an
authenticator-app (TOTP) module, passkeys, and a "Sign in with Google" style
module.

## Security

### CSRF: `http.CrossOriginProtection` + `SameSite=Lax`

The standard library (Go 1.25+) rejects state-changing requests a browser
marks as cross-site, using `Sec-Fetch-Site` or `Origin`. No tokens in forms
or JavaScript. GET requests never change state. **Considered:** classic CSRF
tokens (`gorilla/csrf` or hand-written), which also cover very old browsers
at the cost of a token in every form and request.

### Cookies: `__Host-session`, HttpOnly, Secure, SameSite=Lax

The `__Host-` prefix stops a compromised subdomain from setting or
overwriting the session cookie. **Considered:** `SameSite=Strict`, which
makes people arriving from an email link look logged out.

### Security headers, set by the app

A strict Content-Security-Policy (no inline scripts or styles; JSON under
`/api/` gets `default-src 'none'`), HSTS in production (no preload, which is
hard to undo), `nosniff`, `Referrer-Policy: strict-origin-when-cross-origin`,
`Cross-Origin-Opener-Policy: same-origin`, and `Cache-Control: no-store` by
default, so private pages never linger in a browser's cache; static files
override it. Set in Go middleware so they're tested and survive a change of
proxy. **Considered:** setting them in the Caddyfile, and `no-store` only on
pages known to be private (easy to forget on a new one).

### Rate limiting, no hard lockout

Per IP (20 a minute across auth endpoints), per email and IP together (5
failed logins per 15 minutes), per email alone (50 failed logins an hour),
and per address emailed (3 an hour), answered with `429` and `Retry-After`.
Failed-login counters reset after a successful login, so only consecutive
failures count. If the counter store fails, requests are refused (`503`)
rather than let through. No hard lockout, which would let anyone lock out
any user. The client IP
comes from `X-Forwarded-For` only when the request arrives from
`TRUSTED_PROXIES`. The default store is in memory and per instance, so two
instances allow up to twice the attempts; `SCALING.md` covers a shared
store. **Considered:** database-backed counters from the start.

### Security events: logged by the account service

Logins and failed logins, password resets and changes, email changes, new
accounts, role changes, and disabling and deleting accounts are each logged
as a `security event` line: the event, the user's ID, the acting user's ID,
a detail such as the new role, the IP address and the request ID, never an
email address. The account service records them, so the CLI's changes are
covered too, and `OnEvent` hooks receive each one inside the change's
transaction. **Considered:** logging in the handlers, which would miss the
CLI.

### Email: SMTP through the job queue

An email-sender seam with a standard-library SMTP implementation and
plain-text templates; header injection is blocked. Mailpit catches mail in
dev. Without `SMTP_HOST`, dev logs emails instead of sending them;
production refuses to start without it, and refuses unencrypted SMTP
(`SMTP_TLS=none`) except to a relay on the same machine. Emails are sent by
the background job queue, which removes a timing leak (an unknown email
would otherwise answer faster), keeps slow SMTP out of requests, and
retries failures. A queued email holds the whole message, so a link with a
token sits in the jobs table until it is sent, usually seconds.
**Considered:** `wneessen/go-mail` (for attachments or complex multipart
mail), sending during the request (timing leak, lost mail on failure), and
an `EMAIL_DRIVER` setting (redundant: whether `SMTP_HOST` is set already
says which sender to use).

## Infrastructure

### Docker Compose in dev and production

CI pushes images to GHCR, named after the module (`make init` sets
`ghcr.io/<owner>/<repo>`). `make deploy` copies `compose.yaml` and
`deploy/` to the server and runs `docker compose pull && docker compose up
-d` there. Secrets live in a server-side `.env` with mode 600; compose.yaml
sets what follows from the container itself (`HTTP_ADDR`, `APP_URL` from
`APP_DOMAIN`, `DATABASE_URL`, and `TRUSTED_PROXIES` as Docker's private
ranges, safe because the app publishes no port). Caddy holds requests for
up to 10 seconds while the container restarts: recreating the app under
steady traffic gave no errors, the slowest request taking half a second. **Considered:** a plain binary
under systemd (no Docker daemon, less memory, but Caddy and Postgres are
installed and upgraded by hand, and dev differs from production).

### Image: two-stage build, distroless static, non-root

A few tens of megabytes on distroless Debian 13, with no shell or package
manager; maintenance runs as app
subcommands. Every base image is pinned by digest and kept current by
Dependabot. The build stage cross-compiles, so `amd64` and `arm64` images
build without emulation. **Considered:** `scratch` (copy
certificates and timezone data by hand) and Alpine (a shell for debugging,
more to attack).

### Caddy

Automatic HTTPS in production, compression, request IDs, error-only
logging, and (in API mode) a commented example for a same-domain frontend,
checked with `caddy validate`. Caddy finds the app containers through
Docker's DNS (`dynamic a`), balances them by fewest connections and sends
requests only to instances whose `/readyz` answers, so one instance or
several (`docker compose up --scale app=3`) need no edit. A plain
`reverse_proxy app:8080` was the first version: with two instances it sent
every request to one of them. No ACME email:
Let's Encrypt stopped sending expiry notices in 2025. In dev the app runs
directly on `localhost`, which Chrome and Firefox treat as secure. To test
the full stack over HTTPS (or in Safari), `make up` runs the production
stack locally with `APP_DOMAIN=localhost`, for which Caddy uses its own
certificate authority. Rootless Docker can't bind ports below 1024, so locally
`HTTP_PORT` and `HTTPS_PORT` move Caddy to 8081 and 8443. **Considered:**
Caddy for local HTTPS by default, which matches production exactly but
needs every machine to trust Caddy's local certificate.

### Health endpoints

`/healthz` says the process is alive; `/readyz` checks the database (2-second
timeout) and fails during shutdown. Plain text with no details, so safe to
leave public. They bypass the middleware stack, so the constant polling
doesn't flood the access log. No Docker `HEALTHCHECK`: the image has no shell and Docker
doesn't restart unhealthy containers anyway. **Considered:** JSON with each
dependency's status, which reveals internals.

### Server timeouts and graceful shutdown

5 seconds for request headers, 15 to read a request, 30 to write a response,
120 for idle connections; 1 MB body and header limits (a route can raise
its body limit). Every request's context gets a 25-second deadline, so
database and outbound calls can't outlive the response. On SIGTERM: readiness
fails, an optional drain period passes (`SHUTDOWN_DRAIN`), in-flight requests
get up to 25 seconds, the worker finishes or releases its jobs, and the
database closes. Compose's stop timeout is 30 seconds. **Considered:**
shorter limits, which risk cutting off slow requests.

## Scaling path

### Seams

Built now: session store (database), rate limiter (memory), background jobs
(database), email (SMTP and log). File storage exists only with the uploads
module. No cache seam until a feature needs one. Each seam is an interface
with one implementation built in `main`, except jobs (below), and gets a
selecting setting only when it ships with two implementations; for email,
`SMTP_HOST` being set or not is that setting. **Considered:** a setting for
every seam now, accepting a single value.

### Checked with two instances

The template's scaling check ran a Postgres project with every module as
two app instances behind Caddy (`docker compose up --scale app=2`):
- Requests were spread over both, and a session from one worked on the
  other. Stopping one lost no requests; a request during a single
  instance's restart waited about 6 seconds and succeeded.
<!-- kickstart:begin module=uploads -->
- A file uploaded through one instance downloaded from both (the shared
  `data` volume; several servers need the S3 step).
<!-- kickstart:end -->
- Migrations ran once, the other instance finding nothing to do (an
  advisory lock guards a simultaneous start, as `internal/db` tests).
  Scheduled tasks were enqueued once per period, and a job (an email) ran
  once.
- Rate limits are per instance, as documented: two instances allowed 9
  wrong passwords for one account before refusing, where one allows 5.

SQLite projects stay on one instance: the database is a file one process
writes.

### Job queue: hand-written, database-backed, core

One `jobs` table. Postgres claims jobs with `FOR UPDATE SKIP LOCKED`; SQLite
with a single `UPDATE … RETURNING`. A claimed job is leased for 5 minutes
and its handler gets 4, so a handler that respects its context finishes
before another worker could claim it again; if the instance dies, the job
is retried once the lease runs out. Failures retry with doubling delays,
10 attempts over about 17 minutes, then the job is marked failed. Jobs can
occasionally run twice, so handlers must be safe to repeat. The worker
polls every second with `WORKER_CONCURRENCY` goroutines (default 4) and
runs inside `serve` by default (`WORKER_ENABLED`), or alone as
`app worker`.

The seam is the package API (`Enqueue`, `Worker`, `Scheduler`) rather than
a Go interface, because `Enqueue` joins the caller's database transaction
(a job exists only if the change that caused it commits), which only a
database-backed queue can do. **Considered:** River (excellent,
Postgres-only) and a dedicated queue such as NATS or RabbitMQ (a later
step, behind the same package API).

### Scheduled tasks: core, deduplicated by a unique key

Tasks are declared in code (`scheduler.Every(24*time.Hour, "jobs.cleanup")`).
Every instance tries to enqueue each run, and a unique index on task name
plus scheduled time means only one succeeds. Completed scheduled jobs are
kept for 7 days, which keeps their key taken so a run can't repeat, so
intervals up to 7 days are supported; other jobs are deleted on success.
**Considered:** a cron library plus an advisory lock, which needs a
different lock per database.

### `SCALING.md`

A playbook ordered cheapest first: measure; indexes and pool tuning; a
bigger server; SQLite to Postgres; a separate worker; several app
instances; shared rate limits; object storage for uploads; migrations as a
deploy step; managed Postgres with a pooler; read replicas; a cache; then,
rarely, sessions in a shared store, a dedicated job queue, an email
provider's API, tracing, and splitting out a feature. Each step gives its
signs, the configuration to change and the code to touch. Init keeps the
steps that apply (the move to Postgres only in SQLite projects), so the
steps aren't numbered. Because it describes what the app may grow into, the
leftover check lets it name the other database and modules.

## Developer experience

### Make

Already installed everywhere. `make tools` installs sqlc, air and
golangci-lint at fixed versions into `./bin` with `go install`, which Go's
checksum database verifies; CI uses golangci-lint's official Action, which
downloads its release binary. Generated sqlc code is committed. `make check`
runs exactly what CI runs. **Considered:** Taskfile or just (one more tool
everywhere); Go's `tool` directive (pulls sqlc's dependencies into
`go.mod`); and, for golangci-lint, its install script piped into `sh`,
which runs unreviewed remote code on every developer's machine.

<!-- kickstart:begin template -->
### `make init` is a Go program

Shell scripts that rewrite files behave differently on Linux and macOS; a Go
program can be tested. It moves pre-generated sqlc code into place, so
running init needs nothing but Go, then runs `go mod tidy`.

Before init, the template is a runnable SQLite app. Postgres-only Go files
carry `//go:build postgres` (SQLite-only ones `//go:build !postgres`), so
they stay out of the template's own build; init deletes one set and strips
the constraint from the other. Postgres is therefore tested through
`make template-check DB=postgres`, which runs init on a temporary copy of
the working tree and then the full test suite. **Considered:** building
both databases in place with build tags, which would need build-tagged
generated code and every shared file to compile both ways.

Init writes its own command and the template commit it ran on into the
project's `CLAUDE.md`. Changing a choice later (another database, a module
added) then means running init on two scratch copies of that commit, with
the old options and the new, and applying their difference. **Considered:**
a separate tool for each kind of change, which would have to stay in step
with init.
<!-- kickstart:end -->

### Linting: golangci-lint v2

The standard set plus `bodyclose`, `rowserrcheck`, `sqlclosecheck`, `noctx`,
`errorlint`, `gosec`, `misspell` and `forbidigo` (no `fmt.Print*` or `log.*`
outside `main`), with `gofmt` and `goimports` as formatters. Tests are
exempt from `gosec` and `noctx`. Where a finding is deliberate (retry
jitter that needs no cryptographic randomness, a path the operator chose),
a `//nolint` comment names the linter and the reason. **Considered:** just `go vet` and `staticcheck`, and
gofumpt.

### Tests: the standard library against real databases

Table-driven tests with no assertion library. SQLite tests use a temporary
file each; Postgres tests copy a migrated template database. Auth flows are
tested end to end over HTTP. The race detector runs in CI. **Considered:**
testcontainers-go, which makes `go test` self-contained but adds a heavy
Docker dependency.

### CI: GitHub Actions

Lint, checks that `go.mod` and the generated code are current,
`govulncheck`, tests (with a Postgres service for Postgres projects), and on
`main` and tags an `amd64`/`arm64` image pushed to GHCR. Go's version comes
from `go.mod`. Actions are pinned to commit hashes (after the 2025
`tj-actions` attack moved a tag to malicious code), and only releases at
least two weeks old were pinned. Dependabot proposes newer Go modules,
Actions and images weekly, waiting until a release is a week old; the Go
version and the Makefile's tools are bumped by hand. The workflows are
checked with actionlint. Deploys are
run by hand so GitHub never holds a server key. **Considered:** automatic
deploys on tags, and `amd64`-only images.

<!-- kickstart:begin template -->
### Template CI matrix

Every combination of database, UI mode and modules (all or none) is created
with `make init` in a fresh copy and must pass lint and tests, which also
tests init. Each run also fails if anything from an unchosen option is left
behind. Two more runs then apply `make remove-example` and check the
project again.
<!-- kickstart:end -->

### Not included

Pre-commit hooks, a dev container, editor settings, translations, feature
flags, OpenAPI generation, payments and webhooks. Each is easy to add per
app.

## Optional modules

<!-- kickstart:begin template -->
Modules are chosen with `make init MODULES=…` (default: none) and depend
only on the core, never on each other, so any combination is valid.
<!-- kickstart:end -->

<!-- kickstart:begin module=workspaces -->
### Workspaces

Most of these apps are business tools where people share data, so the
design is settled up front:

- A user can belong to several workspaces.
- Every new user gets a personal workspace, unless they sign up through an
  invitation. A solo user's data lives in a workspace of one; inviting a
  colleague later moves nothing. Whether solo users see workspace screens
  is a per-app decision.
- Roles: `owner` (delete, transfer), `admin` (invite, remove), `member`.
  There is always at least one owner.
- Invitations are single-use links valid for 7 days, tied to the invited
  email: only an account with that address can accept, and accepting
  verifies the address, since the link arrived there. Invitations don't
  record who sent them; the email names the sender.
- The workspace ID is in the URL (`/w/{workspaceID}/…` for pages,
  `/api/workspaces/{workspaceID}/…` for the API); middleware checks
  membership. Every workspace-owned table has a required `workspace_id`,
  every query filters on it (from the request, never from form input), and
  outsiders get 404 for anything in a workspace. Members whose role doesn't
  allow an action get 403.
- Deleting a workspace is for owners only. The only owner of a workspace
  with other members must transfer ownership before deleting their account.
- The core knows nothing of workspaces: the module registers
  `OnUserCreated` and `OnUserDeleting` hooks with the account service,
  which run inside the account's own transaction.

**Considered:** one workspace per user; mixing user-owned and
workspace-owned data (every query becomes "this user or a workspace they're
in"); storing the current workspace in the session (two tabs can't use
different workspaces).
<!-- kickstart:end -->

<!-- kickstart:begin module=uploads -->
### Uploads: local disk

A storage seam with a local-disk implementation. Size limits per route, an
allowlist of file types checked from the content, random storage keys,
downloads through the app with a permission check, and non-images sent as
attachments. **Overridden:** yes. The recommendation was to ship an
S3-compatible implementation too. Concern: the S3 code gets written under
scaling pressure, and existing files must be copied to object storage at
that point. `SCALING.md` covers it.
- Files belong to the user who uploaded them; anyone else gets 404. A
  "Files" page (or `/api/uploads`) lists, uploads, downloads and deletes
  them.
- Accepted: PNG, JPEG, GIF and WebP images and PDFs, up to 10 MiB, judged
  by `http.DetectContentType` on the first bytes. Never SVG or HTML, which
  a browser would run.
- Uploads are streamed straight to storage (no temporary files) and get 5
  minutes, past the usual request timeouts, for slow connections.
- Downloads carry `Content-Security-Policy: sandbox` as well, so even a
  file that slipped through can't run in the site's origin.
- A deleted upload's file, and a deleted account's files, are removed by a
  job after the records are gone, so a rolled-back transaction never loses
  a file.
- Files live on the `data` volume (`/data/uploads`). The database backups
  don't include them: back up that volume as well.
<!-- kickstart:end -->

<!-- kickstart:begin module=admin -->
### Admin pages

User search and detail, role changes, disabling and deleting users,
resending verification, and a failed-jobs list with retry, behind
`auth.RequireRole(auth.RoleAdmin)`; JSON endpoints in API mode.
- Changes go through the account service, so they are checked, logged and
  hooked exactly like the CLI's.
- Admins can't change their own account there, so no one locks themselves
  out by accident; another admin, or the CLI, can.
- Failed jobs never show their payload: an email job's holds its links,
  tokens included. Retrying gives a job a fresh set of attempts.

**Considered:** CLI subcommands only.
<!-- kickstart:end -->

<!-- kickstart:begin module=audit -->
### Audit log

Security events always go to the logs; this module also keeps them, for
good, in an `audit_events` table, with its own admin-only page (filtered by
account, 50 a page).
- Each event is written in the transaction of the change it describes, so
  it exists exactly when the change does; if it can't be written, the change
  rolls back.
- When a user is deleted, their rows stay and the database clears the
  reference to them (`ON DELETE SET NULL`).

**Considered:** making the table part of the core, and writing events after
the change commits, which could lose some.
<!-- kickstart:end -->

<!-- kickstart:begin module=metrics -->
### Metrics

Prometheus's `client_golang` and Go's `pprof` on an internal port that Caddy
never exposes (`METRICS_ADDR`): requests per route, database pool use, job
queue length and failures, and Go runtime stats. Tracing is skipped until a
service is split out.
- Requests are labelled by the route pattern the router matched (`GET
  /reset-password`), never the raw path, so the number of series stays
  small.
- The job counts are queried when Prometheus scrapes, so every instance
  reports the same, shared queue.
- `serve` serves them; a separate `worker` process doesn't.

**Considered:** OpenTelemetry for metrics and tracing, which is
vendor-neutral but much heavier.
<!-- kickstart:end -->

<!-- kickstart:begin module=errreport -->
### Error reporting

`sentry-go`, enabled by `SENTRY_DSN`, works with Sentry, GlitchTip or
Bugsink. Panics and 500 errors are reported with the request ID; cookies and
passwords are stripped.
- It is a slog handler that reports every error-level record. Errors are
  logged once, where they're handled, so panics, 500 responses and failed
  jobs are each reported once, with no reporting calls in the handlers.
- Only the log record is sent (message, error, attributes, request ID),
  never the request, so cookies and form fields can't leak.

**Considered:** logs plus an uptime monitor only, and reporting from the
recovery middleware and the 500 path, which would miss failed jobs.
<!-- kickstart:end -->

<!-- example:begin -->
### Example feature: notes

User-owned notes with create, read, update and delete show the whole
pattern from route to test. `make remove-example` removes them.
- Every project starts with it, whatever the options: it is the reference
  for a new feature. Someone else's note answers 404, never 403.
- In HTML mode, deleting from the list shows the HTMX pattern: the form
  works as a plain post, and with HTMX the server answers with an empty
  fragment that replaces the row (`web.IsHTMX`).
- Its lines carry `example:begin`/`example:end` markers, which init leaves
  alone. `make remove-example` runs a small Go program (`tools/removeexample`)
  that deletes the folder, the marked blocks, files they leave empty
  (the migration), and then itself, and formats the Go it changed. Like
  init, it's Go rather than `sed`, which differs between Linux and macOS.
- Removal is for before the first deploy: afterwards, the notes table goes
  with a new migration, as any schema change does.
<!-- example:end -->
