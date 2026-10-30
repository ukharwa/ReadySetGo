# CLAUDE.md

Conventions for working in this repository. Keep this file accurate when a
convention changes. `DECISIONS.md` explains why things are the way they are.

<!-- kickstart:begin template -->
## Working on the template itself

This repository is the kickstart template. Before `make init`, it is a
runnable SQLite app containing every option. `make init` keeps the options a
new project picks and removes the rest:

- Paths belonging to one option are removed whole when that option isn't
  chosen (`migrations/postgres/`, `internal/store/postgres/`, and later
  module folders). The chosen database's folders are flattened:
  `migrations/<db>/` becomes `migrations/`.
- Files for one UI mode are named for it: `web.go`, `web_test.go`, any
  `templates/` folder and `internal/web/` belong to HTML mode; `api.go` and
  `api_test.go` to API mode. Keep mode-specific code (and its imports) in
  those files, so shared files need at most a one-line marker around a call.
- Go files for one database carry a build constraint: `//go:build postgres`
  or `//go:build !postgres`. Other files belong to a database if it is in
  their name (`backup-postgres.sh`) or folder.
- A file left empty by its markers is removed (`deploy/litestream.yml` in a
  Postgres project). Init deletes the other database's files and
  strips the constraint from the rest. Postgres files therefore don't
  compile in the template itself.
- Inside shared files, option-specific lines sit between markers, written
  in the file's own comment syntax (`//`, `#`, `--` or `<!-- -->`), each on
  a line of its own:

  ```go
  // kickstart:begin db=postgres
  …
  // kickstart:end
  ```

  Conditions are `db=sqlite|postgres`, `ui=html|api`, `module=<name>`, and
  `template` (always removed). Blocks may nest. Markers are always removed.
- Init renames `kickstart` to the project name everywhere except
  `DECISIONS.md`, and the module path everywhere.
- After init, nothing from an unchosen option may remain: don't mention
  another database, UI mode or module outside its markers or paths.

Test every combination with `make template-check DB=sqlite|postgres
UI=html|api`. It copies
the working tree (uncommitted changes included) to a temporary folder, runs
init, then tidy, vet, the tests, lint and sqlc, and fails on leftovers. The
Postgres check needs `make services`. Set `TMPDIR` to choose where the copy
goes. The init program lives in `tools/kickstart/`; its unit tests run with
`go test ./tools/...`.

After changing a query or migration, run `make generate`: sqlc writes the
SQLite code to `internal/store/` and the Postgres code to
`internal/store/postgres/`, and the two must have identical Go signatures.
Keep migrations for both databases in step (same numbers and names). A
query that can't be shared goes in `internal/<feature>/sqlite/` and
`internal/<feature>/postgres/` under the same file name, both listed in
`sqlc.yaml`; init flattens the chosen one into the feature folder.
<!-- kickstart:end -->

## Commands

- `make services`: start the dev services. Mailpit's inbox at
  <http://localhost:8025> catches every email the app sends.
  <!-- kickstart:begin db=postgres -->
  Postgres is needed by `run` and `test`.
  <!-- kickstart:end -->
- `make run`: run the HTTP server and job worker (reads `.env`; copy
  `.env.example` first)
- `make worker`: run only the job worker
- `make test`: all tests, with the race detector
- `make migrate`: apply pending migrations; `make migration NAME=add_x`
  creates a new one
- `make generate`: regenerate `internal/store` after changing SQL
  (`make tools` installs sqlc first)
<!-- kickstart:begin db=sqlite -->
- `make backup`: write a database snapshot into `backups/`
<!-- kickstart:end -->
- `make seed`: add `admin@example.com` and `user@example.com`, both with
  the password `dev-password-123` (dev only)
- `go run ./cmd/kickstart user promote EMAIL`: make a user an admin (also
  `demote`, `disable`, `enable`)
- `make up` / `make down`: run the production stack (`compose.yaml`: the
  app behind Caddy) locally at https://localhost:8443
- `make deploy DEPLOY_HOST=user@server`: copy `compose.yaml` and `deploy/`
  to the server, pull the new image and restart
<!-- kickstart:begin db=sqlite -->
- `docker compose exec app /app backup -dir /data/backups` takes a snapshot
  on the server; to restore one, stop the app, then
  `docker compose run --rm app restore -from /data/backups/FILE.db`
