.PHONY: up down dev dev-infra dev-down dev-api dev-web migrate seed secret-session-key test test-go coverage-go test-web test-scripts e2e lint-go new-module verify-local preflight reset

# Docker Compose reads .env in the project directory automatically for
# ${VAR} substitution in docker-compose.dev.yml; Make does not read it on
# its own. Pulling it in here too means one file drives both, instead of
# two independently maintained sets of port defaults that will eventually
# drift apart. A missing .env is fine — -include swallows the error and the
# ?= defaults below cover every variable.
-include .env

# Written by scripts/zitadel-bootstrap.mjs (run from `dev-infra`, below)
# once the Helivanta org/project/app exist in the local Zitadel — ZITADEL_CLIENT_ID
# cannot be a fixed default the way the other HELIVANTA_* ports are, because
# Zitadel assigns it at creation time. Absent on a fresh clone before the
# first `make dev-infra`/`make up`; -include swallows that the same way it
# swallows a missing .env, and `dev-api` below refuses with an actionable
# message rather than silently booting with an empty client ID.
-include dev/zitadel/secrets/zitadel.env

# Host-side ports for the local stack, overridable via .env (copy
# .env.example) so a developer whose ports are already taken by unrelated
# work can shift the whole stack instead of being blocked outright. Only
# the host side ever changes — container-internal ports are fixed, see
# docker-compose.dev.yml.
HELIVANTA_PG_PORT ?= 5432
HELIVANTA_NATS_PORT ?= 4222
HELIVANTA_NATS_MONITOR_PORT ?= 8222
HELIVANTA_REDIS_PORT ?= 6379
HELIVANTA_OPENFGA_PORT ?= 8090
HELIVANTA_ZITADEL_PORT ?= 20080
HELIVANTA_ZITADEL_PG_PORT ?= 5433
HELIVANTA_API_PORT ?= 8080

# Connection strings derived from the ports above, for dev-api/migrate/seed.
# backend/internal/config/config.go and scripts/seed-dev.mjs already default
# to these exact values (on the stock ports) when the env var is unset, so
# ?= only ever takes over here — a developer who has explicitly exported one
# of these keeps their own value.
APP_DATABASE_URL ?= postgres://hms_app:hms_app@localhost:$(HELIVANTA_PG_PORT)/helivanta?sslmode=disable
ADMIN_DATABASE_URL ?= postgres://helivanta:helivanta@localhost:$(HELIVANTA_PG_PORT)/helivanta?sslmode=disable
NATS_URL ?= nats://localhost:$(HELIVANTA_NATS_PORT)
OPENFGA_URL ?= http://localhost:$(HELIVANTA_OPENFGA_PORT)
ZITADEL_ISSUER_URL ?= http://localhost:$(HELIVANTA_ZITADEL_PORT)

# API_URL is what each app's next.config.ts rewrites /api to (server side).
API_URL ?= http://localhost:$(HELIVANTA_API_PORT)

# apps/shell's browser-side OIDC client (apps/shell/lib/oidc.ts) needs the
# SAME issuer/client-id the backend verifies against, just under
# NEXT_PUBLIC_ names so Next.js inlines them into the client bundle.
# ZITADEL_CLIENT_ID is only ever real once scripts/zitadel-bootstrap.mjs
# has run (see the `-include` at the top of this file) — `up`'s recipe
# re-invokes `$(MAKE)` for dev-api/dev-web AFTER dev-infra has already run
# it, so that recursive invocation re-parses this file with the real
# value already written to dev/zitadel/secrets/zitadel.env. A developer
# running `make dev-web` on its own, before `make dev-infra`, gets an
# empty NEXT_PUBLIC_ZITADEL_CLIENT_ID — apps/shell/lib/env.ts refuses to
# construct with one, the same fail-closed shape as `dev-api`'s own guard
# just above.
NEXT_PUBLIC_ZITADEL_ISSUER_URL ?= $(ZITADEL_ISSUER_URL)
NEXT_PUBLIC_ZITADEL_CLIENT_ID ?= $(ZITADEL_CLIENT_ID)

export HELIVANTA_PG_PORT HELIVANTA_NATS_PORT HELIVANTA_NATS_MONITOR_PORT HELIVANTA_REDIS_PORT HELIVANTA_OPENFGA_PORT HELIVANTA_ZITADEL_PORT HELIVANTA_ZITADEL_PG_PORT HELIVANTA_API_PORT
export APP_DATABASE_URL ADMIN_DATABASE_URL NATS_URL OPENFGA_URL ZITADEL_ISSUER_URL
export API_URL
export ZITADEL_CLIENT_ID
export NEXT_PUBLIC_ZITADEL_ISSUER_URL NEXT_PUBLIC_ZITADEL_CLIENT_ID

