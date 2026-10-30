# kickstart

<!-- kickstart:begin template -->
A starting point for Go web apps and APIs backed by SQLite or PostgreSQL:
database, authentication, Caddy, Docker and tooling, with no business logic.

> **Work in progress.** The template is being built milestone by milestone;
> this README is completed in the final one. `DECISIONS.md` explains every
> choice.
<!-- kickstart:end -->

## Run it locally

Requires Go 1.27 or newer.

```bash
cp .env.example .env
```

```bash
make services
```

```bash
make run
```

Then open <http://localhost:8080/readyz>. `make help` lists every command.

`make services` starts:

- Mailpit, which catches the app's email: read it at <http://localhost:8025>
<!-- kickstart:begin db=postgres -->
- Postgres
<!-- kickstart:end -->
