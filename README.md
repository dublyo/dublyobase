# dublyobase

**An all-in-one, self-hostable Postgres backend** — the PocketBase developer
experience, grounded in real Postgres. dublyobase provisions and supervises
**Postgres 16/17/18** as child processes, and layers auth, sessions, file
storage, SMTP and logs behind one admin panel, shipped as a single container.

> Status: **early development.** See [dublyobase-dev.md](../dublyobase-dev.md)
> for the full architecture and milestone roadmap.

## What makes it different

- **Multi-version Postgres, supervised in one image.** Pick 16, 17 or 18 per
  project; dublyobase runs a cluster per major version for you.
- **Security enforced in Postgres, not faked in the app.** Per-project roles +
  `SET LOCAL ROLE` + `FORCE ROW LEVEL SECURITY`; hashed API keys; encrypted secrets.
- **Collections model.** Define tables/fields/rules in the panel; real Postgres
  tables, auto REST + realtime, and native RLS policies are generated for you.

## Quick start (development)

```bash
# list supported versions and whether their binaries are found
go run . cluster list

# start the API + admin shell
go run . serve            # -> http://localhost:8090  (GET /api/health)

# (needs local Postgres binaries or PGSUPER_PG*_BINDIR set)
go run . cluster ensure 18
go run . cluster provision 18 myapp
```

## CLI

| Command | Description |
|---|---|
| `dublyobase serve` | Start the HTTP API + admin UI |
| `dublyobase cluster list` | Show supported versions + binary detection |
| `dublyobase cluster ensure <v>` | initdb + start a cluster (16/17/18) |
| `dublyobase cluster provision <v> <name>` | Create a project DB in a cluster |

## License

MIT
