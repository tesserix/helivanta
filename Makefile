.PHONY: up down dev dev-infra dev-down dev-api dev-web dev-api-renewal dev-web-renewal migrate seed secret-session-key test test-go coverage-go test-web test-scripts e2e lint-go new-module verify-local preflight reset image-api image-shell

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

# Hostnames, not ports — and unlike the ports above these are NOT a
# convenience knob (#916 Task 4, design spec D6). The app and the IdP must
# sit on DIFFERENT REGISTRABLE DOMAINS in dev and CI, because that is what
# production does (helivanta.app vs auth.tesserix.app) and therefore what
# makes every browser request from the app to the IdP cross-site. A "site"
# is scheme + registrable domain and PORTS ARE NOT PART OF IT, so the old
# localhost:4301 / localhost:20080 pair was same-site: Zitadel's
# SameSite=Lax cookie flowed in dev, the iframe renewal #916 removed
# "worked" here, and no test could have caught the production defect.
#
# `.localhost` keeps this free of /etc/hosts edits: Chrome (and macOS's
# resolver) map *.localhost to loopback and still treat it as a secure
# context, while helivanta.localhost and tesserix.localhost are distinct
# registrable domains. Proven, not assumed — see
# e2e/tests/cross-site-harness.spec.ts.
#
# Overridable for the same reason the ports are (a machine where these
# names already mean something else): every consumer reads these variables
# rather than a literal — the compose file, the bootstrap script, the
# backend's dev defaults, scripts/e2e.sh, and the e2e suite itself through
# e2e/tests/support/hosts.ts. `make e2e` forwards them explicitly.
#
# What is NOT overridable is the PROPERTY: point both at the same
# registrable domain and the harness goes back to same-site, where #916's
# whole class of defect is invisible. That is enforced, not merely asked
# for — cross-site-harness.spec.ts derives the hosts it probes from the
# running configuration and fails if they share a site.
#
# Changing HELIVANTA_ZITADEL_HOST on an EXISTING stack additionally needs
# `make reset`: Zitadel writes its instance domain once, at first-instance
# provisioning, and answers "Instance not found" to any other host
# afterwards. scripts/preflight.sh's check_zitadel_instance_domain catches
# that before `make up` gets far enough to fail obscurely.
HELIVANTA_WEB_HOST ?= helivanta.localhost
HELIVANTA_ZITADEL_HOST ?= auth.tesserix.localhost

# Connection strings derived from the ports above, for dev-api/migrate/seed.
# backend/internal/config/config.go and scripts/seed-dev.mjs already default
# to these exact values (on the stock ports) when the env var is unset, so
# ?= only ever takes over here — a developer who has explicitly exported one
# of these keeps their own value.
APP_DATABASE_URL ?= postgres://hms_app:hms_app@localhost:$(HELIVANTA_PG_PORT)/helivanta?sslmode=disable
ADMIN_DATABASE_URL ?= postgres://helivanta:helivanta@localhost:$(HELIVANTA_PG_PORT)/helivanta?sslmode=disable
# The BYPASSRLS role from dev/init-db.sql. cmd/api refuses to boot without
# it, and refuses to boot if it names a role that cannot actually bypass
# RLS — there is no fallback to ADMIN_DATABASE_URL by design (#894).
SYSTEM_DATABASE_URL ?= postgres://helivanta_system:helivanta_system@localhost:$(HELIVANTA_PG_PORT)/helivanta?sslmode=disable
NATS_URL ?= nats://localhost:$(HELIVANTA_NATS_PORT)
OPENFGA_URL ?= http://localhost:$(HELIVANTA_OPENFGA_PORT)
ZITADEL_ISSUER_URL ?= http://$(HELIVANTA_ZITADEL_HOST):$(HELIVANTA_ZITADEL_PORT)

