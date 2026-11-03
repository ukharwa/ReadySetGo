# AGENTS.md

Conventions for working in this repository. Keep this file accurate when a
convention changes. `DECISIONS.md` explains why things are the way they
are, `README.md` how to run and deploy the app, and `SCALING.md` what to do
as it grows.

<!-- readysetgo:begin template -->
## Read first: this is the uninitialised template

This repository is the ReadySetGo template, not yet an app. It stays that
way until `make init` runs, which deletes `tools/readysetgo/`: while that
folder exists, you are in the template. A user who opens a session here and
describes an app ("set this up with Postgres and an API") is starting a new
project, so follow "Starting a new project" below before anything else.
Don't build features, hand-edit files to switch the database or UI, or
delete folders yourself: init does all of that, consistently. If the user
wants to change the template itself, skip to "Working on the template
itself".

## Starting a new project

Steps 1 to 4 normally end in one message to the user: what the checks
found, anything to correct, the questions still open, and the `make init`
command you propose. Nothing runs until they answer.

### 1. Check the checkout

Run these; don't assume.

- `git remote -v`. If `origin` is the template (`ukharwa/ReadySetGo`, in
  any letter case, or its old name `ukharwa/kickstart`), the
  new project would push into the template, so nothing may run until
  that's fixed (asking the other questions meanwhile is fine). Two ways:
  - **Recommended:** GitHub's "Use this template" button makes a separate
    repository whose history starts with one commit. The user clones it
    and opens their coding agent there, and these checks run again in the new
    clone.
  - An empty repository that this checkout's `origin` is pointed at. Once
    the user gives its URL, run `git remote set-url origin URL` yourself
    and check `git ls-remote origin` lists nothing (it must be empty).
    Quicker, but the project keeps the template's history.

  No `origin` at all is fine.
- `git status`. Init refuses uncommitted changes; ask before committing or
  stashing anything.
- `go version`: 1.27 or newer is required.
- `docker info` and `docker compose version`: Docker with the Compose
  plugin runs the development services, Postgres's tests and the
  production stack.
- `cc --version`: `make tools` builds sqlc, which needs a C compiler.
- Whether the ports the new project uses in development are free:
  `ss -ltn` (or `lsof -i`) for 8080 (the app), 5432 (Postgres, if
  chosen), 1025 and 8025 (Mailpit), and `docker ps` to see what holds
  them. Another project's dev services often do, the template's own
  included. Recommend the next free ports, moved in the new project's
  `.env` (step 5); stop someone else's containers only if the user says
  so.

If something required is missing, say what to install before going on.

### 2. Work out the options

Map what the user asked for onto init's options. Then ask about whatever is
still open, recommending an answer for each.

| Option | Values | Default | Notes |
|---|---|---|---|
| `NAME` | lowercase letters, digits and hyphens, starting with a letter | none | Names the command (`cmd/NAME`), containers, database and image. Use the user's name for it; proposing the repository's name for them to confirm is fine, picking one silently isn't. Init refuses `readysetgo`, the template's own name. |
| `MODULE` | a Go module path | none: required | Take it from `origin` when there is one: `git@github.com:OWNER/REPO.git` means `github.com/OWNER/REPO`. Otherwise ask; propose `github.com/OWNER/NAME` for them to confirm. Off GitHub, the image is plain `NAME` and `APP_IMAGE` must be set for deploys. |
| `DB` | `sqlite`, `postgres` | `sqlite` | Below. |
| `UI` | `html`, `api` | `html` | Below. |
| `MODULES` | a comma-separated list, `all` or `none` | `none` | Below. |

How requests map:

- **`UI=api`:** "API", "REST", "JSON", "backend for a mobile app", "React,
  Vue, Svelte or another SPA frontend", "headless".
- **`UI=html`:** "web app", "website", "server-rendered", "pages", "HTMX",
  "forms", with no separate frontend.