<!-- kickstart:end -->
<!-- kickstart:begin db=postgres -->
- `./deploy/backup-postgres.sh` dumps the database into `backups/` on the
  server; its header shows how to restore a dump
<!-- kickstart:end -->
- `make build`, `make fmt`, `make vet`; `make help` lists everything

## Layout

- `cmd/kickstart/`: the only `main` package and the only place dependencies
  are wired together.
  - `main.go` lists the subcommands; each has its own file, such as
    `serve.go` and `migrate.go`.
  - `app.go` holds the setup they share (config, logger, database).
  - `routes.go` mounts every feature's routes, one line per feature.
  <!-- kickstart:begin ui=html -->
  - `web.go` sets up the page renderer and mounts the HTML pages.
  <!-- kickstart:end -->
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
  no HTTP; `sessions.go` the session-store seam; `middleware.go` loads the
  user and guards routes; `emails/` holds the email templates.
  <!-- kickstart:begin ui=html -->
  `web.go` and `templates/` are the account pages.
  <!-- kickstart:end -->
  <!-- kickstart:begin ui=api -->
  `api.go` is the JSON API under `/api/auth/`.
  <!-- kickstart:end -->
<!-- kickstart:begin ui=html -->
- `internal/web/`: the page layout, renderer, stylesheet, HTMX and flash
  messages.
<!-- kickstart:end -->
- `internal/validate/`: input checks and `validate.Errors` (messages per
  field).
- `internal/apptest/`: the end-to-end test harness.
- `migrations/`: goose SQL migrations, embedded into the binary.
- `Dockerfile`, `compose.yaml` (production stack), `compose.dev.yaml` (dev
  services), `deploy/` (Caddyfile, backups).

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
- **Timestamps:** set from Go with `db.Now()` (UTC, microseconds), never by
  database defaults.
- **Emails and usernames:** trim and lowercase in Go before saving or
  looking up, with a unique index on the cleaned value.
- **Other types:** strings are `TEXT`; money is whole cents in an integer;
  every foreign key has an explicit `ON DELETE` rule and an index.
  <!-- kickstart:begin db=sqlite -->
  Integers are `INTEGER` (`int64` in Go).
  <!-- kickstart:end -->
  <!-- kickstart:begin db=postgres -->
  Integers are `bigint` (`int64` in Go; `integer` would be `int32`).
  <!-- kickstart:end -->
- **Database-specific SQL** goes only in migrations and in queries that
  can't be shared; everything else stays portable.

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
  cookies, emails, query strings or database URLs.
- **State:** no package-level mutable state; dependencies are built in
  `main` and passed explicitly. Nothing may assume a single instance.
- **Background work:**
  - Enqueue jobs with `jobs.Enqueue(ctx, store.New(tx), kind, payload)`
    inside the same `db.InTx` as the change that caused them.
  - Register a job's handler, and any recurring task
    (`scheduler.Every`), in `startJobs` in `cmd/kickstart/app.go`.
  - Handlers must be safe to run twice and must respect their context.
  - Never send email during a request: build an `email.Message` and
    `email.Enqueue` it. Tests use `emailtest.Recorder` and run queued jobs
    with `worker.RunOne`.
- **Interfaces:** only for the agreed seams (sessions, rate limiting, jobs,
  email, file storage), defined where they're used.
- **HTTP:**
  - Each feature has a handler type with a `Routes(mux)` method listing its
    routes; mount it in `cmd/kickstart/routes.go`. Every request reaches
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
  <!-- kickstart:begin ui=html -->
  - Pages: parse a feature's `templates/` with `renderer.Pages` and
    `Render` them. Forms post, then redirect with `web.SetFlash`; on bad
    input re-render with `web.Form` and status 422. Use
    `web.SafeRedirect` for any `?next=`.
  <!-- kickstart:end -->
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
- **Tests:** standard `testing` only, table-driven, `t.Parallel()` where
  possible, no assertion libraries. Tests that touch the database use
  `dbtest.New(t)`, which gives each test its own database. End-to-end tests
  use `apptest.New(t)`: register the feature's routes on its `Mux` and drive
  its HTTPS `Server` with `Client()`, reading emails with `TokenFrom`.
  <!-- kickstart:begin db=postgres -->
  It needs `make services` running and `TEST_DATABASE_URL` set.
  <!-- kickstart:end -->
