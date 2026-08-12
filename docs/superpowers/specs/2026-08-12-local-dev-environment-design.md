# Local development environment — design

Resolves: [#714](https://github.com/tesserix/hms/issues/714) — [DevEx] Local
development environment (CNPG, Redis, NATS, Keycloak, OpenFGA)

Date: 2026-08-12

## Problem

Issue #714 asks for a one-command local stack with seeded tenants, users and FGA
relationships, so that no engineer stubs out tenancy, auth or events and
discovers the integration bugs late.

Most of that already exists. `docker-compose.dev.yml` plus `make up` brings up
Postgres, NATS (JetStream), Redis, OpenFGA and the GIP emulator, runs
migrations, seeds two tenants and three role assignments across two users, and
starts the API and four zone apps. `make verify-local` asserts the whole stack
is healthy.

Four acceptance criteria are not met:

1. **Preflight.** A port conflict or a missing prerequisite produces a container
   loop or an unbounded wait, not "a clear preflight message naming the conflict
   and fix".
2. **Reset.** There is no command returning the stack to a clean seeded state.
3. **Sleep and restart.** No container declares a restart policy, so the stack
   does not reliably survive a laptop sleep or a Docker restart.
4. **Documentation.** Setup notes live in the README, but the new commands and
   the stack's deviation from the issue text are unrecorded.

This design covers those four and nothing else.

## Stack deviations from the issue text

Two dependencies named in #714 predate decisions already taken. This issue
delivers their equivalents.

| Issue text | Delivered | Why |
| --- | --- | --- |
| Keycloak, both realms | Firebase Auth (GIP) emulator on `:9099` | Superseded by [ADR 0002](../../adr/0002-gip-not-keycloak.md). There are no realms to stand up. |
| CNPG | `postgres:16-alpine` in Compose | CNPG is a Kubernetes operator with no role in a Compose stack. The Compose Postgres already runs with the non-superuser `hms_app` role and forced RLS that production uses, which is the property the story depends on. |

Redis, NATS and OpenFGA are unchanged.

Recorded permanently in `docs/adr/0003-local-dev-stack.md` so the next reader of
#714 does not have to rediscover it.

## Out of scope

Each has its own issue and its own spec. They are named here only because they
were raised while scoping this one.

- **Kubernetes-shaped local environment** (sandboxctl, Dockerfiles, Helm charts,
  OpenFGA and GIP as subcharts) — issue #7. sandboxctl needs a Dockerfile and a
  chart, and HMS has neither yet.
- **Feature flags** (OpenFeature API with a GrowthBook provider, `flags.yaml`
  governance) — issues #9, #691, #704.
- **Secrets backend** (External Secrets Operator with GCP Secret Manager or
  OpenBao behind it) — issue #45.

## Design

### 1. `scripts/preflight.sh`

Runs as the first step of `make up`. Exits non-zero before anything touches
Docker.

**Reports every failure, not the first.** A fresh machine usually has more than
one thing wrong, and discovering them one boot at a time is the experience the
acceptance criterion objects to.

| Check | Message on failure |
| --- | --- |
| Docker daemon reachable | `Docker is not running — start Docker Desktop (or 'colima start')` |
| Compose v2 available | `Compose v2 required — 'docker compose version' failed` |
| Go >= 1.26 | `Go 1.26+ required (backend/go.mod: 1.26.5), found <version>` |
| Node >= 22 | `Node 22+ required (package.json engines), found <version>` |
| pnpm present | `pnpm missing — run 'corepack enable'` |
| `NODE_AUTH_TOKEN` set | `NODE_AUTH_TOKEN unset — export NODE_AUTH_TOKEN=$(gh auth token)` |
| Ports free | `port 5432 held by pid <pid> (<command>) — stop it, or 'make down' if it is a stale HMS container` |

Ports checked: 5432, 4222, 8222, 6379, 8090, 9099, 8080, 4301, 4302, 4303, 4304.

**Re-runnability is a hard requirement.** `make up` is documented as safe to
re-run and seeding is idempotent. A port held by this repo's own `hms-dev`
Compose project, or by a process whose working directory is inside the
repository, is therefore not a failure — it is a stack that is already up. Only
a foreign holder fails the check. This reuses the ownership test
`scripts/dev-down.sh` already implements, so both scripts agree on what "ours"
means; the shared logic moves into a small sourced helper rather than being
duplicated.

Output follows the `verify-local.sh` convention (`  ok    <name>` /
`  FAIL  <name>`) so the three scripts read as one family.

### 2. `scripts/reset-dev.sh`, exposed as `make reset`

Sequence: `dev-down.sh` → `docker compose down -v` (drops the `pgdata` and
`natsdata` volumes) → `make dev-infra` → `make seed`.

Ends with clean, seeded infrastructure and prints an instruction to run
`make up`, rather than starting the application servers itself. That keeps the
target non-blocking and scriptable, and `make up` is where the foreground
API-plus-web behaviour belongs.

**Prompts before destroying volumes**, with `RESET_YES=1` to skip for scripts
and CI. `docker compose down -v` is unrecoverable and the volume may hold a
day's hand-entered test data. The repository already sets this precedent:
`dev-down.sh` refuses to kill processes it does not own rather than assuming
the developer meant it.

### 3. Surviving sleep and restart

`restart: unless-stopped` on all five services. `unless-stopped` rather than
`always` so that a container a developer deliberately stopped stays stopped.

**A determinism bug fixed in passing.** The GIP emulator service runs
`npx -y firebase-tools@13`, which resolves a floating minor version on every
container start. Different machines therefore run different emulator builds, and
every start requires network access — directly contradicting the "seeded data
identical across machines" acceptance criterion. The fix is to pin an exact
`firebase-tools` version and mount a named volume for the npm cache, making
restarts fast and offline-capable.

### 4. Documentation

- `README.md` — `make reset`, the preflight step, and the pinned emulator.
- `docs/adr/0003-local-dev-stack.md` — the Keycloak and CNPG deviations, and the
  decision to keep Compose as the inner loop while #7 owns the cluster-shaped
  environment.

## Error handling

Preflight failures are the feature, not an edge case: every check names the
conflict and the fix, and the script never proceeds past a failure into a state
that produces a confusing downstream error. `reset` refuses to destroy volumes
without confirmation. Neither script kills or deletes anything it cannot prove
belongs to this repository.

## Testing

Test-driven, red first.

`scripts/preflight.test.sh` — plain bash, no new dependency — run via
`make test-scripts`:

- an occupied foreign port produces a message naming that port and its fix, exit 1
- a port held by this repo's own container passes (the re-runnability guarantee)
- unset `NODE_AUTH_TOKEN` is reported
- several simultaneous failures are all reported, not just the first
- a clean environment exits 0

Reset is covered by an integration check: seed, mutate a seeded row, reset,
assert the mutation is gone and the seeded rows are back — which is the
"deterministic, identical across machines" criterion expressed as a test.

`.github/workflows/ci.yml` exists; `make test-scripts` is wired into it.

## Files

New:

- `scripts/preflight.sh`
- `scripts/preflight.test.sh`
- `scripts/reset-dev.sh`
- `docs/adr/0003-local-dev-stack.md`

Changed:

- `Makefile` — preflight as the first step of `up`; new `reset` and
  `test-scripts` targets
- `docker-compose.dev.yml` — restart policies, pinned `firebase-tools`, npm
  cache volume
- `scripts/dev-down.sh` — ownership test extracted to a shared helper
- `README.md`
- `.github/workflows/ci.yml`

## Acceptance criteria mapping

| Criterion | Where met |
| --- | --- |
| Stack starts, service boots, login works, two-tenant isolation testable | Already met by `make up` and `scripts/seed-dev.mjs`; verified by `make verify-local` |
| Port conflict or missing prerequisite names the conflict and fix | `scripts/preflight.sh` |
| Reset command returns a clean seeded state | `make reset` |
| Stack survives sleep and restart | `restart: unless-stopped` |
| Seeded data identical across machines | Existing deterministic seed, plus the pinned `firebase-tools` fix |
| Works on Apple Silicon and Linux | All five images are multi-arch; no change needed, asserted in docs |

## Known limitations

- OpenFGA uses the in-memory datastore, so tuples are lost on restart. Postgres
  remains the system of record and the API's reconciler rebuilds them on boot.
  This is existing, documented behaviour and is left alone.
- Preflight checks the ports HMS uses. It cannot predict a port taken between
  the check and the container start; that race is accepted.
- `make reset` destroys local data by design. There is no backup step.