# ZITADEL_HOSTED_LOGIN_URL and HELIVANTA_WEB_ORIGIN used to be derived and
# exported here. Both existed only for the hosted-login handoff, which #947
# deleted — Helivanta never sends a user to Zitadel's login UI — so the API no
# longer reads either variable, and neither is set here any more.

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
# Exported so docker-compose.dev.yml's ${HELIVANTA_ZITADEL_HOST} substitution
# resolves to the same value every recipe here uses.
export HELIVANTA_WEB_HOST HELIVANTA_ZITADEL_HOST
export APP_DATABASE_URL ADMIN_DATABASE_URL SYSTEM_DATABASE_URL NATS_URL OPENFGA_URL ZITADEL_ISSUER_URL
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

# docker-compose.dev.yml bind-mounts dev/zitadel/secrets into three
# containers, and Zitadel WRITES two PAT files there during first-instance
# provisioning. The directory is gitignored, so a fresh clone does not have
# it — and when a bind-mount source is missing, the Docker DAEMON creates it,
# owned by root with mode 0755. The zitadel image runs as its own non-root
# `zitadel` user, which then cannot write, and the failure is deeply
# unhelpful: provisioning has already pushed the instance-domain event by the
# time it tries the file, so setup dies with
# `open /secrets/helivanta-seed.pat: permission denied`, restarts, and every
# restart afterwards reports the CONSEQUENCE instead —
# `Errors.Instance.Domain.AlreadyExists`, forever, on a container that never
# serves a request. Diagnosed from exactly that log on a Linux CI runner
# (#920); the only reason no developer had hit it is that Docker Desktop on
# macOS masks ownership on bind mounts entirely.
#
# So: create it HERE, before compose can, and make it writable by whatever
# uid the images run as. 0777 on a directory whose entire contents are
# well-known DEV credentials (see .gitignore's note, and the masterkey
# comment above) — the alternative, guessing each image's uid and chowning,
# needs root on the host and breaks the next time an image changes its user.
# This is the dev stack only; nothing here is ever deployed.
dev-infra: preflight
	mkdir -p dev/zitadel/secrets
	chmod 0777 dev/zitadel/secrets
	docker compose -f docker-compose.dev.yml up -d --wait postgres nats redis openfga
	docker compose -f docker-compose.dev.yml up -d zitadel-db zitadel zitadel-login zitadel-proxy
	@printf 'Waiting for Zitadel on http://$(HELIVANTA_ZITADEL_HOST):$(HELIVANTA_ZITADEL_PORT)…'
	@until curl -fsS --max-time 2 http://$(HELIVANTA_ZITADEL_HOST):$(HELIVANTA_ZITADEL_PORT)/debug/healthz >/dev/null 2>&1; do printf '.'; sleep 1; done
	@echo ' ready.'
	@# /debug/healthz above is instance-INDEPENDENT: it answers 200 on a
	@# volume provisioned for a DIFFERENT host, and the bootstrap below then
	@# dies with a bare 'HTTP 404 {"code":5,"message":"Instance not found"}'.
	@# `preflight` at the top of this recipe cannot catch that on the common
	@# path — it runs before compose starts Zitadel, so it sees nothing
	@# answering and passes. Here, and only here, the information exists.
	@bash scripts/preflight.sh --only zitadel-instance-domain
	@# zitadel-login reads its PAT (dev/zitadel/secrets/login-client.pat)
	@# exactly ONCE, at container boot, and caches it for the life of the
	@# process — verified by experiment, not inferred (#923): a wedged
	@# container was found live, with the PAT ON DISK returning HTTP 200
	@# against Zitadel core while the CONTAINER mounting that same file
	@# reported `Errors.Token.Invalid (AUTH-7fs1e)` HTTP 401 over 2329
	@# consecutive health-check failures (~19.4 hours — its whole life),
	@# with AUTH-7fs1e present in every one of the (last five, all Docker
	@# retains) logged probe outputs, and a bare `docker restart` (no volume
	@# change) took it straight to healthy. `zitadel-pat-ready` above
	@# already gates container CREATION on the file being non-empty, so a
	@# race with provisioning cannot land here — but the file can still be
	@# written moments after zitadel-login started reading it, which is the
	@# case #923 reproduced on a fresh volume (core and login started 76ms
	@# apart). Poll generously: the same 180s budget zitadel-pat-ready
	@# gives the file to appear, plus room for the image's own healthcheck
	@# cadence (30s interval, 3 retries) to converge once it does.
	@#
	@# The template asks explicitly whether `.State.Health` exists rather
	@# than reading `.State.Health.Status` and swallowing the error — Docker
	@# does NOT report an empty status for a running container with no
	@# healthcheck, it errors ('map has no entry for key "Health"'), and a
	@# bare `2>/dev/null` would make a container that is up but blind
	@# indistinguishable from one that was never created (review round #2 of
	@# #923, proven live against a real running container with no
	@# HEALTHCHECK). scripts/preflight.sh's check_zitadel_login_health uses
	@# the identical format string for the same reason — see its comment for
	@# the full argument.
	@printf 'Waiting for zitadel-login to report healthy…'
	@login_status=""; i=0; \
	while [ "$$i" -le 210 ]; do \
		login_status=$$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}no-healthcheck{{end}}' helivanta-dev-zitadel-login-1 2>/dev/null || echo ""); \
		if [ "$$login_status" = "healthy" ] || [ "$$login_status" = "no-healthcheck" ]; then break; fi; \
		i=$$((i + 1)); printf '.'; sleep 2; \
	done; \
	if [ "$$login_status" = "no-healthcheck" ]; then \
		echo; \
		echo "zitadel-login is running but reports no Docker HEALTHCHECK at all." >&2; \
		echo "Cause: this gate depends entirely on the HEALTHCHECK baked into" >&2; \
		echo "ghcr.io/zitadel/zitadel-login:v4.15.3 — if that image pin moved, or" >&2; \
		echo "upstream dropped/restructured the probe, this gate is now blind to a" >&2; \
		echo "wedged login the same way #923 found the rest of the stack to be." >&2; \
		echo "Remedy: 'docker inspect --format {{.Config.Healthcheck}}" >&2; \
		echo "helivanta-dev-zitadel-login-1' to see what changed, then update this" >&2; \
		echo "gate (and docker-compose.dev.yml) to match." >&2; \
		exit 1; \
	elif [ -z "$$login_status" ]; then \
		echo; \
		echo "zitadel-login was never created (no health status after the wait)." >&2; \
		echo "Cause: unknown from here — this is NOT the cached-PAT wedge (that" >&2; \
		echo "requires a running container). Check 'docker compose -f" >&2; \
		echo "docker-compose.dev.yml ps zitadel-login' and its logs; also confirm" >&2; \
		echo "no COMPOSE_PROJECT_NAME override renamed the container away from" >&2; \
		echo "helivanta-dev-zitadel-login-1." >&2; \
		exit 1; \
	elif [ "$$login_status" != "healthy" ]; then \
		echo; \
		echo "zitadel-login never reported healthy (last status: '$$login_status')." >&2; \
		echo "Cause: a cached login-client PAT that is invalid — either a stale token" >&2; \
		echo "latched at boot (#923) or a genuine credential problem." >&2; \
		echo "Remedy: 'docker restart helivanta-dev-zitadel-login-1', then re-run" >&2; \
		echo "'make dev-infra'. If it wedges again immediately, 'docker logs" >&2; \
		echo "helivanta-dev-zitadel-login-1' has the readiness error." >&2; \
		exit 1; \
	fi
	@echo ' healthy.'
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