- **`DB=postgres`:** Postgres by name; several app servers; many users
  writing at once; a managed database; Postgres features (full-text
  search, JSONB).
- **`DB=sqlite`:** SQLite by name; "simple", "one server", "small".
- **`workspaces`:** teams, organisations, companies or accounts shared by
  several users, multi-tenant, inviting colleagues, roles within a team.
- **`admin`:** an admin panel or back office, managing users, retrying
  failed jobs.
- **`audit`:** an audit log or trail, activity history, compliance.
- **`uploads`:** file uploads, attachments, images, documents, avatars.
  Each file belongs to the user who uploaded it, and only they can fetch
  it. Files attached to a feature's records, or shared by a team, are
  built on top afterwards, reusing its storage and checks.
- **`metrics`:** Prometheus, Grafana, monitoring, performance metrics.
- **`errreport`:** Sentry, GlitchTip, Bugsink, error tracking or alerts.
- **Invite-only sign-up**, a closed beta or an internal tool: not an init
  option but a setting, `REGISTRATION=invite`, changed any time. Mention
  it rather than leaving it out.

A database the user named is the one to use; mention a mismatch only when
there's a real one (SQLite for an app that must run on several servers).
When they didn't name one, recommend SQLite unless the app needs more than
one server or heavy concurrent writes (README.md, "Which database should I
pick?"). Recommend only the modules the description calls for, and list
the rest in a line so the user can add any: init runs once, so modules
can't be added by running it again.

### 3. Correct what the template doesn't do

Say so in the same message, with what the template does instead
(DECISIONS.md has the reasons):

- **MariaDB, MySQL, MongoDB or another database:** only SQLite and
  PostgreSQL. MariaDB was dropped on purpose. PostgreSQL is the nearest
  match; mention SQLite too if the app is small and nothing requires a
  database server.
- **Both pages and a JSON API in one project:** one UI mode per project. A
  browser frontend goes with `UI=api`.
- **A React (or other SPA) frontend:** init doesn't create it. It lives in
  its own folder (say `frontend/`) or repository and talks to `/api/` on
  the same domain, so there's no CORS setup. In development, run its dev
  server with a proxy sending `/api` to `http://localhost:8080`, and set
  `APP_URL` to the dev server's address, because emailed links open
  there. In production, Caddy serves its built files (the Caddyfile has
  the block), so the image or `make deploy` must also ship the build.
- **An OpenAPI or Swagger page:** not generated; "API" here means JSON
  endpoints under `/api/`, each listed in its feature's `Routes` method.
- **JWT:** not used. A browser frontend signs in with an HttpOnly session
  cookie; mobile apps and scripts get revocable bearer tokens
  (`POST /api/auth/token`).
- **Social login (Google and others), two-factor login or passkeys:** not
  included; they'd be built afterwards. Social login needs the OAuth flow,
  linking accounts by verified email, and a migration allowing users
  without a password (`password_hash` is required now).
- **Uploads to S3:** the uploads module stores files on local disk; adding
  S3 is a step in SCALING.md.
- **Redis, Kubernetes, message queues or microservices:** left out by
  design; SCALING.md shows where each fits as the app grows.
- **Another framework, an ORM, GraphQL, gRPC, templ or Tailwind:** not part
  of the template, whose conventions assume its own stack. Point out the
  cost; it's the user's call.
- **Changing an option later:** init runs once and deletes itself, so the
  database, UI mode and modules are fixed by this step. Moving from SQLite
  to PostgreSQL later is a documented job (SCALING.md); adding a module
  means copying it across from a fresh copy of the template.

If a request rests on a misunderstanding, say so plainly. If the user
still wants something unsupported, explain what it would take and let them
decide.

### 4. Propose the command

Give the exact command with one line per choice and its reason, for
example `make init NAME=shop MODULE=github.com/acme/shop DB=postgres UI=api MODULES=workspaces,admin`,
plus any port changes, and wait for the user's yes.

### 5. Run it and check the result

```bash
make init NAME=… MODULE=… DB=… UI=… MODULES=…
cp .env.example .env
```

If step 1 found ports taken,
move them in `.env` now, as "Local ports" under "Commands" lists.

Then:

```bash
make services
make test
make tools
make check
```

`make services` starts Mailpit, and Postgres for a Postgres project, whose
tests need it. `make tools` installs the pinned linters and sqlc into
`bin/`, and `make check` runs everything CI runs (it downloads the
vulnerability checker). If a step fails, stop and report it: nothing is
committed yet, so `git status` shows what init changed and git can undo
it.

If the user wants to see the app, also run `make seed` and `make run`
(leave it running in the background), check `http://localhost:8080/readyz`
(or the moved port) answers, and give them the addresses (with any moved
ports), the seed logins (`admin@example.com` and `user@example.com`,
password `dev-password-123`) and Mailpit's inbox.

### 6. Hand over

Show `git status` and offer to commit. Mention that `make remove-example`
deletes the notes example if the user doesn't want it (before the first
deploy), and that README.md covers running, deploying and backups. From
then on this file describes the project itself: follow it.