# Zitadel refuses to boot with a masterkey that is not EXACTLY 32 bytes —
# but not by failing fast: it crash-loops on every restart with "masterkey
# must be 32 bytes, but is N" buried in its logs, which reads as a flaky
# container rather than a one-line config mistake (reproduced live while
# wiring this stack up — see docker-compose.dev.yml's zitadel service
# comment). scripts/preflight.sh checks the length before Docker is ever
# touched, so a wrong value is caught in one place rather than
# rediscovered per developer via a log grep.
HELIVANTA_DEV_ZITADEL_MASTERKEY ?= HmsDevZitadelMasterKey32BytesXXX
export HELIVANTA_DEV_ZITADEL_MASTERKEY

# Make auto-imports every shell environment variable as a make variable, so
# an ambient PREFLIGHT_SKIP=1 — left over from debugging, or copied from a
# .envrc — would silently disable preflight for every `make up` and `make
# dev-infra`, with no sign anything changed. Clear it when it came from the
# environment; a deliberate `make dev-infra PREFLIGHT_SKIP=1` on the command
# line (see reset-dev.sh) has a different make `origin` and still wins.
ifeq ($(origin PREFLIGHT_SKIP),environment)
  override PREFLIGHT_SKIP :=
endif

preflight:
	@if [ "$(PREFLIGHT_SKIP)" = "1" ]; then \
		echo "Preflight skipped (PREFLIGHT_SKIP=1) — already checked by the caller."; \
	else \
		bash scripts/preflight.sh; \
	fi

dev-infra: preflight
	docker compose -f docker-compose.dev.yml up -d --wait postgres nats redis openfga
	docker compose -f docker-compose.dev.yml up -d zitadel-db zitadel zitadel-login zitadel-proxy
	@printf 'Waiting for Zitadel on :$(HELIVANTA_ZITADEL_PORT)…'
	@until curl -fsS --max-time 2 http://localhost:$(HELIVANTA_ZITADEL_PORT)/debug/healthz >/dev/null 2>&1; do printf '.'; sleep 1; done
	@echo ' ready.'
	@# Provisions the Helivanta org/project/app once (idempotent — see the
	@# script's own doc comment) and writes
	@# dev/zitadel/secrets/zitadel.env, which the -include near the top of
	@# this file picks up for dev-api/seed below.
	node scripts/zitadel-bootstrap.mjs

# The e2e suite is, in load terms, an attack: pagination.spec.ts creates 55
# visits in a tight loop as one principal, and the whole suite runs at four
# workers. Production's 600/min per tenant and 120/min per principal would
# throttle our own tests, so the dev stack raises those two budgets far out
# of the suite's reach (spec D7). The limiter itself stays ENABLED — a
# limiter never exercised where developers work would first be exercised in
# production.
#
# There is no third, tighter budget for POST /v1/iam/me/tenant anymore
# (RATE_LIMIT_MINT_PER_MIN, removed #838 Task 5): that route used to mint a
# GIP custom token against Identity Platform's project-wide quota, and now
# re-mints the Helivanta session in-process — the same cost every other
# authenticated route already pays through authz.Middleware. See
# backend/internal/bootstrap/ratelimit.go's doc comment for the full
# reasoning; it is not silently carried forward here.
RATE_LIMIT_TENANT_PER_MIN ?= 100000
RATE_LIMIT_PRINCIPAL_PER_MIN ?= 100000

# HELIVANTA_ENV=dev is required here: the API refuses to start with
# SESSION_SIGNING_KEY set to the well-known HELIVANTA_DEV_SESSION_SIGNING_KEY
# outside HELIVANTA_ENV=dev (see backend/internal/config/signingkey.go), and
# refuses to boot at all with no SESSION_SIGNING_KEY (#838 Task 2).
# HELIVANTA_DEV_SESSION_SIGNING_KEY below is the SAME well-known value as
# config.DevSessionSigningKey — committed to source, shared by every
# developer and CI runner, and usable ONLY because HELIVANTA_ENV=dev is required
# alongside it here. It is a make variable of its own (not inlined into
# SESSION_SIGNING_KEY directly) so a developer who has provisioned a real
# key via .env can override it the same way every other HELIVANTA_* variable
# here works.
HELIVANTA_DEV_SESSION_SIGNING_KEY ?= X5yoi73f6FRR8XH2ZfRBjanOZLm/bkae0QV7wGJRuf8=