# --- Session-renewal e2e fixture (#916 Task 4) ----------------------------
# e2e/tests/session-renewal.spec.ts has to observe a session SURVIVING past
# its own SESSION_TTL, which is only possible if a renewal actually
# happened. That needs a SESSION_TTL short enough to expire inside a test
# run.
#
# This was six minutes until #916 Task 4's F3 fix, because apps/shell
# scheduled its FIRST renewal from a hardcoded five-minute client constant
# rather than from the server: POST /v1/auth/login returned no `renew_at`,
# so anything under five minutes killed the session before the first
# renewal fired. Login now returns `renew_at` from the same helper the
# renewal endpoint uses, so the cadence is server-driven from the first
# tick and the TTL can be far shorter.
#
# THREE MINUTES, not the 90s this was briefly set to. The margin that
# matters is X, the gap between the login mint and SessionRenewal
# mounting in the browser (a redirect, a React mount, and on a cold CI
# runner a first-request `next dev` compile). The first renewal lands at
# roughly max(mint+TTL/3, mount+30s), so the spec only holds while
# X < TTL - TTL/3. At 90s that left a 60-SECOND budget for X, and blowing
# it fails as "no helivanta_session cookie" — indistinguishable from a
# genuine renewal defect, which is the worst possible way for a harness to
# flake. At 3m the budget is 120s, and renewAtFor answers TTL/3 = 60s, so
# renewals land at t+60/120/180s while the ORIGINAL cookie dies at t+180s.
# A session still working at t+205s can only be a re-minted one.
# e2e/tests/session-renewal.spec.ts names that budget (X_BUDGET_SECONDS)
# and ASSERTS on it right after sign-in, so an over-budget machine fails
# saying so instead of failing 200 seconds later as "no session cookie".
#
# SESSION_TTL_TEST_VALUE is baked into this recipe rather than read from
# SESSION_TTL, for the same reason IDLE_TIMEOUT_TEST_VALUE is above: a
# value a developer exported for some other reason must not leak in, and
# this recipe must never be able to affect dev-api's own SESSION_TTL.
#
# Note this fixture leaves IDLE_TIMEOUT at its 15-minute default on
# purpose. Renewal deliberately does NOT move idle_deadline (design spec
# D4), so a short IDLE_TIMEOUT would end the session on the idle clock
# before the session-TTL clock could prove anything — which is also why
# this cannot share the idle-timeout fixture above.
HELIVANTA_RENEWAL_API_PORT ?= 8098
HELIVANTA_RENEWAL_WEB_PORT ?= 4398
SESSION_TTL_TEST_VALUE ?= 3m
ZITADEL_RENEWAL_ENV_FILE ?= dev/zitadel/secrets/zitadel-renewal.env

