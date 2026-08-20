# Helivanta

Helivanta — a hospital management system platform, multi-zone monorepo.

- `apps/` — Next.js 16 zone apps (shell, medicore, pharmacy, lab) + mobile stubs
- `backend/` — Go modular monolith (`github.com/tesserix/helivanta`)
- `packages/` — shared JS config and Helivanta UI compositions
- Design: `docs/superpowers/specs/2026-08-04-hms-repo-setup-design.md`
- ADRs: `docs/adr/`

## Quick start

Requires Docker, Go 1.26, Node 22 (`corepack enable`). No registry
credentials: `@tesserix/web` is published to the public npm registry, so
`pnpm install` on a fresh clone needs nothing but network access.

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
Go, Node, pnpm, the Zitadel dev masterkey's length, `lsof`, and
every port the stack uses, and reports **every** problem at once with the
fix for each — a fresh machine usually has more than one. A port held by
this repo's own containers or processes is not a conflict, so re-running
`make up` on a stack that is already running still works.

`make reset` returns the stack to a clean seeded state: it stops
everything, drops the Postgres, NATS and Zitadel volumes, restarts
infrastructure and re-seeds. Dropping the Zitadel volume means its org,
users and OIDC app are gone too — `dev-infra` re-provisions all of it from
scratch via `scripts/zitadel-bootstrap.mjs`, so this is safe, but the
resulting `ZITADEL_CLIENT_ID` will be a different value than before (a new
Zitadel instance, not the same one restored). It prompts first, because
dropping those volumes is unrecoverable; `RESET_YES=1 make reset` skips the
prompt for scripts.

Seeded accounts (Zitadel, verified by a real hosted-UI login as part of
`make seed` — see `scripts/seed-dev.mjs`):

| User                 | Password      | Tenant                 | Role           | Sees              |
| -------------------- | ------------- | ---------------------- | -------------- | ----------------- |
| `test@helivanta.dev`       | `HmsDev123!`  | `1111…1111` (default)  | `tenant_admin` | every zone        |
| `test@helivanta.dev`       | `HmsDev123!`  | `2222…2222`            | `pharmacist`   | Pharmacy only     |
| `pharmacist@helivanta.dev` | `HmsDev123!`  | `1111…1111`            | `pharmacist`   | Pharmacy only     |

`test@helivanta.dev` is deliberately a member of **two** tenants so tenant
switching is exercisable end to end: OpenFGA
already carries both memberships, and `POST /v1/iam/me/tenant` re-mints the
session for either without an IdP round trip (spec D3).

Sign in at http://helivanta.localhost:4301/login (**not** `localhost:4301` —
see "Hosts" below). The shell redirects to Zitadel's
hosted login (authorization code + PKCE), and the callback exchanges the ID
token for a Helivanta session via `POST /v1/auth/login` — the API mints and
sets the cookie, so the browser never holds an IdP token as its session.

The login page you land on is Zitadel's **stock** UI. Branding comes later
from a design-system component consumed by the org's `zitadel-login` build
(spec D5a); the redirect target does not change when it lands.

To check auth without a browser: `make verify-local` drives the real
API-level exchange (a Zitadel token through the hosted login UI →
`POST /v1/auth/login` → an authenticated `/v1` call), or run
`node scripts/zitadel-verify-login.mjs` directly.

Stop everything with `make down`.

`make up` provisions and seeds a local Zitadel v4.15.3 (spec
`docs/superpowers/specs/2026-08-15-zitadel-auth-design.md`, topology
`docs/superpowers/specs/2026-08-15-zitadel-tenancy-topology-design.md`):
its own Postgres, a Helivanta org/project/OIDC app (`scripts/zitadel-bootstrap.mjs`,
fully declarative — no browser step, even for the first machine credential),
and the split `zitadel-login` service fronted by a Caddy reverse proxy
(`dev/zitadel/Caddyfile`), matching production's topology rather than the
simpler core-only login mode. `ZITADEL_ISSUER_URL` and `ZITADEL_CLIENT_ID`
are what `make dev-api` (and therefore `make up`) passes the API; override
`ZITADEL_ISSUER_URL` to point dev at a different instance.

The API refuses to start with no `SESSION_SIGNING_KEY`, and refuses the
well-known dev key (`HELIVANTA_DEV_SESSION_SIGNING_KEY`) outside `HELIVANTA_ENV=dev` —
otherwise a forged session would be accepted in what looks like production.
`make dev-api` (and therefore `make up`) sets `HELIVANTA_ENV=dev` for you; a bare
`go run ./cmd/api` does not, so set it yourself when running the API outside
`make`.

