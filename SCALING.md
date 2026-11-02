# Scaling

A playbook for when the app grows, cheapest step first. Most apps never
need more than the first three. Each step lists the signs that it's due,
the configuration to change, and the code (if any) to touch.

The app is built for this path: it keeps no user state in memory, and the
pieces that change when it grows sit behind small interfaces (sessions,
rate limiting, jobs, email,
<!-- kickstart:begin module=uploads -->
file storage,
<!-- kickstart:end -->
and the database's read and write handles). Migrations and scheduled tasks
already run once however many instances start.

## Measure first

**Signs:** anything feels slow, or you're about to spend money on a step
below.

Find out where the time goes before changing anything. Every request is
logged with its route, status and duration, and the request ID ties a
request's log lines together.
<!-- kickstart:begin module=metrics -->
Turn on metrics (`METRICS_ADDR`, README) for request rates and durations
per route, database pool use and the job queue, and use the profiler at
`/debug/pprof/` for CPU and memory.
<!-- kickstart:end -->
<!-- kickstart:begin db=postgres -->
In Postgres, the `pg_stat_statements` extension shows which queries take the
most time.
<!-- kickstart:end -->

## Add indexes and tune the pool

**Signs:** a few queries dominate the time; requests wait for a database
connection.

- Look at the slow queries' plans
<!-- kickstart:begin db=sqlite -->
  (`EXPLAIN QUERY PLAN`)
<!-- kickstart:end -->
<!-- kickstart:begin db=postgres -->
  (`EXPLAIN ANALYZE`)
<!-- kickstart:end -->
  and add the indexes they lack, in a migration. Every foreign key already
  has one.
<!-- kickstart:begin db=postgres -->
- `DB_MAX_OPEN_CONNS` (default 25 per instance) caps each instance's
  connections. Raise it if requests wait for one; keep the total across
  instances under the server's `max_connections` (100 by default).
  `DB_CONN_MAX_IDLE_TIME` and `DB_CONN_MAX_LIFETIME` rarely need changing.
<!-- kickstart:end -->
<!-- kickstart:begin db=sqlite -->
- SQLite writes one transaction at a time; the write pool has one
  connection on purpose. Keep write transactions short: do slow work
  (hashing, calling other services) before opening one.
<!-- kickstart:end -->

**Code:** a migration per index; nothing else.

## A bigger server

**Signs:** CPU or memory is regularly above about 70% after the steps above.

Resize the VPS. It's the cheapest step that buys a lot: one modern server
runs an app like this for tens of thousands of users. Nothing in the app
changes.

<!-- kickstart:begin db=sqlite -->
## Move from SQLite to PostgreSQL

**Signs:** you need a second app server (for capacity or to deploy without
a moment's downtime), writes queue up behind each other, or you need
Postgres features.

The queries are portable and sqlc generates the same Go functions for both
databases, so this is a configuration and data-copy job rather than a
rewrite.

1. Switch the code with CLAUDE.md's "Changing what init chose": init on
   two scratch copies of the template commit, the recorded command and the
   same with `DB=postgres`, and their diff is the whole change (the
   driver, the database code and its tests, migrations, generated queries,
   Compose services, CI, backups and docs, with the SQLite-only `backup`
   and `restore` commands gone).
2. Rewrite your own features' migrations for Postgres: `TEXT` ids become
   `uuid`, `DATETIME` becomes `timestamptz`, `INTEGER` becomes `bigint`;
   name constraints as the existing migrations do. Then `make generate` and
   `make test`: the queries themselves rarely need changes.
3. Copy the data. Start the new database and let the app create the schema
   (`make migrate`), then copy each table in migration order (users first),
   leaving out `goose_db_version`, for example with pgloader in data-only
   mode. Compare row counts table by table and log in as a test user.
4. Point `DATABASE_URL` at Postgres, deploy, and keep the SQLite file until
   you're sure.

The steps after this one assume PostgreSQL.

<!-- kickstart:end -->
## Run the worker as its own process

**Signs:** background jobs (emails, cleanups, your own) slow down requests,
or need more or fewer resources than the web side.

**Config:** set `WORKER_ENABLED=false` for the app, and add a worker service
to `compose.yaml` running the same image with the `worker` command:

```yaml
  worker:
    image: ${APP_IMAGE:-ghcr.io/ukharwa/kickstart:latest}
    restart: unless-stopped
    command: worker
    env_file: .env
    # the same environment entries as the app service
```

**Code:** none. `serve` and `worker` are the same binary; any number of
workers can run, since each job is claimed by one.

## Several app instances

**Signs:** one instance's CPU is the limit, or you want to deploy without
a moment's downtime.
<!-- kickstart:begin db=sqlite -->
It needs PostgreSQL first (earlier in this playbook): SQLite can't be
shared.
<!-- kickstart:end -->

**Config:** `docker compose up -d --scale app=3`. Caddy already finds every
instance, balances requests by fewest connections and stops sending to an
instance whose `/readyz` fails. Sessions live in the database, so users
stay logged in whichever instance answers. Migrations take a database lock
and scheduled tasks are enqueued once, so starting several instances is
safe. This was checked with two instances (DECISIONS.md, "Checked with two
instances").

What changes with more than one:

- **Rate limits are per instance** (the in-memory limiter): N instances
  allow up to N times the attempts. Fine for a few instances; see the next
  step beyond that.
<!-- kickstart:begin module=uploads -->
- **Uploads** live on the `data` volume, which every instance on the same
  server shares. On more than one server, move them to object storage
  (below).
<!-- kickstart:end -->
- On **several servers**, put a load balancer (or Caddy on its own server)
  in front, pointing at each server's app instances.

**Code:** none.

## Share rate limits

**Signs:** more than a few instances, or attackers spreading attempts
across them.

**Code:** implement `ratelimit.Limiter` (`Allow` and `Reset`) on a shared
store, such as Valkey or Redis (`INCR` with an expiry per key and window),
in `internal/ratelimit/`, and build it instead of `ratelimit.NewMemory()`
in `newServices` (`cmd/kickstart/app.go`). Add a setting choosing between
the two (say `RATE_LIMIT_STORE=memory|valkey` and its address) in
`internal/config` and `.env.example`, since there are then two
implementations. If the store is unreachable, limits fail closed (`503`),
as they do now.

<!-- kickstart:begin module=uploads -->
## Move uploads to object storage

**Signs:** app instances on more than one server, or the uploads volume
outgrows the disk.

**Code:** implement `uploads.Storage` (`Put`, `Open`, `Delete`) for
S3-compatible storage with `minio-go`, next to `Disk` in
`internal/uploads/storage.go`, and choose it in `newServices` by a new
`STORAGE_DRIVER=local|s3` setting with the bucket's endpoint, name and keys.
For large files, serve short-lived signed download links instead of
streaming through the app.

**Data:** copy the existing files to the bucket under the same keys (for
example `rclone copy /data/uploads remote:bucket`) before switching.

<!-- kickstart:end -->
## Run migrations as a deploy step

**Signs:** a migration that takes minutes (a big index), or a database user
for the app that shouldn't be able to change the schema.

**Config:** set `MIGRATE_ON_START=false`, and run them before starting the
new version:

```bash
docker compose run --rm app migrate up
```

With a separate database user for migrations, give it in that command's
`DATABASE_URL` only.

**Code:** none.

## Managed Postgres and a connection pooler

**Signs:** you'd rather not run backups, upgrades and failover yourself;
connections across instances near the server's limit.

**Config:** point `DATABASE_URL` at the managed database (with
`sslmode=require` or stricter). Put PgBouncer (or the host's pooler) in
transaction mode in front when instances × `DB_MAX_OPEN_CONNS` nears
`max_connections`; use PgBouncer 1.21 or newer with
`max_prepared_statements` set, or add `default_query_exec_mode=exec` to
`DATABASE_URL`, because pgx prepares statements.

**Code:** none.

## Read replicas

**Signs:** the database's CPU goes on reads, and the primary is busy.

**Config:** set `DATABASE_READ_URL` to the replica. Queries made with
`store.New(d.Read)` go there; writes and transactions stay on the primary.

**Code:** a replica lags slightly, so a page that shows what the same
request (or the one just before) wrote should read with `d.Write`. Check
reads that follow a redirect after a form post.

## A cache

**Signs:** the same expensive query or computation runs on most requests,
and an index can't fix it.

**Code:** there's no cache yet, on purpose. Add one where it's needed: an
in-memory one inside the feature for data that may be a little stale per
instance, or a small cache interface with a Valkey or Redis implementation
when instances must agree. Invalidate on write, in the same function that
writes.

## Sessions in a shared store

**Signs:** session lookups (one per request) are a noticeable part of the
database's load. This comes late, if ever.

**Code:** implement `auth.SessionStore` on Valkey or Redis and pass it to
`auth.NewService` in `newServices` instead of `auth.DBSessions`. Expire
keys with the session's lifetimes.

## A dedicated job queue

**Signs:** jobs keep the database busy, or need priorities, fan-out or
throughput the database queue doesn't give.

**Code:** the `jobs` package's API (`Enqueue`, `Worker`, `Scheduler`) is
the seam. River (Postgres) keeps jobs enqueued in the same transaction as
the change that caused them; a broker such as NATS doesn't, so pair it with
an outbox table to keep that guarantee. Handlers stay as they are.

## An email provider's API

**Signs:** you want bounce and complaint handling or delivery statistics
your SMTP relay doesn't give.

**Code:** implement `email.Sender` (`Send`) on the provider's HTTP API
(Postmark, SES, Mailgun, …) and choose it in `newSender` by a setting.
Sending still happens in a job.

## Tracing

**Signs:** a request is slow and the logs can't say where, typically once
part of the app talks to another service.

**Code:** add OpenTelemetry: `otelhttp` around the handler in `routes`,
spans around database calls, and an exporter set by the standard
`OTEL_EXPORTER_OTLP_ENDPOINT`. The request ID can go on each span.

## Split out a busy feature

**Signs:** one feature needs far more resources, a different release
cadence, or its own team.

**Code:** features are self-contained packages under `internal/` that
reach each other only through `cmd/kickstart/` and the core's seams, so a
feature can move to its own binary (another command in this repository) or
its own service with its own tables. Do it for one feature at a time, and
only when this step's signs are real.