dev-api-renewal:
	@if [ ! -s "$(ZITADEL_RENEWAL_ENV_FILE)" ]; then \
		echo "$(ZITADEL_RENEWAL_ENV_FILE) does not exist or is empty — run" >&2; \
		echo "'make dev-infra' first so scripts/zitadel-bootstrap.mjs can" >&2; \
		echo "provision the helivanta-web-renewal app and write it." >&2; \
		exit 1; \
	fi
	@if [ -z "$${ZITADEL_LOGIN_CLIENT_TOKEN:-}" ] && [ ! -s "$(ZITADEL_LOGIN_CLIENT_PAT_FILE)" ]; then \
		echo "$(ZITADEL_LOGIN_CLIENT_PAT_FILE) does not exist or is empty — run" >&2; \
		echo "'make dev-infra' first so Zitadel's first-instance provisioning" >&2; \
		echo "can write it (see docker-compose.dev.yml's zitadel-pat-ready" >&2; \
		echo "service comment for why this can lag the container starting)." >&2; \
		exit 1; \
	fi
	set -a; . ./$(ZITADEL_RENEWAL_ENV_FILE); set +a; \
	cd backend && HELIVANTA_ENV=$${HELIVANTA_ENV:-dev} ZITADEL_ISSUER_URL=$${ZITADEL_ISSUER_URL:-$(ZITADEL_ISSUER_URL)} ZITADEL_CLIENT_ID=$$ZITADEL_CLIENT_ID ZITADEL_LOGIN_CLIENT_TOKEN=$${ZITADEL_LOGIN_CLIENT_TOKEN:-$$(cat ../$(ZITADEL_LOGIN_CLIENT_PAT_FILE))} SESSION_SIGNING_KEY=$${SESSION_SIGNING_KEY:-$(HELIVANTA_DEV_SESSION_SIGNING_KEY)} PORT=$(HELIVANTA_RENEWAL_API_PORT) SESSION_TTL=$(SESSION_TTL_TEST_VALUE) RATE_LIMIT_TENANT_PER_MIN=$(RATE_LIMIT_TENANT_PER_MIN) RATE_LIMIT_PRINCIPAL_PER_MIN=$(RATE_LIMIT_PRINCIPAL_PER_MIN) go run ./cmd/api