Hosts. The stack serves the app at **`helivanta.localhost`** and Zitadel at
**`auth.tesserix.localhost`**. Neither needs an `/etc/hosts` entry — Chrome and
macOS both resolve `*.localhost` to loopback, and Chrome still treats it as a
secure context. Use those hostnames, not `localhost`: Zitadel resolves its
instance from the `Host` header and answers "Instance not found" to anything
else, and the app's OIDC redirect URIs are registered against
`helivanta.localhost` byte-for-byte.

This is deliberate, and it is not cosmetic (#916, design spec D6). Production
serves the app at `helivanta.app` and the IdP at `auth.tesserix.app` —
**different registrable domains**, so every browser request from the app to the
IdP is *cross-site* and a `SameSite=Lax` IdP cookie is withheld. The old dev
setup put both on `localhost` and differed only by port, and **ports are not
part of a "site"** — so dev was *same-site*, a cross-site defect worked
perfectly here, and #916 reached production with no test able to fail.
`helivanta.localhost` and `tesserix.localhost` are distinct registrable domains,
so dev now reproduces production's relationship;
`e2e/tests/cross-site-harness.spec.ts` proves it against a real browser rather
than assuming it.

Override via `HELIVANTA_WEB_HOST` / `HELIVANTA_ZITADEL_HOST` if these names
collide with something on your machine — every consumer reads those variables
rather than a literal (compose, `scripts/zitadel-bootstrap.mjs`, the API's
whole Zitadel configuration — `ZITADEL_ISSUER_URL`,
`ZITADEL_HOSTED_LOGIN_URL` and `HELIVANTA_WEB_ORIGIN` are all derived from
these two variables in the Makefile and exported, so no hardcoded dev
default in `backend/internal/config/` can answer for a stale host —
`scripts/e2e.sh`, `scripts/lib/zitadel.mjs`'s dev redirect URIs, and the e2e
suite via `e2e/tests/support/hosts.ts` — which reads `.env` itself
(`load-env.ts`) so the guard sees your override even on the Make-less
`pnpm --filter e2e exec playwright test` path), so an override is honoured end
to end. Verified rather than asserted: the whole
stack was re-provisioned on a third hostname and `make e2e` passed all three
phases against it. What you
may **not** do is point both at the same registrable domain: that returns the
harness to same-site and makes #916's class of defect invisible again.
`cross-site-harness.spec.ts` derives the hosts it probes from the running
configuration and fails if you try, so this is a control rather than a request.

Changing `HELIVANTA_ZITADEL_HOST` on an already-provisioned stack needs
`RESET_YES=1 make reset`: Zitadel writes its instance domain once, at
first-instance time, and answers "Instance not found" to any other host
afterwards. `scripts/preflight.sh` checks for exactly that and tells you, so
`make up` fails with the cause and the fix rather than with a bare
`HTTP 404 Instance not found` from the bootstrap script.

Ports (defaults): shell 4301, medicore 4302, pharmacy 4303, lab 4304, API
8080, Postgres 5432, NATS 4222 (monitoring 8222), Redis 6379, OpenFGA 8090,
Zitadel 20080 (its own Postgres 5433). The four zone-app ports are fixed —
they're Next.js dev servers configured in their own package scripts — but
every other port is a host-side default only, overridable via `.env`:

    cp .env.example .env
    # .env
    HELIVANTA_PG_PORT=15432   # something else already has 5432
    HELIVANTA_REDIS_PORT=16379

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
  writes to it. It also depends on `make dev-infra` having already run:
  seeding reads the `helivanta-seed-bot` machine PAT and `ZITADEL_CLIENT_ID` from
  `dev/zitadel/secrets/`, which `scripts/zitadel-bootstrap.mjs` writes.
- `make seed` verifies every account by completing a REAL login through
  Zitadel's hosted UI (a headless Chromium via Playwright), not by trusting
  the creation call's status code — `POST /v2/users/human` returns 200
  while silently discarding fields it does not recognise, so a 200 is not
  evidence an account can actually authenticate (see
  `docs/superpowers/spikes/2026-08-15-zitadel-spike.md`'s "Task 0"). This
  means `make seed` needs a Chromium binary Playwright can drive —
  `pnpm install` pulls in `@playwright/test`; run `pnpm exec playwright
  install chromium` once if seeding fails with a "browser not found" error.
- Backend tests need Docker (testcontainers): `cd backend && go test ./...`
- `make verify-local` retries the zone checks: `next dev` compiles a route on
  its first request, so a cold zone can take tens of seconds to answer once
  and milliseconds thereafter. A genuinely down service still fails.
- Zitadel does NOT fail fast on a wrong-length masterkey — a value that
  isn't exactly 32 bytes crash-loops on every restart instead of refusing
  once (`docker-compose.dev.yml`'s `zitadel` service comment has the full
  story). `scripts/preflight.sh` checks the length before Docker is
  touched, so this is caught in one place.
- All containers use `restart: unless-stopped`, so the stack comes
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
