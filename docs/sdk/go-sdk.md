# Backend Go SDK (`helivanta-go-sdk`)

Module: `github.com/tesserix/helivanta-go-sdk` (single module; see architecture §4.2). Evolves from `go-shared`.
Target: Go 1.26+, consumed by every backend service via pinned versions.

## Package structure

```
helivanta-go-sdk/
├── config/          # typed config + secrets           (#677)
├── logging/         # slog + PHI redaction             (#678)
├── telemetry/       # OpenTelemetry setup              (#679)
├── httpkit/         # Gin bootstrap, envelope, health  (#680)
├── authn/           # Keycloak OIDC middleware         (#681)
├── authz/           # OpenFGA client + helpers         (#682)
├── tenant/          # tenant context + guard           (#683)
├── database/        # pgx/GORM kit, RLS binding        (#684)
├── events/          # NATS JetStream, outbox, DLQ      (#685)
├── audit/           # tamper-evident audit emitter     (#686)
├── cache/           # Redis, tenant-namespaced         (#687)
├── storage/         # object storage, signed URLs, AV  (#688)
├── ratelimit/       # per-tenant limits & quotas       (#689)
├── i18n/            # server strings + country profile (#690)
├── flags/           # GrowthBook client                (#691)
├── notify/          # notifications abstraction        (#692)
├── payments/        # payments abstraction             (#693)
├── testkit/         # testcontainers, fixtures         (#694)
└── internal/        # shared helpers (not importable by consumers)
```

## Layering (allowed import edges)

```
Layer 0 (imports nothing internal): config, logging
Layer 1: telemetry, tenant, i18n            → may import L0
Layer 2: authn, authz, database, cache,
         ratelimit, storage, flags, audit   → may import L0–L1
Layer 3: httpkit, events, notify, payments  → may import L0–L2
testkit: may import anything (test-only)
```

Cycles are CI-blocked. Heavy dependencies live only in their leaf package (e.g. NATS client only in `events`).

---

## Package specifications

Each spec: purpose → key API sketch → failure behaviour → what its tests must prove. API sketches are _shape_, not final signatures — finalised in the package's issue.

### `config` (#677)

Typed config from env + files + GCP Secret Manager, validated at startup.

```go
type AppConfig struct {
    Port        int           `env:"PORT" default:"8080"`
    DatabaseURL secret.String `env:"DATABASE_URL" required:"true" secret:"gcp"`
}
cfg, err := config.Load[AppConfig](ctx)          // fails fast, names the missing key
```

- Precedence: explicit env > secret ref > file > default (documented, tested).
- `secret.String` redacts in `String()`/JSON/logs; raw value via `.Reveal()` only.
- Failure: missing/malformed required value → startup error naming the key. Never a nil later.
- Tests: precedence matrix; redaction; rotation TTL for cached secrets.

### `logging` (#678)

`slog` JSON logging with correlation fields and layered PHI redaction.

```go
log := logging.New(logging.Config{Service: "medicore", Level: "info"})
log = logging.WithContext(ctx, log)               // + request_id, tenant, principal, trace_id
log.Info("record accessed", logging.PHI("patient_name", name)) // value never emitted raw
```

- Redaction layers: (1) typed `PHI()/PII()` attrs — always masked; (2) pattern pass on all string values (Aadhaar 12-digit, ABHA 14-digit, Indian mobile, email) — masked + `redaction_total` metric; (3) key denylist (`name`, `phone`, `address`, …) — masked.
- Failure: redactor errors fail safe (drop the field, keep the line, count it).
- Tests: pattern corpus (positives + near-miss negatives), wrapped errors, denylist, concurrency.

### `telemetry` (#679)

One-line OTel: traces + metrics + log correlation, standard resource attributes.

```go
shutdown, err := telemetry.Setup(ctx, telemetry.Config{
    Service: "medicore", Version: version, Environment: env, Country: "IN",
})
defer shutdown(ctx)
```