# Only apps/shell, for the same reason dev-web-idle-timeout is: the spec
# never navigates into a zone app, and `pnpm turbo dev` would re-bind
# 4301-4304.
dev-web-renewal:
	@if [ ! -s "$(ZITADEL_RENEWAL_ENV_FILE)" ]; then \
		echo "$(ZITADEL_RENEWAL_ENV_FILE) does not exist or is empty — run" >&2; \
		echo "'make dev-infra' first so scripts/zitadel-bootstrap.mjs can" >&2; \
		echo "provision the helivanta-web-renewal app and write it." >&2; \
		exit 1; \
	fi
	set -a; . ./$(ZITADEL_RENEWAL_ENV_FILE); set +a; \
	cd apps/shell && API_URL=http://localhost:$(HELIVANTA_RENEWAL_API_PORT) NEXT_PUBLIC_ZITADEL_CLIENT_ID=$$ZITADEL_CLIENT_ID NEXT_PUBLIC_ZITADEL_ISSUER_URL=$${NEXT_PUBLIC_ZITADEL_ISSUER_URL:-$(ZITADEL_ISSUER_URL)} npx next dev -p $(HELIVANTA_RENEWAL_WEB_PORT)

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

# `make e2e` runs the WHOLE suite in THREE phases, because both fixture
# shells (idle-timeout.spec.ts's and session-renewal.spec.ts's) and the main
# stack's own shell cannot coexist — Next.js 16 refuses a second `next dev`
# for the same project directory. See the "Idle timeout e2e fixture" and
# "Session-renewal e2e fixture" comments above. A bare `playwright test` only
# ever runs phase one: both fixture projects are deliberately left out of
# Playwright's default project list (e2e/playwright.config.ts) so that
# command stays honest about what it covers, instead of quietly failing
# whenever a fixture happens to also be up.
#
# scripts/e2e.sh does the actual swaps (stop the zone apps -> start a fixture
# -> run its project -> stop the fixture, once per fixture -> restart the
# zone apps) behind an EXIT trap, so a failure partway through still restores
# the shell and frees the fixtures' ports rather than leaving the developer
# stuck. It also runs preflight's stale-Zitadel-instance check first. It
# assumes `make dev` (or `make up`) and `make seed` have already brought
# up infra + API + shell + medicore — the same assumption "specs"/"bulk"
# already make.
e2e:
	HELIVANTA_API_PORT=$(HELIVANTA_API_PORT) HELIVANTA_IDLE_API_PORT=$(HELIVANTA_IDLE_API_PORT) HELIVANTA_IDLE_WEB_PORT=$(HELIVANTA_IDLE_WEB_PORT) HELIVANTA_RENEWAL_API_PORT=$(HELIVANTA_RENEWAL_API_PORT) HELIVANTA_RENEWAL_WEB_PORT=$(HELIVANTA_RENEWAL_WEB_PORT) HELIVANTA_WEB_HOST=$(HELIVANTA_WEB_HOST) HELIVANTA_ZITADEL_HOST=$(HELIVANTA_ZITADEL_HOST) HELIVANTA_ZITADEL_PORT=$(HELIVANTA_ZITADEL_PORT) bash scripts/e2e.sh