## Working on the template itself

Read `tools/readysetgo/README.md` first. It explains the markers init uses,
keeping the two databases' code in step, and testing every combination of
options.
<!-- readysetgo:end -->

## This project

It was created from a template by `INIT_COMMAND`, run on commit
`INIT_COMMIT`: the template as it was just before init, which this
repository's history keeps. That chose:

<!-- readysetgo:begin db=sqlite -->
- **Database:** SQLite, on one server. SCALING.md covers moving to a
  database server when the app needs more than one.
<!-- readysetgo:end -->
<!-- readysetgo:begin db=postgres -->
- **Database:** PostgreSQL.
<!-- readysetgo:end -->
<!-- readysetgo:begin ui=html -->
- **UI:** server-rendered pages with HTMX.
<!-- readysetgo:end -->
<!-- readysetgo:begin ui=api -->
- **UI:** a JSON API under `/api/`, for a separate frontend or a mobile
  app.
<!-- readysetgo:end -->
<!-- readysetgo:begin module=workspaces -->
- **Module:** workspaces (teams sharing data).
<!-- readysetgo:end -->
<!-- readysetgo:begin module=admin -->
- **Module:** admin (managing users and failed jobs).
<!-- readysetgo:end -->
<!-- readysetgo:begin module=audit -->
- **Module:** audit (security events kept in a table).
<!-- readysetgo:end -->
<!-- readysetgo:begin module=uploads -->
- **Module:** uploads (users' files).
<!-- readysetgo:end -->
<!-- readysetgo:begin module=metrics -->
- **Module:** metrics (Prometheus and the profiler).
<!-- readysetgo:end -->
<!-- readysetgo:begin module=errreport -->
- **Module:** errreport (error reports to Sentry and compatible services).
<!-- readysetgo:end -->

The init step deleted itself, so these can't be changed by a setting.
Asked to change one, say what it involves first, then follow "Changing
what init chose" below rather than editing your way there piecemeal.

## Changing what init chose

To switch the database or UI mode, or to add one of the template's other
optional modules, let init work out the change:

1. Make two scratch copies of the template commit above, outside this
   repository: `git archive INIT_COMMIT | tar -x -C DIR` for each.
2. In one, run the command above unchanged; in the other, the same command
   with the new options.
3. `diff -ruN old new` is exactly what the change needs. Compare the first
   copy with this repository too: anything that differs there is the
   project's own work, which the change must keep. (The copies aren't git
   checkouts, so they record the commit as `unknown`; ignore that line.)
4. If nothing has been built yet (this repository still matches the first
   copy), the second copy can simply replace these files. Otherwise apply
   the diff here (`patch -p1 --dry-run` first) and settle each conflict by
   hand.