- Auto-instrumentation glue for httpkit, database, events, cache.
- Cardinality guard: tenant allowed on spans; on metrics only via bounded top-N views. Patient/user IDs never label anything.
- Failure: collector unreachable → buffer/drop with internal counter; never blocks a request.
- Tests: attribute presence, cardinality guard, non-blocking under collector outage.

### `httpkit` (#680)

Gin bootstrap that _is_ the API style guide (#666) in code.

```go
r := httpkit.New(httpkit.Config{Service: "medicore", CORS: cors, Timeout: 10 * time.Second})
r.GET("/patients/:id", handler)                    // stack pre-wired
httpkit.Fail(c, http.StatusNotFound, "PATIENT_NOT_FOUND", "patient does not exist")
```

- Middleware order (fixed): request-ID → recovery → timeout → telemetry → logging → CORS → authn (opt-in per group) → tenant.
- Error envelope: `{"error":{"code","message","requestId","details":[...]}}` — the only shape any handler can produce.
- `/healthz`, `/readyz` (with pluggable checks), `/metrics` isolation; graceful shutdown.
- Tests: envelope golden files per status; panic → 500 envelope with request ID; timeout → 504 + downstream context cancel.

### `authn` (#681)

Keycloak OIDC/JWT validation → typed `Principal` in context.

```go
mw := authn.Middleware(authn.Config{Issuer: realmURL, Audience: "helivanta-api"})
p := authn.PrincipalFrom(ctx)                     // Subject, Roles, TenantID, SessionID
```

- JWKS cached with rotation; issuer/audience/exp/signature verified; both realms (customer, internal) supported side by side.
- Failure: invalid token → 401 envelope, token contents never logged. JWKS cold-start unavailable → fail closed.
- Tests: full negative matrix (expired, wrong aud, wrong issuer, unknown kid, alg confusion, clock skew bounds).

### `authz` (#682)

OpenFGA wrapper with healthcare-shaped helpers.

```go
ok, err := authz.Check(ctx, authz.CanViewPatient(principal, patientID))
authz.MustAll(ctx, checks...)                      // batch; any failure → deny
```

- Short-TTL decision cache (bounded staleness after revocation — documented figure).
- Failure: FGA unreachable/error → **deny** with 503 envelope. Never fail open.
- Tests: fail-closed on every error class; cache TTL expiry; batch semantics — against a real FGA container (testkit).

### `tenant` (#683) — the isolation keystone

```go
ctx = tenant.With(ctx, tenantID)                   // set by middleware from Principal
id, err := tenant.From(ctx)                        // ErrNoTenant if absent
ctx = tenant.System(ctx, "nightly-retention-job")  // explicit bypass: audited, reason required
```

- Propagates across HTTP (middleware), events (envelope field → consumer context), and goroutines.
- `database` refuses to open a tenant-bound transaction without it (below).
- Tests: propagation matrix; guard refusal; `System()` emits audit event; NATS round-trip carries tenant.

### `database` (#684)

pgx pool + transaction discipline that welds tenancy to RLS.

```go
pool, err := database.Connect(ctx, cfg)            // pooled, traced, health-checked
err = database.WithTenantTx(ctx, pool, func(tx pgx.Tx) error {
    // SET LOCAL app.tenant_id = <ctx tenant> already applied; RLS enforces the rest
    ...
})
```

- `WithTenantTx` errors if ctx has no tenant (unless `tenant.System`, which routes to `WithSystemTx` and audits).
- RLS contract: policies read `current_setting('app.tenant_id')`; policies themselves live in `tesserix-k8s` schemas (never here); CI cross-check that every tenant-owning table has them.
- GORM adapter provided for teams that want it — same guard underneath.
- Tests (testkit): two-tenant adversarial suite; guard refusal; `SET LOCAL` scoping (leaks nothing after commit/rollback); pool exhaustion behaviour.

### `events` (#685)

Typed JetStream pub/sub with transactional outbox.

```go
events.Publish(ctx, tx, PrescriptionIssued{...})   // written to outbox in the same tx
events.Consume(ctx, events.Consumer{Type: PrescriptionIssued{}, MaxDeliver: 5,
    Handle: func(ctx context.Context, e PrescriptionIssued) error { ... }})   // ctx carries tenant
```

- Envelope: id, type, version, tenant, occurredAt, payload — validated against the registry (#667).
- Outbox relay ships committed events; consumers get retry/backoff, DLQ with alert, idempotency helper.
- Failure: poison message → DLQ, stream continues; relay down → events accumulate durably, gap alert.
- Tests: kill relay/consumer mid-flow with zero loss; DLQ path; idempotent redelivery; tenant round-trip.

### `audit` (#686)

```go
audit.Record(ctx, audit.RecordAccess{Entity: audit.Patient(id), Action: audit.View})
```

- Event: actor, action, entity ref, tenant, reason, before/after _references_ (never inline PHI); hash-chained per tenant stream (`hash = H(prev ‖ canonical(event))`).
- Delivered via `events` outbox → gap-free even during pipeline outages.
- `VerifyChain` tool for integrity audits.
- Tests: chain verification detects mutation/deletion; mandated categories (record access, prescription/billing change, break-glass, export) have helpers; outbox durability.

### `cache` (#687)

Redis cache-aside with structural tenant namespacing (`{tenant}:{service}:{key}` — builder API makes bypass impossible), singleflight, circuit breaker → fall through to source when Redis is down.
Tests: cross-tenant key isolation; breaker fall-through; stampede control.

### `storage` (#688)

Object storage with short-TTL signed URLs, AV-scan quarantine gate, data-classification tags (drives retention), tenant-scoped paths. Fail: unscanned/flagged object → no URL, ever.
Tests: EICAR quarantine flow; URL expiry; tenant path scoping.

### `ratelimit` (#689)

Redis token buckets per tenant/principal; 429 + Retry-After envelope; config-driven quotas per plan; **exemption tier for life-critical routes** (the one documented fail-open). Tests: tenant A saturation leaves tenant B unaffected; store-down fallback policy.

### `i18n` + country profile accessor (#690)

Server message catalogs (fallback chain → English, missing-key metric) and typed `countryprofile.Get(ctx)` (currency, tax, regulatory flags, adapter selection). Lint: no `if country == ...`.

### `flags` (#691)

GrowthBook wrapper: standard attributes (tenant, plan, country, role), local cache + background refresh, deterministic safe defaults offline (features off, kill-switches on), typed accessors.

### `notify` (#692)

Channel-agnostic send API; provider plugins routed by country profile (WhatsApp-first for India, SMS fallback); templates by reference; delivery-status callbacks; opt-out enforcement; idempotency keys.

### `payments` (#693)

Interfaces only + Razorpay implementation: create/capture, refund, mandate (UPI AutoPay), webhook signature verification, reconciliation fetch; idempotency on all mutations; CI fake.

### `testkit` (#694)

Testcontainers for CNPG/Redis/NATS/Keycloak/OpenFGA with health-wait; fixtures: two seeded tenants, standard principals, FGA tuples; the **adversarial isolation harness** every service must run; golden-file + contract-test helpers.

### `create-helivanta-service` template (#695)

Not a package — a generator producing a service with every package wired, sample endpoint/event/test, Dockerfile, CI (from DevEx templates #709), Helm/ArgoCD stubs. The generated service passes lint, tests, scans, and ships dashboards (#716) unmodified.

---

## Testing approach (per test strategy #671)

| Level                                                 | Where                                               | Gate                   |
| ----------------------------------------------------- | --------------------------------------------------- | ---------------------- |
| Unit (pure logic: redaction, envelope, chain, config) | package `_test.go`                                  | PR                     |
| Integration (real containers: DB/RLS, FGA, NATS)      | via `testkit`, tagged `integration`                 | PR (cached containers) |
| Adversarial isolation suite                           | `testkit` harness, run by SDK **and** every service | PR + nightly           |
| Contract (envelope/event schemas)                     | SDK CI vs sample consumer                           | PR                     |
| Race/concurrency                                      | `go test -race` everywhere                          | PR                     |
