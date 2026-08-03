.PHONY: dev dev-infra dev-down dev-api dev-web seed test test-go test-web

dev-infra:
	docker compose -f docker-compose.dev.yml up -d --wait postgres nats redis openfga
	docker compose -f docker-compose.dev.yml up -d firebase-auth

dev-down:
	docker compose -f docker-compose.dev.yml down

dev-api:
	cd backend && go run ./cmd/api

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

test-web:
	pnpm turbo lint type-check test build
