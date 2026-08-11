# HMS

Hospital Management System platform — multi-zone monorepo.

- `apps/` — Next.js 16 zone apps (shell, medicore, pharmacy, lab) + mobile stubs
- `backend/` — Go modular monolith (`github.com/tesserix/hms`)
- `packages/` — shared JS config and HMS UI compositions
- Design: `docs/superpowers/specs/2026-08-04-hms-repo-setup-design.md`
- ADRs: `docs/adr/`

## Quick start

Requires Docker, Go 1.26, Node 22 (`corepack enable`).

Set `NODE_AUTH_TOKEN` to a GitHub token with `read:packages` (e.g. `export NODE_AUTH_TOKEN=$(gh auth token)`) — required for `@tesserix/web` from GitHub Packages.

    make dev        # infra (Postgres, NATS, Redis, OpenFGA, GIP emulator) + API + web
    make seed       # dev tenant + test user (test@hms.dev / password123), granted tenant_admin
    open http://localhost:4301

`make dev` points the API at the local GIP emulator automatically
(`FIREBASE_AUTH_EMULATOR_HOST=localhost:9099`); override the variable to
target a real GIP project.

> `make seed` must run after `make dev` has started the API once — it writes the
> bootstrap `tenant_admin` membership into tables the API's migrations create.
> The dev OpenFGA runs with an in-memory datastore, so its tuples are lost on
> restart; the reconciler rebuilds them from Postgres at every boot.

Deployment lives in `tesserix-k8s` (charts/apps/hms-*), not here.