new-module:
	cd backend && ./scripts/new-module.sh $(NAME)

# Builds the backend API image (Dockerfile.api, #824 slice 1b Task 2) from
# the REPO ROOT as build context — see Dockerfile.api's own top comment for
# why, given the Go module actually lives under backend/. VERSION/COMMIT
# default to `git describe`/the short SHA so a plain `make image-api` still
# stamps something traceable to a commit; either can be overridden
# (`make image-api VERSION=v1.2.3`) for a real release build.
IMAGE_API_TAG ?= helivanta-api:local
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)

image-api:
	docker build -f Dockerfile.api \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		-t $(IMAGE_API_TAG) .

# Builds the shell frontend image (Dockerfile.shell, #824 slice 1b Task 3),
# also from the REPO ROOT as build context — apps/shell is a pnpm workspace
# member whose @helivanta/ui and @helivanta/api dependencies are consumed as
# TypeScript source, so the workspace IS the build context. See
# Dockerfile.shell's own header.
#
# Every value below is baked into the image and cannot be changed by the
# pod's environment afterwards. The two NEXT_PUBLIC_ ones are inlined into
# the client bundle; the URL ones are frozen into next.config.ts's rewrite
# table. So these defaults are PRODUCTION values, unlike the localhost
# defaults the rest of this file carries for `make dev-web` — an image is
# not a dev server, and a local-looking default here ships to the cluster.
#
# Note these deliberately do NOT reuse the NEXT_PUBLIC_ZITADEL_* variables
# defined near the top of this file. Those resolve to the LOCAL Zitadel
# (dev/zitadel/secrets/zitadel.env, whatever client id a developer's own
# stack was provisioned with) and are exported into every recipe. Inheriting
# them here would mean `make image-shell` on a developer machine with a dev
# stack up silently produces an image pointing at http://localhost:20080.
IMAGE_SHELL_TAG ?= helivanta-shell:local
IMAGE_SHELL_ZITADEL_ISSUER_URL ?= https://auth.tesserix.app
IMAGE_SHELL_ZITADEL_CLIENT_ID ?= 386782591925092979
IMAGE_SHELL_API_URL ?= http://helivanta-api.helivanta.svc.cluster.local:8080

# The zone apps are NOT deployed — they land in slice 2. These are the
# in-cluster Service DNS names they will have, following the convention
# helivanta-api and helivanta-postgres already use, on each zone's own
# listen port (4302/4303/4304, see each app's package.json). Until slice 2
# lands, /medicore, /pharmacy and /lab return 502 from an unresolvable host.
# That is chosen over localhost, which would make the shell proxy those
# paths back to itself and look like it worked.
IMAGE_SHELL_MEDICORE_URL ?= http://helivanta-medicore.helivanta.svc.cluster.local:4302
IMAGE_SHELL_PHARMACY_URL ?= http://helivanta-pharmacy.helivanta.svc.cluster.local:4303
IMAGE_SHELL_LAB_URL ?= http://helivanta-lab.helivanta.svc.cluster.local:4304

image-shell:
	docker build -f Dockerfile.shell \
		--build-arg NEXT_PUBLIC_ZITADEL_ISSUER_URL=$(IMAGE_SHELL_ZITADEL_ISSUER_URL) \
		--build-arg NEXT_PUBLIC_ZITADEL_CLIENT_ID=$(IMAGE_SHELL_ZITADEL_CLIENT_ID) \
		--build-arg API_URL=$(IMAGE_SHELL_API_URL) \
		--build-arg MEDICORE_URL=$(IMAGE_SHELL_MEDICORE_URL) \
		--build-arg PHARMACY_URL=$(IMAGE_SHELL_PHARMACY_URL) \
		--build-arg LAB_URL=$(IMAGE_SHELL_LAB_URL) \
		-t $(IMAGE_SHELL_TAG) .
