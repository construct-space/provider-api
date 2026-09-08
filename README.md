# provider-api

Construct's AI provider plane:

- **Public catalog** — the list of providers + models the operator
  consumes via `modelspec.load`. Moved out of source-api so source
  stays focused on identity/orgs.
- **Construct managed provider** — multi-model OpenAI-compatible
  proxy backed by credits (100 free/day, paid top-ups, no rollover).
- **Credits ledger** — append-only Postgres ledger + per-user balance.
- **Control plane** — staff-only CRUD over models, upstream keys,
  global config, user grants/blocks. Driven by oracle-web.

See `construct-app/docs/plans/2026-05-11-construct-provider-credits.md`
for the full design + phased rollout.

## Layout

```
api/provider/
  main.go                  — http.ServeMux, health + (TODO) catalog + chat
  Dockerfile               — multi-stage build, ENV PORT=80
  captain-definition       — CapRover deploy descriptor
  .env.sample              — required env vars (CapRover panel sets these)
  internal/
    config/                — env loading
    database/              — gorm Postgres connection to `credits` DB
    gwauth/                — gateway-trust helper (X-Internal-Secret + X-Auth-*)
    middleware/            — Logger, CORS, AdminAuth
    handlers/              — health (Phase 1)
    models/                — gorm models (added per phase)
```

## Deploy

CapRover app `provider-api`, repo `construct-space/provider-api`.
Env vars per `.env.sample` (see header note on
`CREDITS_UPSTREAM_KEY_SECRET` — losing it means re-pasting every
upstream key in Oracle).
