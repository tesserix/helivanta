.PHONY: dev dev-infra dev-down dev-api dev-web seed test test-go coverage-go test-web e2e lint-go

dev-infra:
	docker compose -f docker-compose.dev.yml up -d --wait postgres nats redis openfga
	docker compose -f docker-compose.dev.yml up -d firebase-auth

dev-down:
	docker compose -f docker-compose.dev.yml down

dev-api:
	cd backend && FIREBASE_AUTH_EMULATOR_HOST=$${FIREBASE_AUTH_EMULATOR_HOST:-localhost:9099} go run ./cmd/api

dev-web:
	pnpm turbo dev

dev: dev-infra
	@echo "Infra up. Starting API + web (Ctrl-C stops both)…"
	@$(MAKE) -j2 dev-api dev-web

seed:
	node scripts/seed-dev.mjs

test: test-go test-web

test-go:
	cd backend && go test -race ./...

lint-go:
	cd backend && golangci-lint run ./...

coverage-go:
	cd backend && ./scripts/coverage-gate.sh

test-web:
	pnpm turbo type-check test build

e2e:
	pnpm --filter @hms/e2e run test:e2e