# ZITADEL_LOGIN_CLIENT_PAT_FILE is written by Zitadel's first-instance
# provisioning (docker-compose.dev.yml's zitadel service,
# FirstInstance.Org.LoginClient.PatPath) — NOT by scripts/zitadel-bootstrap.mjs
# the way zitadel.env is, so it exists as soon as the zitadel container has
# finished its one-time FirstInstance run, independent of whether
# dev-infra's bootstrap script has run yet. It carries the SAME ordering
# trap docker-compose.dev.yml's zitadel-pat-ready service documents for
# zitadel-login: on a fresh clone the file does not exist the instant the
# zitadel container starts, only once provisioning completes. `make up`'s
# dependency chain (dev-infra, which polls Zitadel's own /debug/healthz
# before returning) means the file is already present by the time dev-api
# runs it through `make -j2 dev-api dev-web` — but a developer invoking
# `make dev-api` directly, before `make dev-infra` has ever completed, hits
# the same missing-file case `dev-api`'s existing ZITADEL_CLIENT_ID check
# just above guards against, so this fails the same fail-closed way rather
# than booting the API with an empty PAT (which config.go's own
# RequireZitadelLoginClientToken would then refuse anyway, just later and
# with a less specific message).
ZITADEL_LOGIN_CLIENT_PAT_FILE ?= dev/zitadel/secrets/login-client.pat
dev-api:
	@if [ -z "$${ZITADEL_CLIENT_ID:-$(ZITADEL_CLIENT_ID)}" ]; then \
		echo "ZITADEL_CLIENT_ID is not set — run 'make dev-infra' first so" >&2; \
		echo "scripts/zitadel-bootstrap.mjs can provision the Helivanta app and" >&2; \
		echo "write dev/zitadel/secrets/zitadel.env." >&2; \
		exit 1; \
	fi
	@if [ -z "$${ZITADEL_LOGIN_CLIENT_TOKEN:-}" ] && [ ! -s "$(ZITADEL_LOGIN_CLIENT_PAT_FILE)" ]; then \
		echo "$(ZITADEL_LOGIN_CLIENT_PAT_FILE) does not exist or is empty — run" >&2; \
		echo "'make dev-infra' first so Zitadel's first-instance provisioning" >&2; \
		echo "can write it (see docker-compose.dev.yml's zitadel-pat-ready" >&2; \
		echo "service comment for why this can lag the container starting)." >&2; \
		exit 1; \
	fi
	cd backend && HELIVANTA_ENV=$${HELIVANTA_ENV:-dev} ZITADEL_ISSUER_URL=$${ZITADEL_ISSUER_URL:-$(ZITADEL_ISSUER_URL)} ZITADEL_CLIENT_ID=$${ZITADEL_CLIENT_ID:-$(ZITADEL_CLIENT_ID)} ZITADEL_LOGIN_CLIENT_TOKEN=$${ZITADEL_LOGIN_CLIENT_TOKEN:-$$(cat ../$(ZITADEL_LOGIN_CLIENT_PAT_FILE))} SESSION_SIGNING_KEY=$${SESSION_SIGNING_KEY:-$(HELIVANTA_DEV_SESSION_SIGNING_KEY)} PORT=$${PORT:-$(HELIVANTA_API_PORT)} RATE_LIMIT_TENANT_PER_MIN=$(RATE_LIMIT_TENANT_PER_MIN) RATE_LIMIT_PRINCIPAL_PER_MIN=$(RATE_LIMIT_PRINCIPAL_PER_MIN) go run ./cmd/api

dev-web:
	@# Source dev/zitadel/secrets/zitadel.env at RECIPE RUN TIME, not via
	@# make's -include. On a fresh clone the -include is parsed before
	@# dev-infra's recipe has created the file, so NEXT_PUBLIC_ZITADEL_CLIENT_ID
	@# is empty for the whole make process and every Next app boots with an
	@# invalid environment — /login then 500s with "String must contain at
	@# least 1 character(s)" while the API, which reads the file at runtime,
	@# works fine. Same staleness window scripts/lib/zitadel.mjs's
	@# readClientID() documents for the seed path; the web path needs its own
	@# fix because these values are inlined into the client bundle at boot.
	set -a; [ -f dev/zitadel/secrets/zitadel.env ] && . ./dev/zitadel/secrets/zitadel.env; set +a; \
	NEXT_PUBLIC_ZITADEL_CLIENT_ID=$${NEXT_PUBLIC_ZITADEL_CLIENT_ID:-$$ZITADEL_CLIENT_ID} \
	NEXT_PUBLIC_ZITADEL_ISSUER_URL=$${NEXT_PUBLIC_ZITADEL_ISSUER_URL:-$(ZITADEL_ISSUER_URL)} \
	pnpm turbo dev

