# HMS

Hospital Management System platform — multi-zone monorepo.

- `apps/` — Next.js 16 zone apps (shell, medicore, pharmacy, lab) + mobile stubs
- `backend/` — Go modular monolith (`github.com/tesserix/hms`)
- `packages/` — shared JS config and HMS UI compositions
- Design: `docs/superpowers/specs/2026-08-04-hms-repo-setup-design.md`
- ADRs: `docs/adr/`

## Quick start

Requires Docker, Go 1.26, Node 22 (`corepack enable`).

Set `NODE_AUTH_TOKEN` to a GitHub token with `read:packages`
(`export NODE_AUTH_TOKEN=$(gh auth token)`) — required for `@tesserix/web`
from GitHub Packages.

    pnpm install
    make dev-infra      # Postgres, NATS, Redis, OpenFGA, GIP emulator
    make seed           # migrations + dev tenant + two test users
    make dev            # API (8080) + all four zone apps
    make verify-local   # asserts every process is healthy

Log in at http://localhost:4301/login:

| User                 | Password      | Tenant                 | Role           | Sees              |
| -------------------- | ------------- | ---------------------- | -------------- | ----------------- |
| `test@hms.dev`       | `password123` | `1111…1111` (default)  | `tenant_admin` | every zone        |
| `test@hms.dev`       | `password123` | `2222…2222`            | `pharmacist`   | Pharmacy only     |
| `pharmacist@hms.dev` | `password123` | `1111…1111`            | `pharmacist`   | Pharmacy only     |

`test@hms.dev` is deliberately a member of **two** tenants so tenant
switching is exercisable by hand: log in as that user and use the hospital
picker in the sidebar. Switching re-mints the session with the target
tenant's claim, so the visible zones change from "all" to "Pharmacy only".
`pharmacist@hms.dev` shows permission gating without switching.

Stop everything with `make dev-down`.

`make dev` points the API at the local GIP emulator automatically
(`FIREBASE_AUTH_EMULATOR_HOST=localhost:9099`); override the variable to
target a real GIP project.

Ports: shell 4301, medicore 4302, pharmacy 4303, lab 4304, API 8080,
Postgres 5432, NATS 4222, Redis 6379, OpenFGA 8090, GIP emulator 9099.

### Notes

- The dev OpenFGA uses an in-memory datastore, so its tuples vanish on
  restart. Postgres is the system of record — restarting the API rebuilds
  every tuple via the reconciler.
- `make seed` runs `make migrate` first (`backend/cmd/migrate`, a
  migrate-only entrypoint with no NATS/OpenFGA dependency), so it works on
  a fresh clone with no API running — `iam_members` exists before seed
  writes to it.
- Backend tests need Docker (testcontainers): `cd backend && go test ./...`
- `make verify-local` retries the zone checks: `next dev` compiles a route on
  its first request, so a cold zone can take tens of seconds to answer once
  and milliseconds thereafter. A genuinely down service still fails.
- Useful targets: `make test` (Go + web), `make lint-go`, `make coverage-go`,
  `make e2e` (Playwright, needs the stack up), `make new-module NAME=<name>`.

Deployment lives in `tesserix-k8s` (charts/apps/hms-*), not here.
