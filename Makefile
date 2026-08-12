.PHONY: up down dev dev-infra dev-down dev-api dev-web migrate seed test test-go coverage-go test-web test-scripts e2e lint-go new-module verify-local preflight reset

# Docker Compose reads .env in the project directory automatically for
# ${VAR} substitution in docker-compose.dev.yml; Make does not read it on
# its own. Pulling it in here too means one file drives both, instead of
# two independently maintained sets of port defaults that will eventually
# drift apart. A missing .env is fine — -include swallows the error and the
# ?= defaults below cover every variable.
-include .env

# Host-side ports for the local stack, overridable via .env (copy
# .env.example) so a developer whose ports are already taken by unrelated
# work can shift the whole stack instead of being blocked outright. Only
# the host side ever changes — container-internal ports are fixed, see
# docker-compose.dev.yml.
HMS_PG_PORT ?= 5432
HMS_NATS_PORT ?= 4222
HMS_NATS_MONITOR_PORT ?= 8222
HMS_REDIS_PORT ?= 6379
HMS_OPENFGA_PORT ?= 8090
HMS_GIP_PORT ?= 9099
HMS_API_PORT ?= 8080

# Connection strings derived from the ports above, for dev-api/migrate/seed.
# backend/internal/config/config.go and scripts/seed-dev.mjs already default
# to these exact values (on the stock ports) when the env var is unset, so
# ?= only ever takes over here — a developer who has explicitly exported one
# of these keeps their own value.
APP_DATABASE_URL ?= postgres://hms_app:hms_app@localhost:$(HMS_PG_PORT)/hms?sslmode=disable
ADMIN_DATABASE_URL ?= postgres://hms:hms@localhost:$(HMS_PG_PORT)/hms?sslmode=disable
NATS_URL ?= nats://localhost:$(HMS_NATS_PORT)
OPENFGA_URL ?= http://localhost:$(HMS_OPENFGA_PORT)
AUTH_EMULATOR_HOST ?= localhost:$(HMS_GIP_PORT)

# The frontend needs the same two ports, by different routes, and both are
# easy to forget: API_URL is what each app's next.config.ts rewrites /api to
# (server side), while NEXT_PUBLIC_AUTH_EMULATOR_HOST is read in the browser
# by apps/shell/lib/firebase.ts. Without these, shifting the ports moves the
# API and the emulator but leaves the UI still calling 8080 and 9099 — which
# on a machine whose ports were shifted precisely because something else owns
# them means the app talks to that something else. Silent and baffling.
API_URL ?= http://localhost:$(HMS_API_PORT)
NEXT_PUBLIC_AUTH_EMULATOR_HOST ?= localhost:$(HMS_GIP_PORT)

export HMS_PG_PORT HMS_NATS_PORT HMS_NATS_MONITOR_PORT HMS_REDIS_PORT HMS_OPENFGA_PORT HMS_GIP_PORT HMS_API_PORT
export APP_DATABASE_URL ADMIN_DATABASE_URL NATS_URL OPENFGA_URL AUTH_EMULATOR_HOST
export API_URL NEXT_PUBLIC_AUTH_EMULATOR_HOST

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
	docker compose -f docker-compose.dev.yml up -d firebase-auth
	@printf 'Waiting for the GIP emulator on :$(HMS_GIP_PORT)…'
	@until curl -fsS --max-time 2 http://localhost:$(HMS_GIP_PORT)/ >/dev/null 2>&1; do printf '.'; sleep 1; done
	@echo ' ready.'

# HMS_ENV=dev is required here: the API refuses to start with
# FIREBASE_AUTH_EMULATOR_HOST set outside dev, because the emulator makes
# ID token signature verification a no-op.
dev-api:
	cd backend && HMS_ENV=$${HMS_ENV:-dev} FIREBASE_AUTH_EMULATOR_HOST=$${FIREBASE_AUTH_EMULATOR_HOST:-localhost:$(HMS_GIP_PORT)} PORT=$${PORT:-$(HMS_API_PORT)} go run ./cmd/api

dev-web:
	pnpm turbo dev

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

e2e:
	pnpm --filter @hms/e2e run test:e2e

new-module:
	cd backend && ./scripts/new-module.sh $(NAME)