5. A module's migration needs a number after this project's latest, since
   goose won't apply an older number to a database that's past it.
6. Run `make generate`, `make test` and `make check`, and update the
   record of this command above and the matching decision in
   DECISIONS.md, with a dated note rather than rewriting it.

Moving an existing database's data into a new one is a separate step:
SCALING.md covers it.
<!-- readysetgo:begin module=workspaces,module=admin,module=audit,module=uploads,module=metrics,module=errreport -->
README.md ("Removing a module") explains taking a module out.
<!-- readysetgo:end -->

## Commands

- `make services`: start the dev services. Mailpit's inbox at
  <http://localhost:8025> catches every email the app sends.
  <!-- readysetgo:begin db=postgres -->
  Postgres is needed by `run` and `test`.
  <!-- readysetgo:end -->
- **Local ports:** the app uses 8080 and Mailpit 1025 and 8025
  <!-- readysetgo:begin db=postgres -->
  (Postgres 5432)
  <!-- readysetgo:end -->
  in development. When another project already holds one (`ss -ltn`,
  `docker ps`), move it in `.env`, taking the next free port, rather than
  stopping that project. These settings move together:
  - the app: `HTTP_ADDR` and `APP_URL`
  - Mailpit: `MAILPIT_SMTP_PORT` with `SMTP_PORT`, and `MAILPIT_PORT` (its
    inbox)
  <!-- readysetgo:begin db=postgres -->
  - Postgres: `POSTGRES_PORT`, with the port in `DATABASE_URL` and
    `TEST_DATABASE_URL`
  <!-- readysetgo:end -->
- `make run`: run the HTTP server and job worker (reads `.env`; copy
  `.env.example` first)
- `make worker`: run only the job worker
- `make dev`: like `make run`, rebuilding and restarting on code changes
- `make test`: all tests, with the race detector
- `make check`: everything CI checks (lint, tidy, generated code,
  vulnerabilities, tests); run it before pushing
- `make tools`: install the pinned sqlc, air and golangci-lint into `bin/`
- `make migrate`: apply pending migrations; `make migration NAME=add_x`
  creates a new one
- `make generate`: regenerate each feature's `store` package after
  changing SQL (`make tools` installs sqlc first)
<!-- readysetgo:begin db=sqlite -->
- `make backup`: write a database snapshot into `backups/`
<!-- readysetgo:end -->
- `make seed`: add `admin@example.com` and `user@example.com`, both with
  the password `dev-password-123` (dev only)
- `go run ./cmd/readysetgo user promote EMAIL`: make a user an admin (also
  `demote`, `disable`, `enable`, `delete`); `user invite EMAIL` emails
  someone a link to sign up
<!-- example:begin -->
- `make remove-example`: delete the notes example and every line marked
  for it, before the first deploy
<!-- example:end -->
- `make up` / `make down`: run the production stack (`compose.yaml`: the
  app behind Caddy) locally at https://localhost (`HTTPS_PORT` moves it)
- `make deploy DEPLOY_HOST=user@server`: copy `compose.yaml` and `deploy/`
  to the server, pull the new image and restart
<!-- readysetgo:begin db=sqlite -->
- `docker compose exec app /app backup -dir /data/backups` takes a snapshot
  on the server; to restore one, stop the app, then
  `docker compose run --rm app restore -from /data/backups/FILE.db`
<!-- readysetgo:end -->
<!-- readysetgo:begin db=postgres -->
- `./deploy/backup-postgres.sh` dumps the database into `backups/` on the
  server; its header shows how to restore a dump
<!-- readysetgo:end -->
- `make build`, `make fmt`, `make vet`; `make help` lists everything

## Layout

