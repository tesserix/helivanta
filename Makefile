.PHONY: up down dev dev-infra dev-down dev-api dev-web migrate seed test test-go coverage-go test-web test-scripts e2e lint-go new-module verify-local preflight

preflight:
	@bash scripts/preflight.sh

dev-infra: preflight
	docker compose -f docker-compose.dev.yml up -d --wait postgres nats redis openfga
	docker compose -f docker-compose.dev.yml up -d firebase-auth
	@printf 'Waiting for the GIP emulator on :9099…'
	@until curl -fsS --max-time 2 http://localhost:9099/ >/dev/null 2>&1; do printf '.'; sleep 1; done
	@echo ' ready.'

dev-api:
	cd backend && FIREBASE_AUTH_EMULATOR_HOST=$${FIREBASE_AUTH_EMULATOR_HOST:-localhost:9099} go run ./cmd/api

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
