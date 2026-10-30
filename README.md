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

<!-- kickstart:begin db=postgres -->
```bash
make services
```

<!-- kickstart:end -->
```bash
make run
```

Then open <http://localhost:8080/readyz>. `make help` lists every command.
