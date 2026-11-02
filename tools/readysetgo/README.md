# Working on the template itself

The init tool (`go run ./tools/readysetgo init`, behind `make init`) and the
template check (`make template-check`) live here. Init deletes this folder
from every project it creates.

This repository is the ReadySetGo template. Before `make init`, it is a
runnable SQLite app containing every option. `make init` keeps the options a
new project picks and removes the rest:

- Paths belonging to one option are removed whole when that option isn't
  chosen (`migrations/postgres/`, `internal/<feature>/store/postgres/`). The chosen
  database's folders are flattened: `migrations/<db>/` becomes
  `migrations/`.
- An optional module lives in `internal/<module>/`, removed whole when it
  isn't chosen. Its migrations wrap their contents in `module=<name>`
  markers (so they empty and are dropped), and so do its `sqlc.yaml`
  entries, mount lines and nav links. Modules depend only on the core,
  never on each other; they hook into it through seams such as
  `auth.Service.OnUserCreated`.
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
  // readysetgo:begin db=postgres
  …
  // readysetgo:end
  ```

  Conditions are `db=sqlite|postgres`, `ui=html|api`, `module=<name>`, and
  `template` (always removed). A comma-separated list keeps the block if
  any of its conditions holds (`db=sqlite,module=uploads`). Blocks may
  nest. Markers are always removed.
- The notes example's lines sit between `example:begin` and `example:end`
  markers instead. Init keeps them, and the example, in every project;
  `make remove-example` (`tools/removeexample/`) deletes them later.
- Init deletes `LICENSE`: the template's licence isn't the project's.
- Init renames `readysetgo` (in any letter case, so `ReadySetGo` too) to the
  project name everywhere except
  `DECISIONS.md`, and the module path everywhere.
- After init, nothing from an unchosen option may remain: don't mention
  another database, UI mode or module outside its markers or paths.

Test every combination with `make template-check DB=sqlite|postgres
UI=html|api MODULES=none|all|a,b`, adding `REMOVE_EXAMPLE=1` to check a
project after `make remove-example`. It copies
the working tree (uncommitted changes included) to a temporary folder, runs
init, then tidy, vet, the tests, lint and sqlc, and fails on leftovers. The
Postgres check needs `make services`. Set `TMPDIR` to choose where the copy
goes. The init program lives in `tools/readysetgo/`; its unit tests run with
`go test ./tools/...`.

After changing a query or migration, run `make generate`: sqlc writes each
feature's SQLite code to `internal/<feature>/store/` and its Postgres code
to `internal/<feature>/store/postgres/`, and the two must have identical Go
signatures. Where sqlc infers a parameter's type differently per database
(`LIMIT`, `OFFSET`, a parameter compared with a literal), cast it:
`CAST(@skip AS BIGINT)`, `CAST(@email AS TEXT)`.
Keep migrations for both databases in step (same numbers and names). A
query that can't be shared goes in `internal/<feature>/sqlite/` and
`internal/<feature>/postgres/` under the same file name, both listed in
`sqlc.yaml`; init flattens the chosen one into the feature folder.