- `cmd/readysetgo/`: the only `main` package and the only place dependencies
  are wired together.
  - `main.go` lists the subcommands; each has its own file, such as
    `serve.go` and `migrate.go`.
  - `app.go` holds the setup they share: config, logger, database,
    `newServices` (the services every command uses) and `startJobs`.
  - `routes.go` mounts every feature's routes, one line per feature.
  <!-- readysetgo:begin ui=html -->
  - `web.go` sets up the page renderer and mounts the HTML pages.
  <!-- readysetgo:end -->
- `internal/config/`: reads every environment variable. Nothing else calls
  `os.Getenv` (test helpers excepted).
- `internal/logging/`: builds the slog logger.
- `internal/httpx/`: HTTP server setup, health endpoints, the middleware
  stack, and helpers shared by every feature's handlers.
- `internal/ratelimit/`: the rate-limiter seam (`Limiter`), its in-memory
  default, and `PerIP` middleware.
- `internal/jobs/`: the database job queue (`Enqueue`), the `Worker` that
  runs jobs and the `Scheduler` that enqueues recurring ones.
- `internal/email/`: the email-sender seam (`Sender`, with SMTP and log
  implementations) and `Enqueue`; `emailtest.Recorder` for tests.
- `internal/db/`: opens the database (separate `Read` and `Write` handles),
  runs migrations, and provides `InTx`, `NewID`, `Now` and `Err`. The
  database-specific code is in its dialect file.
- `internal/db/dbtest/`: gives each test its own migrated database.
- `internal/<feature>/<feature>.sql`: each feature's queries, and
  `internal/<feature>/store/`: the Go sqlc generates from them. Never edit
  the generated code.
- `internal/auth/`: accounts. `auth.go` is the `Service` with every flow and
  no HTTP; `sessions.go` the session-store seam; `devices.go` lists a
  user's sessions as signed-in devices; `profile.go` the name and time
  zone; `invitations.go` sign-up invitations and invite-only sign-up
  (`REGISTRATION=invite`); `middleware.go` loads the user and guards
  routes; `emails/` holds the email templates.
  <!-- readysetgo:begin ui=html -->
  `web.go` and `templates/` are the account pages.
  <!-- readysetgo:end -->
  <!-- readysetgo:begin ui=api -->
  `api.go` is the JSON API under `/api/auth/`.
  <!-- readysetgo:end -->
  Other features react to accounts through `OnUserCreated` and
  `OnUserDeleting` hooks, and to security events (failed logins, role
  changes, …) through `OnEvent`; all run inside the same transaction. A
  feature with invitations of its own lets their addresses sign up when it
  needs an invitation through `AllowSignup`.
<!-- readysetgo:begin module=workspaces -->
- `internal/workspaces/`: workspaces, their members (owner, admin, member)
  and email invitations. `middleware.go` resolves `{workspaceID}` and
  answers 404 to non-members; `workspacestest/` signs people in with their
  personal workspace for tests.
<!-- readysetgo:end -->
<!-- readysetgo:begin module=admin -->
<!-- readysetgo:begin ui=html -->
- `internal/admin/`: the admin pages under `/admin/`: users (role,
  disabling, deleting, verification links) and failed jobs to retry.
<!-- readysetgo:end -->
<!-- readysetgo:begin ui=api -->
- `internal/admin/`: the admin endpoints under `/api/admin/`: users (role,
  disabling, deleting, verification links) and failed jobs to retry.
<!-- readysetgo:end -->
  Account changes go through `auth.Service`, like the CLI's; admins can't
  change their own account there.
<!-- readysetgo:end -->
<!-- readysetgo:begin module=audit -->
- `internal/audit/`: keeps every security event in the `audit_events`
  table (an `OnEvent` hook) and lists them for admins
  <!-- readysetgo:begin ui=html -->
  at `/admin/audit`.
  <!-- readysetgo:end -->
  <!-- readysetgo:begin ui=api -->
  at `/api/admin/audit-events`.
  <!-- readysetgo:end -->
