# CLAUDE.md

Conventions for working in this repository. Keep this file accurate when a
convention changes. `DECISIONS.md` explains why things are the way they are.

<!-- kickstart:begin template -->
## Working on the template itself

This repository is the kickstart template. Before `make init`, it is a
runnable app containing every option. `make init` keeps the options a new
project picks and removes the rest:

- Paths belonging to one option are removed whole when that option isn't
  chosen (for example `migrations/postgres/`, `internal/workspaces/`).
- Inside shared files, option-specific lines sit between markers, written
  in the file's own comment syntax:

  ```go
  // kickstart:begin db=postgres
  …
  // kickstart:end
  ```

  Keys are `db=sqlite|postgres`, `ui=html|api`, `module=<name>`, and
  `template` (always removed). Markers are always removed.
- After init, nothing from an unchosen option may remain. Don't mention
  another database, UI mode or module outside its markers or paths.
<!-- kickstart:end -->

## Commands

- `make run`: run the HTTP server (reads `.env`; copy `.env.example` first)
- `make test`: all tests, with the race detector
- `make build`: static binary in `bin/`
- `make fmt`, `make vet`

## Layout

- `cmd/kickstart/`: the only `main` package and the only place dependencies
  are wired together.
  - `main.go` dispatches subcommands.
  - One file per subcommand (`serve.go`).
  - `routes.go` registers every route, so the URL space reads in one place.
- `internal/config/`: reads every environment variable. Nothing else calls
  `os.Getenv`.
- `internal/logging/`: builds the slog logger.
- `internal/httpx/`: HTTP server setup, health endpoints, and helpers shared
  by every feature's handlers.

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
  cookies, emails or query strings.
- **State:** no package-level mutable state; dependencies are built in
  `main` and passed explicitly. Nothing may assume a single instance.
- **Interfaces:** only for the agreed seams (sessions, rate limiting, jobs,
  email, file storage), defined where they're used.
- **HTTP:** GET requests never change state.
- **Tests:** standard `testing` only, table-driven, `t.Parallel()` where
  possible, no assertion libraries.