# --- Idle-timeout e2e fixture (#848 Task 8) -------------------------------
# e2e/tests/idle-timeout.spec.ts has to observe a session actually going
# idle without sitting for the real 15-minute IDLE_TIMEOUT default, and no
# other spec — or production — may have that default weakened out from
# under it. So the spec gets its own API + shell pair, on their own ports,
# running ALONGSIDE (not instead of) the ones dev-api/dev-web start; the
# e2e "idle-timeout" Playwright project (e2e/playwright.config.ts) points
# its baseURL at HELIVANTA_IDLE_WEB_PORT.
#
# IDLE_TIMEOUT_TEST_VALUE is baked into dev-api-idle-timeout's recipe
# rather than read from IDLE_TIMEOUT: reading that variable the normal way
# would let a value a developer already exported for some other reason
# leak into this recipe too, and — the direction that actually matters —
# it keeps this recipe from ever being able to affect dev-api's own
# IDLE_TIMEOUT, since the two recipes read two different variables.
HELIVANTA_IDLE_API_PORT ?= 8099
HELIVANTA_IDLE_WEB_PORT ?= 4399
IDLE_TIMEOUT_TEST_VALUE ?= 20s
ZITADEL_IDLE_TIMEOUT_ENV_FILE ?= dev/zitadel/secrets/zitadel-idle-timeout.env

# helivanta-web-idle-timeout is a SEPARATE Zitadel OIDC app from helivanta-web
# (scripts/zitadel-bootstrap.mjs), not a reused client_id — Zitadel's
# per-app Login V2 `baseUri` is a single origin, so pointing helivanta-web's own
# baseUri at a second port would make every OTHER login (every other e2e
# spec, every human developer) render its interactive step on whichever
# port last won. dev-infra runs the bootstrap script, which writes this
# app's client_id to ZITADEL_IDLE_TIMEOUT_ENV_FILE the same way it writes
# zitadel.env for helivanta-web.
dev-api-idle-timeout:
	@if [ ! -s "$(ZITADEL_IDLE_TIMEOUT_ENV_FILE)" ]; then \
		echo "$(ZITADEL_IDLE_TIMEOUT_ENV_FILE) does not exist or is empty — run" >&2; \
		echo "'make dev-infra' first so scripts/zitadel-bootstrap.mjs can" >&2; \
		echo "provision the helivanta-web-idle-timeout app and write it." >&2; \
		exit 1; \
	fi
	@if [ -z "$${ZITADEL_LOGIN_CLIENT_TOKEN:-}" ] && [ ! -s "$(ZITADEL_LOGIN_CLIENT_PAT_FILE)" ]; then \
		echo "$(ZITADEL_LOGIN_CLIENT_PAT_FILE) does not exist or is empty — run" >&2; \
		echo "'make dev-infra' first so Zitadel's first-instance provisioning" >&2; \
		echo "can write it (see docker-compose.dev.yml's zitadel-pat-ready" >&2; \
		echo "service comment for why this can lag the container starting)." >&2; \
		exit 1; \
	fi
	set -a; . ./$(ZITADEL_IDLE_TIMEOUT_ENV_FILE); set +a; \
	cd backend && HELIVANTA_ENV=$${HELIVANTA_ENV:-dev} ZITADEL_ISSUER_URL=$${ZITADEL_ISSUER_URL:-$(ZITADEL_ISSUER_URL)} ZITADEL_CLIENT_ID=$$ZITADEL_CLIENT_ID ZITADEL_LOGIN_CLIENT_TOKEN=$${ZITADEL_LOGIN_CLIENT_TOKEN:-$$(cat ../$(ZITADEL_LOGIN_CLIENT_PAT_FILE))} SESSION_SIGNING_KEY=$${SESSION_SIGNING_KEY:-$(HELIVANTA_DEV_SESSION_SIGNING_KEY)} PORT=$(HELIVANTA_IDLE_API_PORT) IDLE_TIMEOUT=$(IDLE_TIMEOUT_TEST_VALUE) RATE_LIMIT_TENANT_PER_MIN=$(RATE_LIMIT_TENANT_PER_MIN) RATE_LIMIT_PRINCIPAL_PER_MIN=$(RATE_LIMIT_PRINCIPAL_PER_MIN) go run ./cmd/api