<!-- readysetgo:end -->
<!-- readysetgo:begin module=uploads -->
- `internal/uploads/`: users' files. `storage.go` is the `Storage` seam and
  its local-disk implementation (`UPLOADS_DIR`); `uploads.go` checks each
  file's type from its contents and size, and deletes stored files through
  a job once their records are gone; `http.go` streams uploads and serves
  downloads to their owner only.
<!-- readysetgo:end -->
<!-- readysetgo:begin module=metrics -->
- `internal/metrics/`: Prometheus metrics (requests per route, database
  pools, job queue, Go runtime) and Go's profiler, served by `serve` on
  `METRICS_ADDR`, never publicly.
<!-- readysetgo:end -->
<!-- readysetgo:begin module=errreport -->
- `internal/errreport/`: a slog handler that reports every error-level log
  record to Sentry (or GlitchTip or Bugsink) when `SENTRY_DSN` is set.
<!-- readysetgo:end -->
<!-- readysetgo:begin ui=html -->
- `internal/web/`: the page layout, renderer, stylesheet, HTMX, flash
  messages, and `app.js`, which fills time zones in from the browser.
<!-- readysetgo:end -->
<!-- example:begin -->
- `internal/notes/`: the example feature, notes that belong to the user who
  wrote them. Copy its shape for a new feature.
<!-- example:end -->
- `internal/validate/`: input checks and `validate.Errors` (messages per
  field).
- `internal/apptest/`: the end-to-end test harness.
- `migrations/`: goose SQL migrations, embedded into the binary.
- `Dockerfile`, `compose.yaml` (production stack), `compose.dev.yaml` (dev
  services), `deploy/` (Caddyfile, backups).
<!-- example:begin -->
- `tools/removeexample/`: what `make remove-example` runs.
<!-- example:end -->

## Database conventions

- **Adding a table or column:** `make migration NAME=add_things`, write the
  `-- +goose Up` and `-- +goose Down` sections, then `make generate`.
  Migrations are expand/contract: add the new column or table, switch the
  code over in a later deploy, and only then remove the old one. Never edit
  a migration that has already run anywhere; add a new one.
- **Adding a query:** add it to the feature's `.sql` file with a sqlc
  `-- name: DoThing :one|:many|:exec` comment and `@name` parameters, list
  the file under `queries` in `sqlc.yaml` if it's new, and run
  `make generate`.
- **Calling queries:** `store.New(d.Read)` for queries that only read,
  `store.New(d.Write)` for writes, and `store.New(tx)` inside `d.InTx`.
  Pass every error through `db.Err` and check for `db.ErrNotFound` and
  `db.ErrConflict` with `errors.Is`; never check driver error types.
- **IDs:** UUIDv7 from `db.NewID()`, generated in Go before the insert.
  Name UUID columns `id` or `<thing>_id`.
  <!-- readysetgo:begin db=sqlite -->
  A nullable UUID column needs its own entry in `sqlc.yaml`'s overrides
  (see the comment there).
  <!-- readysetgo:end -->
- **Timestamps:** set from Go with `db.Now()` (UTC, microseconds), never by
  database defaults. Stored and returned in JSON as UTC; only display
  converts them to the user's time zone.
- **Emails and usernames:** trim and lowercase in Go before saving or
  looking up, with a unique index on the cleaned value.
- **Other types:** strings are `TEXT`; money is whole cents in an integer;
  every foreign key has an explicit `ON DELETE` rule and an index.
  <!-- readysetgo:begin db=sqlite -->
  Integers are `INTEGER` (`int64` in Go).
  <!-- readysetgo:end -->
  <!-- readysetgo:begin db=postgres -->
  Integers are `bigint` (`int64` in Go; `integer` would be `int32`).
  <!-- readysetgo:end -->
- **Database-specific SQL** goes only in migrations and in queries that
  can't be shared; everything else stays portable.

## Adding a feature

