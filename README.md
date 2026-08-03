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
    make seed       # dev tenant + test user (test@hms.dev / password123)
    open http://localhost:4301

Deployment lives in `tesserix-k8s` (charts/apps/hms-*), not here.