# Only apps/shell, not `pnpm turbo dev` (which would start every zone app
# again on the default 4301-4304 ports and collide with dev-web's own
# instances). The idle-timeout spec never navigates into a zone app, so
# shell alone is enough.
dev-web-idle-timeout:
	@if [ ! -s "$(ZITADEL_IDLE_TIMEOUT_ENV_FILE)" ]; then \
		echo "$(ZITADEL_IDLE_TIMEOUT_ENV_FILE) does not exist or is empty — run" >&2; \
		echo "'make dev-infra' first so scripts/zitadel-bootstrap.mjs can" >&2; \
		echo "provision the helivanta-web-idle-timeout app and write it." >&2; \
		exit 1; \
	fi
	set -a; . ./$(ZITADEL_IDLE_TIMEOUT_ENV_FILE); set +a; \
	cd apps/shell && API_URL=http://localhost:$(HELIVANTA_IDLE_API_PORT) NEXT_PUBLIC_ZITADEL_CLIENT_ID=$$ZITADEL_CLIENT_ID NEXT_PUBLIC_ZITADEL_ISSUER_URL=$${NEXT_PUBLIC_ZITADEL_ISSUER_URL:-$(ZITADEL_ISSUER_URL)} npx next dev -p $(HELIVANTA_IDLE_WEB_PORT)

# `make up` is the one command: infra, migrations, seed, then API + web in
# the foreground. seed is idempotent, so re-running up is safe.
up: dev-infra seed
	@echo "Infra seeded. Starting API + web — Ctrl-C stops them, then run 'make down'."
	@$(MAKE) -j2 dev-api dev-web

# `make down` stops infra AND the app processes. `docker compose down`
# alone leaves the API and the four next dev servers holding ports
# 4301-4304 and 8080 against infra that no longer exists.
down:
	@./scripts/dev-down.sh

# Destroys the local volumes and re-seeds. Prompts first; RESET_YES=1 skips.
reset:
	@./scripts/reset-dev.sh

# Back-compat aliases for the older target names.
dev: up
dev-down: down

migrate:
	cd backend && go run ./cmd/migrate

# Mints a SESSION_SIGNING_KEY in the exact format the API accepts (#45,
# spec D6). Prints ONLY the key, so it can be piped into a secret store.
# See docs/runbooks/secrets.md for where it goes.
secret-session-key:
	@cd backend && go run ./cmd/session-key

# seed depends on migrate so `make dev-infra && make seed` works on a
# fresh clone with no API running — seed writes into iam_members, which
# only exists after migrations have run.
seed: migrate
	node scripts/seed-dev.mjs

verify-local:
	./scripts/verify-local.sh

test: test-go test-web

test-go:
	cd backend && go test -race ./...

lint-go:
	cd backend && golangci-lint run ./...

coverage-go:
	cd backend && ./scripts/coverage-gate.sh

test-web:
	pnpm turbo type-check test build

test-scripts:
	bash scripts/preflight.test.sh

# `make e2e` runs the WHOLE suite in two phases, because idle-timeout.spec.ts's
# fixture and the main stack's own shell cannot coexist (see the "Idle
# timeout e2e fixture" comment above) — Next.js 16 refuses a second `next
# dev` for the same project directory. A bare `playwright test` only ever
# runs phase one: the "idle-timeout" project is deliberately left out of
# Playwright's default project list (e2e/playwright.config.ts) so that
# command stays honest about what it covers, instead of quietly failing
# whenever the fixture happens to also be up.
#
# scripts/e2e.sh does the actual swap (stop shell -> start fixture -> run
# the idle-timeout project -> stop fixture -> restart shell) behind an
# EXIT trap, so a failure partway through still restores the shell and
# frees the fixture's ports rather than leaving the developer stuck. It
# assumes `make dev` (or `make up`) and `make seed` have already brought
# up infra + API + shell + medicore — the same assumption "specs"/"bulk"
# already make.
e2e:
	HELIVANTA_API_PORT=$(HELIVANTA_API_PORT) HELIVANTA_IDLE_API_PORT=$(HELIVANTA_IDLE_API_PORT) HELIVANTA_IDLE_WEB_PORT=$(HELIVANTA_IDLE_WEB_PORT) bash scripts/e2e.sh

new-module:
	cd backend && ./scripts/new-module.sh $(NAME)