1. `make migration NAME=create_things`: its tables, each with the owner's
   `user_id` (`ON DELETE CASCADE`, indexed).
2. Its queries in `internal/things/things.sql`, every one filtered by the
   owner; list the file in `sqlc.yaml` and run `make generate`.
3. `things.go`: a `Service` with the logic and validation, and no HTTP.
<!-- readysetgo:begin ui=html -->
4. `web.go` and `templates/`: a `Web` type whose `Routes` lists the pages,
   mounted in `cmd/readysetgo/web.go`.
<!-- readysetgo:end -->
<!-- readysetgo:begin ui=api -->
4. `api.go`: an `API` type whose `Routes` lists the endpoints, mounted in
   `cmd/readysetgo/routes.go`.
<!-- readysetgo:end -->
5. Build the service in `newServices` (`cmd/readysetgo/app.go`).
6. End-to-end tests with `apptest`, including that someone else gets 404
   for every route.
<!-- example:begin -->

`internal/notes/` does each step; copy it and rename. If the notes example
grows into a real feature instead, delete its `example:begin` and
`example:end` markers (and `make remove-example` with them), so its code
is never removed by mistake.
<!-- example:end -->

A feature may use the project's modules; the modules themselves still
don't depend on each other.
<!-- readysetgo:begin module=uploads -->
Files attached to a feature's records go through `uploads.Service`, with
`uploads.Limit`, `uploads.RequestContext` and `uploads.FilePart` in the
handlers and a link table from the feature's rows to `uploads(id)`.
<!-- readysetgo:end -->

## Conventions

- **Go style:** follow Effective Go, Go Code Review Comments and Google's Go
  style guide. Code is `gofmt`ed. Package names are short and lowercase;
  no `util`, `common`, `helpers` or `models` packages.
- **Configuration:** add a field to `config.Config`, read it in
  `config.Load` with the loader helpers, add it to `LogValue` unless it's a
  secret, and document it in `.env.example` with its default.
- **Errors:** wrap with context (`fmt.Errorf("load user: %w", err)`), check
  with `errors.Is` and `errors.As`, and log once, where the error is
  handled. Users never see internal error details.
- **Context:** `context.Context` is the first parameter. Every database and
  outbound call uses a context with a timeout.
- **Logging:** use slog's `…Context` methods. Never log passwords, tokens,
  cookies, emails, query strings or database URLs. Changes to accounts go
  through `auth.Service`, which logs each as a security event with the
  user, the acting user, the IP address and the request ID.
  <!-- readysetgo:begin module=errreport -->
  Every error-level record is also reported as an error to fix, so log at
  error level only what someone should look at, and only once.
  <!-- readysetgo:end -->
- **State:** no package-level mutable state; dependencies are built in
  `main` and passed explicitly. Nothing may assume a single instance.
- **Background work:**
  - Enqueue jobs with `jobs.Enqueue(ctx, store.New(tx), kind, payload)`
    inside the same `db.InTx` as the change that caused them.
  - Register a job's handler, and any recurring task
    (`scheduler.Every`), in `startJobs` in `cmd/readysetgo/app.go`.
  - Handlers must be safe to run twice and must respect their context.
  - Never send email during a request: build an `email.Message` and
    `email.Enqueue` it. Tests use `emailtest.Recorder` and run queued jobs
    with `worker.RunOne`.
- **Interfaces:** only for the agreed seams (sessions, rate limiting, jobs,
  email), defined where they're used.
  <!-- readysetgo:begin module=uploads -->
  File storage (`uploads.Storage`) is one too.
  <!-- readysetgo:end -->
