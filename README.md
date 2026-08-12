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
    make up             # infra + migrations + seed + API (8080) + all four zone apps
    make verify-local   # asserts every process is healthy (separate terminal)
    make down           # stops everything

`make up` runs in the foreground. Ctrl-C stops the API and web servers;
`make down` then stops the containers **and** any app processes still
holding ports 4301-4304 or 8080 — `docker compose down` on its own leaves
those running against infrastructure that no longer exists. `make down`
only ever stops processes whose working directory is inside this repo, so
an unrelated service of yours on 8080 is reported and left alone.

Seeding is idempotent, so re-running `make up` is safe. The individual
steps are still available if you want them: `make dev-infra`, `make
migrate`, `make seed`, `make dev-api`, `make dev-web`.

`make up` runs `scripts/preflight.sh` first. It checks Docker, Compose v2,
Go, Node, pnpm, `NODE_AUTH_TOKEN` and all eleven ports the stack uses, and
reports **every** problem at once with the fix for each — a fresh machine
usually has more than one. A port held by this repo's own containers or
processes is not a conflict, so re-running `make up` on a stack that is
already running still works.

`make reset` returns the stack to a clean seeded state: it stops
everything, drops the Postgres and NATS volumes, restarts infrastructure
and re-seeds. It prompts first, because dropping those volumes is
unrecoverable; `RESET_YES=1 make reset` skips the prompt for scripts.

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

Stop everything with `make down`.

`make up` points the API at the local GIP emulator automatically
(`FIREBASE_AUTH_EMULATOR_HOST=localhost:9099`); override the variable to
target a real GIP project.

Ports (defaults): shell 4301, medicore 4302, pharmacy 4303, lab 4304, API
8080, Postgres 5432, NATS 4222 (monitoring 8222), Redis 6379, OpenFGA 8090,
GIP emulator 9099. The four zone-app ports are fixed — they're Next.js dev
servers configured in their own package scripts — but every other port is a
host-side default only, overridable via `.env`:

    cp .env.example .env
    # .env
    HMS_PG_PORT=15432   # something else already has 5432
    HMS_REDIS_PORT=16379

Compose reads `.env` directly; `make` pulls the same file in (`-include
.env`) so both sides always agree, and `make dev-api`/`make seed` pass
connection URLs derived from it. Container-internal ports never change —
only the host side of each mapping shifts — so this is purely about freeing
yourself from a clash with someone else's stack, never a way to reconfigure
the containers themselves. See `.env.example` for the full variable list.

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
- `firebase-tools` is pinned to an exact version in `docker-compose.dev.yml`
  and its npm download is cached in the `npmcache` volume. The previous
  floating `@13` resolved a different minor on every container start, so
  two machines could run different emulator builds.
- All five containers use `restart: unless-stopped`, so the stack comes
  back after a laptop sleep or a Docker restart. A container you stopped
  deliberately stays stopped. Note: OpenFGA uses an in-memory datastore, so
  a Docker restart brings the container back with no tuples — those are
  only rebuilt by the API's boot reconciler, and the API is a host process
  (not a container), so it doesn't restart on its own. Re-run `make up`
  after a Docker restart to get permissions working again. A laptop sleep
  doesn't kill any container, so it needs no such follow-up.
- Useful targets: `make test` (Go + web), `make test-scripts` (shell tests),
  `make preflight`, `make reset`, `make lint-go`, `make coverage-go`,
  `make e2e` (Playwright, needs the stack up), `make new-module NAME=<name>`.

Deployment lives in `tesserix-k8s` (charts/apps/hms-*), not here.