- **HTTP:**
  - Each feature has a handler type with a `Routes(mux)` method listing its
    routes; mount it in `cmd/readysetgo/routes.go`. Every request reaches
    them through the middleware stack (`httpx.Stack`), outermost first: real
    client IP, request ID, access log, panic recovery, security headers,
    cross-origin (CSRF) check, 1 MiB body limit, 25-second request timeout,
    then `LoadUser`.
  - Guard routes with `auth.RequireUser`, then `auth.RequireVerified` or
    `auth.RequireRole(auth.RoleAdmin)` as needed; read the user with
    `auth.UserFrom(ctx)`. Wrap routes that check passwords or send email in
    `ratelimit.PerIP`.
  - Services return `validate.Errors` for bad input and named errors for
    other expected failures; handlers turn them into responses.
  <!-- readysetgo:begin ui=html -->
  - Pages: parse a feature's `templates/` with `renderer.Pages` and
    `Render` them. Forms post, then redirect with `web.SetFlash`; on bad
    input re-render with `web.Form` and status 422. Use
    `web.SafeRedirect` for any `?next=`. Show times in the viewer's time
    zone: `{{(.CreatedAt.In $.TimeZone).Format "2 Jan 2006 15:04"}}`.
  <!-- readysetgo:end -->
  - JSON: decode with `httpx.ReadJSON`, respond with `httpx.WriteJSON`,
    `httpx.WriteInvalid` (422 with field errors) or `httpx.ErrorMessage`.
    Errors are RFC 9457 problem details.
  - GET requests never change state: the CSRF check only covers POST, PUT,
    PATCH and DELETE.
  - Respond to errors with `httpx.Error(w, r, status)`; it never shows
    error details and adds the request ID to 5xx responses.
  - Use `httpx.ClientIP(ctx)` for the client's address, never `RemoteAddr`
    or `X-Forwarded-For` directly.
  - Raise one route's body limit by wrapping it in `httpx.MaxBytes(n)`;
    limit requests with `ratelimit.PerIP` or a `ratelimit.Limiter`.
- **Linting:** fix findings rather than silencing them. When one is
  deliberate, add `//nolint:<linter> // <reason>` on that line.
- **Upgrading Go:** change the `go` line in `go.mod` and the `golang` image
  in the `Dockerfile` together; CI reads its version from `go.mod`.
- **Tests:** standard `testing` only, table-driven, `t.Parallel()` where
  possible, no assertion libraries. Tests that touch the database use
  `dbtest.New(t)`, which gives each test its own database. End-to-end tests
  use `apptest.New(t)`: register the feature's routes on its `Mux` and drive
  its HTTPS `Server` with `Client()`, reading emails with `TokenFrom`.
  `SignIn(client, email)` signs a client in as a new verified user. Tests
  that need a folder use `t.TempDir()`, so nothing is left behind.
  <!-- readysetgo:begin db=postgres -->
  It needs `make services` running and `TEST_DATABASE_URL` set.
  <!-- readysetgo:end -->
<!-- readysetgo:begin module=workspaces -->

## Workspace-owned data

New features belong to a user unless they say otherwise. To make a feature's
data shared by a workspace instead:

- Give its table a `workspace_id` column referencing `workspaces(id)
  ON DELETE CASCADE`, with an index, and scope every query by it
  (`WHERE workspace_id = @workspace_id AND id = @id`), so one workspace can
  never reach another's rows.
<!-- readysetgo:begin ui=html -->
- Put its pages under `/w/{workspaceID}/…` and guard each route with
  `auth.RequireUser(workspacesService.Require(handler))`.
<!-- readysetgo:end -->
<!-- readysetgo:begin ui=api -->
- Put its endpoints under `/api/workspaces/{workspaceID}/…` and guard each
  with `auth.RequireUser(workspacesService.Require(handler))`.
<!-- readysetgo:end -->
- In the handler, `workspaces.From(ctx)` is the workspace with the user's
  role in it. Outsiders already got 404 from `Require`; answer 403 when a
  member's role doesn't allow an action (`ws.CanManage()`, `ws.IsOwner()`).
- Test that someone outside the workspace gets 404 for every route, using
  `workspacestest.SignIn` for each person.
<!-- readysetgo:end -->
