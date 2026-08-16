# HMS backend standards

Binding rules for every module under `backend/internal/modules/*` and the
shared platform packages (`backend/internal/platform/*`, `backend/pkg/*`).
"Binding" means: code that doesn't follow these rules should not pass
review, and where a rule can be machine-enforced it is (golangci-lint,
`go vet`, arch tests, the coverage gate). Every section below points at a
real file in this repo — read that file before writing new code in the
same area.

## 1. Module anatomy

A module is a single Go package at `internal/modules/<name>/module.go`
implementing `platform.Module` (`backend/internal/platform/module.go`):

```go
type Module interface {
	Name() string
	Migrations() []tenantdb.Migration
	// Permissions declares every permission this module's routes use and
	// which system roles hold it (section 11 — RoleTenantAdmin is
	// implicit, never list it here).
	Permissions() []authz.Grant
	Routes(r *Router, deps Deps)
	Consumers(deps Deps) []events.Consumer
}
```

`platform.Deps` (same file) is the only thing a module may depend on —
`*tenantdb.DB` and `*events.Bus`. There is no other way to reach a
database connection or the message bus from inside a module.

A module lives entirely in one package: `module.go` (routes, migrations,
consumers), row structs, request/response types, and `module_test.go`
all sit at `internal/modules/<name>/` with no sub-packages. See
`backend/internal/modules/pharmacy/module.go` and
`backend/internal/modules/medicore/module.go` for the shape.

Registering a module means adding it in **two** places, or its routes
never mount and its migrations never run:

1. `backend/internal/bootstrap/modules.go`'s `Modules()` — the single
   runtime registry constructor, shared by `cmd/api` (boots the full API)
   and `cmd/migrate` (applies migrations only, no NATS/OpenFGA dependency):
   ```go
   func Modules() []platform.Module {
       return []platform.Module{
           iam.New(), reference.New(), medicore.New(), pharmacy.New(), lab.New(),
       }
   }
   ```
   `cmd/api/main.go` and `cmd/migrate/main.go` both call
   `bootstrap.NewRegistry()`, so the module list exists exactly once at
   runtime — no hand-maintained copy per entrypoint.
2. `backend/internal/archtest/arch_test.go`'s `allModules()` — the test
   registry that arch tests (isolation, migration-ID uniqueness, consumer
   contracts, RLS lint) iterate over. It is deliberately a second literal
   list, not a shared import of `bootstrap.Modules()`, so a module wired
   into one but not the other fails CI instead of silently running
   unchecked. `TestMainRegistersExactlyAllModules` diffs the two sets on
   every run.

Never hand-write a new module from scratch. Run
`make new-module NAME=<name>` (`backend/scripts/new-module.sh`) — it
scaffolds `internal/modules/<name>/module.go` and `module_test.go` from
the pharmacy/medicore template (forced-RLS migration with a status
`CHECK`, `respond`-helper routes, a published event constant, a consumer
stub, and `testutil.ModuleHarness`-based tests), `gofmt`s the result, and
prints the two registration follow-ups above plus a reminder to rename
the placeholder `item` domain nouns.

## 2. Isolation rules

Modules never import each other. Cross-module data flows only via
events (section 6) — never a direct function call, never a shared
repository, never reaching into another module's tables. This is
enforced twice, for different failure modes:

- **Lint (fast, every save):** `backend/.golangci.yml`'s `depguard`
  `module-isolation` rule denies any import of
  `github.com/tesserix/hms/internal/modules` from files under
  `**/internal/modules/**`:
  ```yaml
  depguard:
    rules:
      module-isolation:
        files:
          - "**/internal/modules/**"
        deny:
          - pkg: "github.com/tesserix/hms/internal/modules"
            desc: "modules must not import other modules — cross-module data flows only via events (spec D3)"
  ```
  A module's own `_test.go` package importing itself (e.g.
  `pharmacy_test` importing `pharmacy`) is a self-import, not
  cross-module coupling, and carries an explicit
  `//nolint:depguard // external test package importing the module under
test (self-import), not cross-module coupling` comment — see the top of
  `backend/internal/modules/pharmacy/module_test.go`.
- **Arch test (structural, CI):** `TestModulesDoNotImportEachOther` in
  `backend/internal/archtest/arch_test.go` loads the real package graph
  with `golang.org/x/tools/go/packages` and fails if any module package
  imports a different module's package path, catching transitive imports
  depguard's glob can miss.

When one module needs to react to another module's write, it consumes
that module's published event. Pharmacy needs a medicore visit's ID and
patient name to open a pending dispense — it does not import medicore.
Instead it repeats medicore's subject string by value and consumes it:

```go
const (
	// SubjectVisitCreated is medicore's subject, repeated by value —
	// modules must not import each other (spec D6 / phase 1).
	subjectVisitCreated     = "hms.in.medicore.visit_created.v1"
	SubjectDispenseRecorded = "hms.in.pharmacy.dispense_recorded.v1"
)
```

(`backend/internal/modules/pharmacy/module.go`)

## 3. Data access

`*tenantdb.DB` (`backend/pkg/tenantdb/db.go`) is the only database
handle a module ever sees, and it never exposes a raw `*gorm.DB` field —
every access goes through one of three methods, each opening its own
transaction:

- **`WithTenant(ctx, tenantID, fn)`** — the path for every tenant-scoped
  table. It validates `tenantID` is a UUID, opens a transaction on the
  app pool (a non-`BYPASSRLS` role), and sets the tenant GUC
  transaction-local before calling `fn`:
  ```go
  func (d *DB) WithTenant(ctx context.Context, tenantID string, fn func(tx *gorm.DB) error) error {
  	if _, err := uuid.Parse(tenantID); err != nil {
  		return ErrInvalidTenant
  	}
  	return d.app.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
  		if err := tx.Exec(`SELECT set_config('app.tenant_id', ?, true)`, tenantID).Error; err != nil {
  			return err
  		}
  		return fn(tx)
  	})
  }
  ```
  `set_config(..., true)` (transaction-local, not session-local) matters:
  an unset GUC makes every RLS policy evaluate to `NULL`, which reads as
  zero rows rather than an error — a silent data-loss bug, not a crash,
  if the GUC scoping were session-local and leaked across pooled
  connections.
- **`WithSystem(ctx, fn)`** — for platform tables that have no
  `tenant_id` column at all (the outbox, `processed_events`). It opens a
  transaction on the same app pool with **no** tenant GUC set, so every
  RLS-protected tenant table reads as empty inside it — `WithSystem` is
  not an escape hatch for tenant data, it is the mechanism used to touch
  the platform tables that predate tenancy.
- **`WithAdmin(ctx, fn)`** — opens a transaction on the *admin* pool (the
  migration role), which bypasses RLS completely and sees every tenant's
  rows in every table. This is for boot-time/ops code only, never a
  request path — the permission reconciler
  (`backend/internal/platform/reconcile.go`) is currently its only
  caller, enumerating every tenant's memberships in one pass with no
  single tenant to scope a `WithTenant` GUC by. `TestWithAdminIsOnlyCalledFromTheAllowlist`
  in `backend/internal/archtest/arch_test.go` enforces this mechanically:
  any new `.WithAdmin(` call site outside that allowlist fails the build.
  Extending the allowlist is a real RLS-bypass decision — bring it to
  review rather than adding a file to it.

Every tenant table gets forced RLS in the migration that creates it —
`ENABLE ROW LEVEL SECURITY` alone is not enough, because a table owner
(which the migration role effectively is) bypasses ordinary RLS by
default; `FORCE ROW LEVEL SECURITY` closes that hole. The checklist,
taken from `backend/internal/modules/pharmacy/module.go`'s
`pharmacy_medications` migration:

```sql
CREATE TABLE pharmacy_medications (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  name text NOT NULL,
  strength text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE pharmacy_medications ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_medications FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_medications
  USING (tenant_id = current_setting('app.tenant_id', true)::uuid)
  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
CREATE INDEX ON pharmacy_medications (tenant_id, created_at DESC);
```

Checklist for any new tenant table:

1. `tenant_id uuid NOT NULL` column.
2. `ENABLE ROW LEVEL SECURITY` **and** `FORCE ROW LEVEL SECURITY`.
3. A policy with **both** `USING` and `WITH CHECK` clauses scoped to
   `current_setting('app.tenant_id', true)::uuid` — a policy with only
   one clause is deliberately flagged by the linter below.
4. A `(tenant_id, created_at DESC)` index if the table is ever listed
   (section 4's newest-first-LIMIT-100 rule needs it).

This is not just convention — it's linted twice. `DB.LintRLS`
(`backend/pkg/tenantdb/db.go`) queries `pg_class`/`pg_policies` for any
table with a `tenant_id` column that is missing forced RLS or a policy
carrying both clauses, and `TestAllMigrationsPassRLSLint`
(`backend/internal/archtest/rls_test.go`) applies every module's
migrations to a fresh database and runs that lint in CI. Every module
test built on `testutil.ModuleHarness` (section 9) runs the same lint at
harness setup, so a missing `FORCE ROW LEVEL SECURITY` fails the very
first test in the package, not just the dedicated arch test.

## 4. HTTP semantics

Every response goes through `respond` (`backend/internal/platform/respond/respond.go`)
— handlers never call `c.JSON`/`c.AbortWithStatusJSON` directly:

| Helper                            | Status | Envelope                                            | Use                                                |
| --------------------------------- | ------ | --------------------------------------------------- | -------------------------------------------------- |
| `respond.OK(c, data)`             | 200    | `data` unchanged                                    | reads, and successful state transitions            |
| `respond.Created(c, data)`        | 201    | `data` unchanged                                    | synchronous creates                                |
| `respond.Accepted(c, data)`       | 202    | `data` unchanged                                    | async creates (section 6 — write lands via outbox) |
| `respond.NotFound(c, res)`        | 404    | `{"error":"not_found","message":"<res> not found"}` | missing **or cross-tenant** resource               |
| `respond.Conflict(c, msg)`        | 409    | `{"error":"conflict","message":msg}`                | guarded-UPDATE lost race / bad transition          |
| `respond.BadRequest(c, err)`      | 400    | `{"error":"invalid_request","message":err.Error()}` | binding/validation failure                         |
| `respond.Internal(c, msg)`        | 500    | `{"error":"internal","message":msg}`                | unexpected DB/bus error                            |
| `respond.Unauthenticated(c, msg)` | 401    | `{"error":"unauthenticated","message":msg}`         | missing/invalid credentials (used by `authn`)      |

Error envelope shape (`{"error", "message"}`) and success envelope shape
(`{"data": [...]}` for lists, raw object for single resources) are
frozen — a handler must not invent a new response shape. Paginated
collections keep `data` and add a sibling `page` (below).

**Collection endpoints are registered through `platform.ListRoute`, never
`g.GET`.** It owns the limit clamp, the keyset, the `+1` probe, the trim and
the `{"data": [...], "page": {...}}` envelope, so a handler cannot forget any
of them. Default page size 50, maximum 200; an out-of-range `limit` is a 400,
never a silent clamp. Row types implement `platform.Keyed` (`PageKey() (time.Time,
uuid.UUID)`). `TestEveryCollectionGETIsPaginated` fails any collection GET that
is neither paginated nor in `unpaginatedGETAllowlist` with a reason — the two
kinds that qualify are single-item reads and collections bounded by
construction rather than by tenant data.

**404, never 403, for cross-tenant access.** Every read is
`WithTenant`-scoped (section 3), so a row belonging to another tenant is
invisible to the query in the first place — the handler cannot even
distinguish "doesn't exist" from "exists, wrong tenant," and must not try
to (returning 403 there would leak the row's existence to a
non-authorized caller). Pharmacy's dispense lookup is the canonical
example: a cross-tenant `POST .../dispense` fails at
`tx.First(&row, "id = ?", id)` with `gorm.ErrRecordNotFound` because the
RLS-scoped transaction never sees the other tenant's row, and the
handler maps that straight to `respond.NotFound`:

```go
if errors.Is(err, gorm.ErrRecordNotFound) {
	respond.NotFound(c, "dispense")
	return
}
```

**409 via a guarded UPDATE**, not a read-then-write race. A
`SELECT ... FOR UPDATE` followed by a separate `UPDATE` still races
against a concurrent request between the two statements unless you take
a row lock and hold the transaction, which this codebase avoids in favor
of a single conditional `UPDATE ... WHERE status = '<precondition>'` and
checking `RowsAffected`. From
`backend/internal/modules/pharmacy/module.go`'s dispense transition:

```go
err = deps.DB.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
	var row dispense
	if err := tx.First(&row, "id = ?", id).Error; err != nil {
		return err
	}
	if row.Status != "pending" {
		status = http.StatusConflict
		return nil
	}
	now := time.Now().UTC()
	result := tx.Model(&dispense{}).Where("id = ? AND status = 'pending'", id).
		Updates(map[string]any{"status": "dispensed", "medication": req.Medication, "dispensed_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		// Lost the race to a concurrent dispense between the
		// pre-check above and this guarded UPDATE.
		status = http.StatusConflict
		return nil
	}
	data, err := json.Marshal(dispenseRecordedData{DispenseID: row.ID.String(), VisitID: row.VisitID.String()})
	if err != nil {
		return err
	}
	return deps.Bus.Publish(tx, SubjectDispenseRecorded, events.Event{
		Type: "DispenseRecorded", Version: 1, TenantID: p.TenantID, Data: data,
	})
})
```

The initial `row.Status != "pending"` check is a fast path for the
common case (already dispensed); the `RowsAffected == 0` check after the
`WHERE status = 'pending'` UPDATE is what actually closes the race — a
second concurrent request that passes the fast-path check but loses the
`UPDATE` still gets `RowsAffected == 0` and 409s correctly. Publishing
the event happens only after `RowsAffected > 0` confirms this request
won the transition, and it happens in the same transaction (section 6).

**202 for async creates.** A create whose downstream effects run through
the outbox/consumer pipeline (section 6) rather than completing
synchronously in the request returns `respond.Accepted`, not `Created` —
medicore's visit creation is the reference: the visit row and the
`visit_created` outbox row commit together, but pharmacy's consumption
of that event (opening a pending dispense) happens asynchronously, so
the caller only gets confirmation the write was accepted, not that every
downstream module has reacted.

**Lists are newest-first, `LIMIT 100`.** Every list handler orders
`created_at DESC` and caps at 100 rows — there is no pagination yet, so
an unbounded list query is a footgun the moment a tenant accumulates
more rows than fits comfortably in one response:

```go
err := deps.DB.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
	return tx.Order("created_at DESC").Limit(100).Find(&rows).Error
})
```

(`backend/internal/modules/pharmacy/module.go`'s medication/dispense
list handlers; the `(tenant_id, created_at DESC)` index from section 3
is what keeps this cheap.)

## 5. Auth

`authn.TenantPrincipal(c)` (`backend/pkg/authn/authn.go`) is how every
handler gets the caller's identity — it is never read off the context by
hand. It extracts the principal set by the auth middleware, parses its
tenant claim as a UUID, and on failure writes the 401 envelope, aborts
the request, and returns `ok = false` so the handler's only job is to
check the bool and return:

```go
func (m *Module) Routes(r *gin.RouterGroup, deps platform.Deps) {
	g := r.Group("/pharmacy")

	g.POST("/medications", func(c *gin.Context) {
		p, tenantUUID, ok := authn.TenantPrincipal(c)
		if !ok {
			return
		}
		// ... p.TenantID / tenantUUID drive every WithTenant call below
```

`p` is the `authn.Principal{Subject, TenantID}`; `tenantUUID` is the
already-parsed `uuid.UUID` form, so handlers that need the string form
(`WithTenant`'s signature) use `p.TenantID` and handlers that need the
typed form (row structs) use `tenantUUID` — both are returned so neither
call site re-parses.

**Middleware chain.** The `/v1` chain is declared in
`bootstrap.V1Chain` (`backend/internal/bootstrap/chain.go`), and
`cmd/api/main.go` mounts it:

```go
api := platform.NewRouter(
	srv.Engine.Group("/v1", bootstrap.V1Chain(verifier, revocationChecker, limiter, cfg, fga)...),
	fga,
)
```

```go
// bootstrap.V1Chain, in order:
authn.Middleware(verifier, revocations),   // 1. verify the token
requestid.PrincipalMiddleware(),           // 2. attribute the logs
ratelimit.Middleware(limiter, RateLimitConfig(cfg)), // 3. refuse over-budget
authz.Middleware(resolver),                // 4. resolve permissions (OpenFGA)
```

**The chain is a function, not a literal inside `main.go`, and that is
load-bearing.** `run()` opens a database, a NATS connection and an OIDC
provider before it builds a router, so nothing in `main.go` is reachable
from a test. While the chain was inline, deleting `ratelimit.Middleware`
removed rate limiting from production and the entire backend suite
stayed green — the placement test built its own equivalent chain and
kept passing against a replica of a chain production no longer had. Arch
tests build their harness from `V1Chain` so that mutation now fails CI.
**Do not re-inline a middleware into `main.go`, and do not hand-copy the
chain into a test.**

`requestid.Middleware()` (section 8) runs earlier still, on every route
including `/healthz`, stamping a request ID before auth runs so a 401 log
line is still correlated. `authn.Middleware` reads a `Bearer` header or
the `hms_session` cookie, verifies it via the injected `TokenVerifier`
(`authn.NewSessionVerifier` over `pkg/session` in production, since #838 made
the HMS session — not the IdP token — what a `/v1` request presents;
`testutil.StaticVerifier` in tests), and sets the `authn.Principal` on the Gin context for
`TenantPrincipal` to read later; a missing or invalid credential aborts
with 401 before any module handler runs.

### Rate limiting

**Every `/v1` route is rate limited**, per tenant *and* per principal —
both buckets must allow. The tenant bucket protects other hospitals from
a noisy one; the principal bucket protects a hospital from one of its own
users, or from a compromised credential looping.

Placement is the whole design: **after `authn`** (the tenant key is only
trustworthy because it comes from a verified token, not a client header)
and **before `authz`** (which calls OpenFGA on every request — the most
expensive step in the chain and itself a shared resource). Limiting after
`authz` would let a flood exhaust OpenFGA before anything was refused,
and the limiter would still return 429, so nothing would look broken.
`TestThrottledRequestMakesNoOpenFGACall` pins that order by counting
OpenFGA calls, not by inspecting the chain.

Routes consuming a shared **external** resource may get a tighter budget
in `RateLimitConfig`'s `Tight` map — a tight rule gets its **own** bucket
key, so draining it does not drain the budget every other route reads
from. `Tight` is empty as of #838: its one entry, `POST
/v1/iam/me/tenant`, was budgeted tighter because it minted a GIP custom
token per call against project-wide Identity Platform quota. #838
replaced that with an in-process HMS session re-mint (one Ed25519
signature) plus one OpenFGA membership check — the same shape of cost
every other authenticated route already pays through `authz.Middleware`'s
own `Resolve` call — so there is no longer a shared external resource
this route uniquely threatens, and it is deliberately left off `Tight`
rather than carried forward with a stale rationale.

Routes that must never be throttled go in `Exempt` **with a reason**,
pinned by `TestRateLimitExemptionsAreAllowlisted`. The bar is high: an
exempt route is one an attacker may hammer without being refused. The two
entries today are sign-out and admin revoke — security controls whose
whole purpose is to work during the incident that would trip a limiter.
Exempt means exempt from the *limit*, not from the *record*: exempt
requests are still logged, or the exemption becomes a blind spot.

`TestRateLimitPolicyRoutesAreRegistered` additionally requires every
`Exempt` and `Tight` key to name a route that actually exists. A typo'd
key is otherwise silently dead — sign-out would be rate limited during
exactly the incident it must survive, with nothing red.

Rate limiting is a **capacity** control, so per
`docs/standards/engineering-principles.md` §3 it fails **open**, unlike
authorization or tenant scoping. The in-memory limiter cannot be
unavailable; when a Redis-backed one replaces it (#7), a limiter that
cannot reach its store must admit the request and alert, never deny.
Limits come from env (`RATE_LIMIT_TENANT_PER_MIN`,
`RATE_LIMIT_PRINCIPAL_PER_MIN`) with production defaults, and an
unparseable value falls back to the default with a warning rather than
refusing to boot — same call `LOG_LEVEL` makes.

**Known cost:** the limiter is in-process, so with N replicas the
effective global limit is N × configured — exact for the connection pool
(per-process), and the trade ADR-0005 accepts until #7 fixes the replica
count.

### The login-client credential

`ZITADEL_LOGIN_CLIENT_TOKEN` is an **instance-level** Zitadel PAT: its
holder can finalise an OIDC auth request for *any* app on the shared
Zitadel instance, not just HMS's own (`docs/superpowers/specs/2026-08-16-hms-login-client-design.md`
D2). It is the most privileged secret HMS holds. It **must never appear
in a frontend service or in a log** — it lives only in the Go API
(`backend/internal/modules/iam/loginclient`), which loads it from env the
same way every other secret does, and `loginclient.Client` never embeds
it, a session token, or a raw Zitadel error body in an error string (see
`readZitadelErrorID`'s doc comment in `client.go`) precisely so a log
line built from one of its errors cannot leak it either.

A missing `ZITADEL_LOGIN_CLIENT_TOKEN` is a **boot refusal**, not a
degraded mode: without it the API cannot check a credential at all, and
starting anyway would serve a login form that fails every submission. So
is a missing **`HMS_WEB_ORIGIN`** outside `HMS_ENV=dev` — it is the
origin HMS's own `/login` is served from, and
`config.RequireDistinctHostedLoginOrigin` compares it against
`ZITADEL_HOSTED_LOGIN_URL` to refuse a configuration where the handoff
target points back at the page that just decided to hand off. That
misconfiguration loops every MFA-enrolled clinician forever while every
individual request succeeds — no non-2xx, no log line above INFO — so
there is nothing for alerting to catch and it has to be caught at boot.
Both are on the enforcement ladder's "boot failure" rung deliberately.

The one call this credential can make that matters most — finalising an
auth request — sits behind `loginclient.Client.CompleteIfSufficient`, the
**only** path that can reach the unexported `finalize` call
(`sufficiency.go`). A caller cannot skip the sufficiency decision (org
`forceMfa` and `forceMfaLocalOnly`, and — since #854 Task 8 — the user's
own enrolled second factors) by calling `finalize` directly, because
nothing outside the package can name it. This is enforced by an arch
test, not a convention: it asserts the Zitadel finalize call appears in
exactly one call site **within `backend/`**, so a second Go call site
added later fails CI rather than silently becoming a bypass.

Two boundaries on that claim, stated so it is not read as more than it
is. The arch test walks `backend/` only, and excludes `_test.go` files.
Outside that scope there is exactly one **known and accepted** other call
site: `scripts/lib/zitadel.mjs`'s `passwordLoginIDToken`, a dev-only
verification helper that mints an ID token for local testing. It is not
production code, does not ship, and is not reachable from a request — but
it does POST the finalize endpoint with the login-client PAT, so "nothing
outside `CompleteIfSufficient` finalises an auth request" is true of the
Go API and not of the repository. Nothing in the frontend does; keeping
it that way is a deployment/config control (keep the PAT out of the
frontend's reach), not something a Go arch test can enforce.

The org-policy half of the sufficiency decision reads **two** wire fields,
not one: `forceMfa` and `forceMfaLocalOnly` (the latter a real, supported
Zitadel configuration — "require MFA for local users, not federated
ones"). They are registered in a single list (`mfaPolicyKeys`) that
drives both the read and the rename/re-casing guard, so adding a third is
a one-place change; a test pins that the read is genuinely list-driven,
because a list that guards a key without reading it is a silent MFA
bypass.

## 6. Events

All cross-module data flows through NATS JetStream via
`*events.Bus` (`backend/pkg/events/bus.go`), never a direct call.

**Subjects** must match
`^hms\.[a-z]+\.[a-z]+\.[a-z_]+\.v\d+$` (`archtest`'s `subjectRe`) —
direction, module, event name, version:
`hms.in.pharmacy.dispense_recorded.v1`. **Consumer names** must match
`^[a-z]+-[a-z-]+$` (`consumerRe`) — module prefix, purpose:
`pharmacy-visit-intake`. Both are checked in CI by
`TestConsumerContracts` and `TestPublishedSubjectConstants` in
`backend/internal/archtest/arch_test.go`, against every module's
`Consumers(deps)` and every module's exported `Subject*` constant.

**Broadcast subscriptions are the one exception to consumer naming.** The
`<module>-<purpose>` rule assumes a work queue, where replicas sharing a
durable name compete and exactly one handles each message. A broadcast
(`events.Broadcast`, `Bus.StartBroadcasts`) is the opposite: every replica
must receive every message, so it uses an ephemeral, unnamed JetStream
consumer with `DeliverNew`. Use it only for state whose durable truth
lives elsewhere — the credential-revocation cache invalidation (#781) is
the model: Postgres holds the watermark, the broadcast only says "re-read
it". A broadcast handler gets no transaction and no idempotency claim,
because a dropped or duplicated hint must be harmless by construction.

**Versioning is additive, never in-place.** A breaking payload change
gets a new subject (`...v2`) and a new consumer — the old `v1` consumer
keeps running against the old subject until every publisher and consumer
of it is retired. There is no in-place schema migration of an event
payload; `Event.Data` is `json.RawMessage`
(`backend/pkg/events/types.go`) precisely so each version's consumer
decodes only the shape it declares.

**Idempotency is platform-provided, not per-handler.** Every consumer
message is claimed via an `INSERT ... ON CONFLICT DO NOTHING` into
`processed_events` keyed on `(consumer, event_id)` before the handler
runs, inside `Bus.handleMsg`:

```go
err := db.WithSystem(ctx, func(tx *gorm.DB) error {
	if _, err := uuid.Parse(evt.TenantID); err == nil {
		if err := tx.Exec(`SELECT set_config('app.tenant_id', ?, true)`, evt.TenantID).Error; err != nil {
			return err
		}
	}
	res := tx.Exec(`INSERT INTO processed_events (consumer, event_id) VALUES (?, ?) ON CONFLICT DO NOTHING`,
		c.Name, evt.ID)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return nil // duplicate delivery — no-op (idempotency)
	}
	// Handler runs in the SAME tx as the idempotency claim — do not
	// open your own transaction; rollback of the claim implies
	// rollback of handler effects.
	return c.Handle(ctx, tx, evt)
})
```

A module's `Consumer.Handle` never opens its own transaction and never
inserts into `processed_events` itself — both are handled by the bus.
The tx handed to `Handle` is also tenant-scoped from `evt.TenantID`
before the handler runs, which is what lets a consumer make
RLS-forced writes without re-deriving the tenant itself; pharmacy's
visit-intake consumer relies on exactly this:

```go
func (m *Module) Consumers(deps platform.Deps) []events.Consumer {
	return []events.Consumer{{
		Name:    "pharmacy-visit-intake",
		Subject: subjectVisitCreated,
		Handle: func(ctx context.Context, tx *gorm.DB, evt events.Event) error {
			var d visitCreatedData
			if err := json.Unmarshal(evt.Data, &d); err != nil {
				return err
			}
			// The bus scoped this tx to evt.TenantID (Task 1), so this
			// RLS-forced insert lands under the visit's tenant.
			return tx.Exec(`INSERT INTO pharmacy_dispenses (tenant_id, visit_id, patient_name)
				VALUES (?, ?, ?)`, evt.TenantID, d.VisitID, d.PatientName).Error
		},
	}}
}
```

**DLQ behavior:** a message is redelivered up to `maxDeliver` (5) times
on handler failure (`Nak`); once `NumDelivered >= maxDeliver`, `handleMsg`
publishes the raw payload to `hms.dlq.<consumer>` and `Term`s the
original instead of Nak-ing it forever. If the DLQ publish itself fails,
the message is `Nak`'d (not `Term`'d) so the next redelivery gets another
chance to dead-letter it — the alternative (`Term` unconditionally) would
silently drop the event from both the live stream and the DLQ.

**Outbox publishing happens inside the business transaction**, never
after it commits. `Bus.Publish(tx, subject, evt)` is just an `INSERT`
into `outbox_events` on the caller's `tx` — it does not touch the
network:

```go
func (b *Bus) Publish(tx *gorm.DB, subject string, evt Event) error {
	// ...
	return tx.Create(&outboxRow{ID: id, Subject: subject, Payload: payload}).Error
}
```

A background `RunDispatcher` loop (started once in `main.go` via
`go bus.RunDispatcher(ctx, db)`) drains unpublished outbox rows into
JetStream every 500ms using `FOR UPDATE SKIP LOCKED`. This is what makes
the write atomic with the "I published this" fact — a crash between the
business write and a naive direct-publish would either lose the event or
duplicate the write; committing the outbox row in the same transaction
as the row it describes means the event is guaranteed to eventually
publish if and only if the transaction committed. Every module route
that publishes does so from inside its `WithTenant`/`WithSystem` closure,
on the same `tx` as the row create — see pharmacy's dispense handler and
the generated `new-module.sh` template's `items` create handler for the
pattern.

**An event's subject and payload live in the publishing module's `contract`
package, and nowhere else.** `internal/modules/<module>/contract` is the one
part of a module that other modules may import — the exception exists so a
publisher and its consumers share one definition instead of copies, because a
renamed field in a copied struct is not an error to `encoding/json`, just a
zero value written into a real record (#827).

Contract packages are **data only**: `const`, `type`, `var`, no funcs or
methods, importing nothing beyond `time` and `github.com/google/uuid`.
`TestContractPackagesDeclareOnlyData` and `TestContractPackagesImportAlmostNothing`
enforce it; without them the exception would be a hole in module isolation
rather than a narrow opening in it.

Import a foreign contract aliased `<module>contract` — two packages both named
`contract` will not compile unaliased, and `contract.VisitCreatedData` does not
tell a reader whose contract it is.

Every module declares `Publishes() []string`, using constants from its own
contract package. `TestEveryConsumedSubjectIsPublished` fails any consumer
subscribed to a subject no module publishes; `TestNoSubjectIsPublishedByTwoModules`
keeps one subject to one publisher. A published subject with no consumer is
legal and checked by nothing.

### The outbox is inside the RLS boundary

`outbox_events` carries a `tenant_id` and a forced-RLS policy like any other
tenant table (#835). It used to carry none, which meant a patient name written
into a payload was readable by **any** `WithSystem` transaction in any module,
forever, with no policy in the way.

`tenant_id` is **nullable**, and that is deliberate. `SubjectCredentialRevoked`
publishes with no tenant at all — revocation is subject-scoped and ends every
session for a subject *in every tenant* — and broadcasts travel the same
`Publish` → outbox → dispatcher path as module events. So the policy is
asymmetric:

```sql
USING      (hms_tenant_visible(tenant_id))
WITH CHECK (tenant_id IS NULL
            OR tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
```

- `USING` omits the NULL case, so a tenant-less row is readable by **nobody**
  under RLS. A platform-wide event is not any single tenant's to read.
- `WITH CHECK` must permit NULL, or publishing a tenant-less event from inside
  a tenant-scoped transaction — exactly what sign-out does — is rejected and
  sign-out 500s.

**Every `WITH CHECK` needs its own `NULLIF`.** `USING` clauses are safe because
they call `hms_tenant_visible`, which carries it; `WITH CHECK` clauses *cannot*
call that function (the lint requires writes to stay pinned to one tenant), so
they must wrap `current_setting` themselves. Without it, an unset GUC on a
**reused pooled connection** is `''` rather than NULL — `current_setting`
returns SQL NULL only the first time a session ever names it — and `''::uuid`
is a hard type error. The same cross-tenant write then reports
`new row violates row-level security policy` on a fresh connection and
`invalid input syntax for type uuid: ""` on a reused one, so enforcement
becomes indistinguishable from malformed input, nondeterministically.

**Only the dispatcher and the pruner use `WithAdmin` on this path**, both
allowlisted in `internal/archtest`. The dispatcher must: `WithSystem` sets no
tenant GUC, so under the new policy it would select zero rows on every tick —
and `drainOnce` returns no error on an empty result, so the entire event bus
would stop platform-wide with nothing in the log.
`TestDispatcherPublishesEveryTenant` pins that. `Prune` must, for the same
reason in reverse: a `DELETE` under `WithSystem` matches nothing and reports
success, which is why `TestPruneDeletesPublishedOutboxRows` asserts the row
count drops rather than that the call returned nil.

### Retention

`streamMaxAge` (24h) bounds how long a payload lives on the stream and how long
a *published* outbox row is kept. **Unpublished outbox rows are never pruned at
any age** — an old unpublished row is a dispatcher failure to investigate, not
garbage.

`processedRetention` is **derived** as `streamMaxAge + 24h`, never written as a
second number. `processed_events` is the idempotency ledger and JetStream
redelivers for `streamMaxAge`, so pruning the ledger sooner makes a redelivered
event indistinguishable from a new one: the consumer's `ON CONFLICT DO NOTHING`
claim finds no row, runs the handler again, and writes a **second dispense
record**. A duplicate clinical row produced by a cleanup job.
`TestProcessedRetentionOutlivesRedelivery` makes the invariant a CI failure
rather than a comment.

### Payload fields carrying PHI are pinned by hand

Every exported field of every contract payload must appear in one of two maps in
`internal/archtest/event_payload_test.go` — `eventPayloadPHIAllowlist` (with the
reason it must be on the wire) or `eventPayloadReviewedNonPHI` (with the reason
it is not PHI). A field in neither fails CI by name.

**There is no automatic pass, including for identifier-shaped names.** An
earlier version auto-classified anything ending in `ID` as an opaque row
pointer; it silently admitted `AadhaarID` and `ABHAID`. Aadhaar is sensitive
personal data under the DPDP Act and ABHA is the national health identifier —
being an identifier is an argument for scrutiny, not against it. Nor is
classification by Go type: a date of birth is as identifying as a `time.Time`
as it is as a `string`.

Payload minimisation is the rule, but **stripping PHI outright is not the
answer here**: consumers write `patient_name` into their own tenant-scoped,
RLS-forced tables and a pharmacist's queue displays it, and modules may not
import modules, so there is no cross-module read path to fetch it instead. The
destination is legitimate; it was the transport that was not.

## 7. Migrations

Every module's `Migrations()` returns `[]tenantdb.Migration{{ID, SQL}}`
with IDs of the form `NNNN_<module>` — a 4-digit sequence number plus the
owning module name, e.g. `0001_pharmacy`
(`backend/internal/modules/pharmacy/module.go`), `0001_events_outbox`
for the platform-owned events package
(`backend/pkg/events/bus.go`). `tenantdb.DB.Migrate`
(`backend/pkg/tenantdb/db.go`) applies each migration's SQL exactly once,
recording the ID in a `schema_migrations` table with
`INSERT ... ON CONFLICT DO NOTHING` — the same ID always resolves to the
same SQL, so migrations are **append-only**: never edit a migration that
has already shipped, only add a new one with the next sequence number.
Editing an applied migration's SQL changes nothing on any database that
already ran it (the `ON CONFLICT DO NOTHING` skips it), so the
in-repo SQL and the live schema silently diverge.

IDs must be globally unique across every module and the platform-owned
`events` migrations — two modules independently picking `0001_<name>`
is fine (the module name makes the full ID unique) but two migrations
reusing the exact same ID string is not.
`TestMigrationIDsAreGloballyUnique` in
`backend/internal/archtest/arch_test.go` collects every module's and
`events.Migrations()`'s IDs into one map and fails on the first
collision.

## 8. Logging

`slog` is the only logging package — `logrus` is a hard `depguard` error
repo-wide (`backend/.golangci.yml`'s `no-logrus` rule, not scoped to
modules like `module-isolation` is):

```yaml
no-logrus:
  deny:
    - pkg: "github.com/sirupsen/logrus"
      desc: "slog only (spec D6)"
```

Inside a request handler, get the **request-scoped** logger via
`requestid.Logger(c)` (`backend/internal/platform/requestid/requestid.go`)
rather than `slog.Default()` — it's the default logger pre-bound with
`request_id`, set once by `requestid.Middleware()`:

```go
func Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if id == "" || !validRequestID.MatchString(id) {
			id = uuid.NewString()
		}
		c.Set(Key, id)
		c.Set(loggerKey, slog.Default().With("request_id", id))
		c.Header("X-Request-ID", id)
		c.Next()
	}
}
```

`validRequestID` bounds an inbound `X-Request-ID` to `^[A-Za-z0-9_-]{1,128}$`
before it's echoed back or used in log correlation — an unbounded or
oddly-charactered client-supplied ID is replaced with a fresh UUID rather
than trusted.

Standard fields across the codebase: `request_id` (every request-scoped
log line, via `requestid.Logger`), `module` / `consumer` (which module or
consumer emitted the line — see the bus's
`slog.Error("consumer handle", "consumer", c.Name, "event_id", evt.ID, "tenant_id", evt.TenantID, "err", err)`
in `backend/pkg/events/bus.go`), `event_id` and `tenant_id` (any event
processing path), and `err` (always the wrapped error value, not
`err.Error()` — `slog`'s handler renders an `error` value correctly on
its own).

### Output, level and redaction

`pkg/logging.New(level)` (`NewWithWriter(w, level)` for tests) builds the
process logger: a JSON handler on stdout, writing through a **redacting
`io.Writer`**. `cmd/api` and `cmd/migrate` install it with
`slog.SetDefault` as their first act, before anything else logs.

`LOG_LEVEL` selects the threshold — `debug`, `info`, `warn`/`warning`,
`error`, case-insensitive. An unrecognised non-empty value degrades to
`info` with a warning and the process still boots; an empty value degrades
to `info` silently (the ordinary unset case, not a mistake). A mistyped log
level cannot compromise tenant isolation, and a hospital's API must not
fail to start over a typo — this is deliberately the opposite call from the
`HMS_ENV` guards, which fail closed because a wrong value there could
silently disable a safety check.

**Redaction happens at the writer, not the handler.** Every line `slog`'s
JSON handler emits passes through pattern redaction before it reaches
stdout — Aadhaar (12 digits), ABHA (14 digits), and Indian mobile numbers
(`+91` forms and bare 10-digit numbers beginning 5–9, including the
conventional 5-5 grouping). Matches are masked as `[REDACTED:aadhaar]`,
`[REDACTED:abha]`, `[REDACTED:mobile]`. A handler would have to predict how
`slog` renders each attribute value — `json.Marshaler` vs
`encoding.TextMarshaler` vs `error` vs `fmt.Stringer` — and every version of
that prediction leaked PHI in a different way during development. The
writer instead screens the bytes `slog` actually emits, so there's nothing
to predict. Full history and the failed handler-based approaches are in the
design spec — see
`docs/superpowers/specs/2026-08-13-structured-logging-phi-redaction-design.md`
rather than reproducing the tables here.

The writer **parses the line as JSON and re-encodes it**, rather than
scanning the raw bytes. Consequence worth relying on: a masked JSON number
comes out as a valid JSON string (never invalid syntax like a truncated
numeric literal), and a line with no PHI match is returned byte-identical.
Redaction cannot corrupt a line into invalid JSON.

`logging.RedactionCount()` returns an in-process counter of masks applied
— the seam #679 will wire to metrics; there's no metrics sink yet.

**Names, dates of birth and addresses are masked by struct tag, not by
pattern.** Tag a field `hmslog:"phi"` and `pkg/logging`'s handler replaces
its value with `[REDACTED:phi]` before the record is serialised:

```go
type Patient struct {
    ID   uuid.UUID `json:"id"`
    Name string    `json:"name" hmslog:"phi"`
    DOB  string    `json:"dob"  hmslog:"phi"`
}
```

The tag is honoured wherever the tagged type appears in a logged value —
directly, through a pointer, and inside slices, arrays and maps, including
when an untagged wrapper DTO holds them. The value is rendered by
`encoding/json` first and masked afterwards, so **the rendering itself** —
`json:"-"`, `omitempty`, renaming, embedding, `,string`, custom marshallers
— is exactly what it would be without this layer; nothing is reshaped, and
a `json:"-"` field cannot be published because it is never rendered.
What this layer computes independently is which emitted **key** each tagged
field corresponds to. Two things decide that, and both are handled without
guessing: **which tag names `encoding/json` honours** is answered by asking
`encoding/json` itself (it silently ignores some tag names — Go 1.26 rejects
`json:"नाम"` while accepting `json:"aé"` — so a rejected name renders under
the Go field name instead), and **which field wins when two compete for one
key** mirrors `encoding/json`'s own rule: shallowest embedding depth, then
the single tag-named field, then the key is dropped. Where that mirror
cannot be certain the position is masked rather than guessed. Anything that
cannot be rendered or walked (a reference cycle, an unmarshallable field, an
oversized graph) has its **whole** attribute value masked. This layer fails
closed throughout.

Both of those were learned the hard way: earlier versions guessed the
tag-name rule, and then hedged the guess by also claiming the Go field name,
and each leaked tagged PHI in plaintext for a different shape. Neither
mechanism remains. `phitag_conflict_test.go` fuzzes the class — generated
structs whose fields deliberately collide on emitted names, with tag names
`encoding/json` rejects in the pool — asserting in both directions that the
tagged value never appears and untagged values are never lost.

Three things it cannot see, by construction:

- PHI reached only through an `any`-typed element — `[]any`,
  `map[string]any`, or an `any`/interface-typed field — because the static
  type carries no tag. **Do not log PHI that way.**
- PHI inside a type that marshals itself (`json.Marshaler` /
  `encoding.TextMarshaler`), including the pointer-receiver case: this is
  about the *type*, not about how it was logged, so **any value reachable
  through such a type is affected** — `struct{ P *T }` where `*T` has its own
  `MarshalJSON` is as exposed as logging the `*T` directly. Tag the field
  that holds it instead; a tagged field of such a type is masked whole.
- PHI already flattened into a string before it reaches slog
  (`fmt.Errorf("%s", name)`), where no type remains to carry a tag.

A value with no `hmslog` tag anywhere in its type graph is left strictly
alone and renders byte-for-byte as it would without the handler.

The tag layer is defence in depth, not the primary control. What keeps
patient rows out of the log stream in the first place is the GORM guard
(`logger.Silent` in `pkg/tenantdb.Open`, enforced by
`TestGormOpenIsOnlyCalledFromTheAllowlist` and
`TestOpenNeverLogsQueryParameters`).

Known limitations, stated plainly:

- Redaction only sees what passes through `slog`. Anything a dependency
  writes straight to a file descriptor bypasses it entirely — precisely why
  the GORM logger needed its own guard rather than relying on this.
- `bounded()` makes hyphen-adjacent PHI a deliberate blind spot:
  `9876543210-9876543211` is not masked. This is the unavoidable other side
  of the fix that keeps a UUID like `tenant_id` from being torn apart and
  masked as an Aadhaar number. Do not widen the neighbour class without
  re-deriving that UUID case, or every `tenant_id` in every log line gets
  redacted and correlation breaks.
- False positives are expected — a legitimate 12-digit identifier that
  isn't PHI will still be masked — and that's the correct direction to
  fail.
- Redaction runs on every attribute of every emitted line. At current
  volumes that's not a concern; a future high-volume path (#438's
  read-access audit is the likely first candidate) should measure the cost
  rather than assume it's free.
- **No sampling guidance exists, deliberately.** No high-volume logging path
  exists in this codebase yet, so there is nothing to sample. Locally
  measured cost per line through `NewRedactingWriter` (`BenchmarkRedactingWriter*`
  in `backend/pkg/logging/redact_test.go`) is roughly 15–16µs for a realistic
  line carrying a timestamp (the common case, since almost every line clears
  the four-consecutive-digit prefilter on its `time` field alone) and drops to
  under 100ns for a line the prefilter can reject outright. #438's read-access
  audit is the likely first candidate for a genuinely high-volume path — when
  it lands, measure redaction's actual cost on that path before assuming it
  needs sampling, rather than adding sampling speculatively here.

**A free extra:** `slog.SetDefault` also redirects the standard library's
`log` package through the configured handler — `log.Printf` and friends route
through `slog`'s default handler once it is set, not just calls made directly
against `*slog.Logger`. A dependency that logs with stdlib `log` (e.g.
`log.Printf("patient %s not found", mobile)`) is therefore redacted for free,
the same as a direct `slog` call, and emits
`{"msg":"patient [REDACTED:mobile] not found", ...}`. This does **not** extend
to a dependency that builds its own `*log.Logger` pointed at a writer other
than `log.Default()`'s — gin's `gin.Recovery()` is exactly that case, and is
the reason `gin.DefaultErrorWriter` is wrapped explicitly in
`backend/cmd/api/main.go`'s `run()` rather than relied on to inherit the
redirection. Any other dependency that constructs its own writer the same way
gin does is an equivalent escape hatch and needs the same explicit treatment.

### Correlation fields

Every request line carries `request_id`, stamped by `requestid.Middleware`
before auth runs. Once `authn` has populated the context,
`requestid.PrincipalMiddleware` adds `tenant_id` and `subject`, so a line
answers which hospital, which user, which request. Neither is patient data:
`subject` is a pseudonymous Zitadel user id and `tenant_id` is a UUID.

That enrichment lives in `internal/platform/requestid`, not `pkg/authn`:
`internal/` may import `pkg/`, and the reverse is a dependency inversion the
foundation audit already flagged.

Add your own request-scoped fields with `requestid.Enrich(c, "module",
"medicore")` — it returns a new logger (`slog.Logger.With`) rather than
mutating a shared one, so it can't leak into another request.

Wrap errors with `%w`, not `%v` or string concatenation, whenever an
error crosses a function boundary and the caller might need
`errors.Is`/`errors.As` on it — `tenantdb.Open` is the pattern:

```go
app, err := open(appDSN)
if err != nil {
	return nil, fmt.Errorf("open app pool: %w", err)
}
```

`panic`/`log.Fatal` never appear outside `main.go` — a module or platform
package returns an `error` and lets the caller decide whether that's
fatal. `main.go`'s `run()` returns an `error` all the way up to `main()`,
which is the only place that turns a startup failure into a
`slog.Error` + `os.Exit(1)`.

## 9. Testing

`testutil.ModuleHarness(t, tokens, mods...)`
(`backend/internal/testutil/harness.go`) boots the full stack for a
module test package in one call — ephemeral Postgres, migrations for
every passed module plus the platform `events` migrations, an RLS lint
assertion, an ephemeral NATS/JetStream instance, a Gin router with the
`/v1` group and `authn.Middleware(StaticVerifier(tokens))` wired,
`Routes`/`Consumers` registered for every module, and the outbox
dispatcher running in the background:

```go
func setup(t *testing.T) (*gin.Engine, *tenantdb.DB, context.Context) {
	r, db, _, ctx := testutil.ModuleHarness(t,
		map[string]string{"tokA": testutil.TenantA, "tokB": testutil.TenantB},
		medicore.New())
	return r, db, ctx
}
```

(`backend/internal/modules/medicore/module_test.go`) — `testutil.TenantA`
/ `testutil.TenantB` are fixed UUID constants, and `StaticVerifier` maps
a bearer token string straight to a tenant ID so tests never mint a
real HMS session or call Zitadel. `testutil.Do(r, method, path, token, body)` issues an
authenticated JSON request against the harness router and returns the
`httptest.ResponseRecorder`.

Every module's test package covers, at minimum, the five cases the
generator's stub already demonstrates
(`backend/internal/modules/pharmacy/module_test.go` is the fullest
real example):

1. **CRUD happy path** — create then list, asserting the created row
   appears (`TestMedicationsCrud`).
2. **Tenant isolation** — the same list call under a second token does
   not see the first tenant's row (`require.NotContains` in
   `TestMedicationsCrud` / `TestVisitIntakeCreatesPendingDispense`).
3. **Cross-tenant 404** — a mutating call against another tenant's
   resource ID returns 404, not 403 (section 4):
   ```go
   require.Equal(t, http.StatusNotFound,
   	do(r, "POST", "/v1/pharmacy/dispenses/"+dispenseID+"/dispense", "tokB", `{"medication":"Paracetamol 500mg"}`).Code)
   ```
4. **Transition 409** — the guarded-UPDATE case: dispensing once
   succeeds, dispensing again 409s (`TestDispenseFlow`).
5. **Outbox assertion** — the published event actually landed and was
   marked published, via `require.Eventually` polling `outbox_events`
   directly (both `TestCreateAndListVisits` and `TestDispenseFlow`
   assert this by subject string).

**Arch tests** (`backend/internal/archtest/`) run once for the whole
repo, not per module: isolation (section 2), migration ID uniqueness
(section 7), consumer/subject naming (section 6), and the RLS lint
against every module's migrations applied together (section 3).

**Coverage gate:** `./scripts/coverage-gate.sh`
(`backend/scripts/coverage-gate.sh`) runs `go test -race -cover ./...`
and fails if any package under `internal/modules/*`, `pkg/*`,
`internal/platform`, or `internal/platform/*` reports below
`COVERAGE_FLOOR` (default 70%). It is not a repo-wide floor — packages
outside that list (e.g. `cmd/api`) are not gated.

Commands, all run from `backend/` unless using the `make` wrapper from
repo root:

```bash
make lint-go          # golangci-lint run ./...
make coverage-go       # ./scripts/coverage-gate.sh (70% floor)
go test -race ./...    # full suite, from backend/
```

## 10. Checklist for a new module

1. `make new-module NAME=<name>` from repo root.
2. Register `<name>.New()` in `backend/internal/bootstrap/modules.go`'s
   `Modules()` (shared by `cmd/api` and `cmd/migrate`).
3. Add `<name>.New()` to `allModules()` in
   `backend/internal/archtest/arch_test.go`.
4. Rename the generated `item` domain nouns (table, struct, subjects,
   routes) to your real ones.
5. Declare real permissions in `Permissions()` — the roles that hold
   each one your routes use (section 11) — and add them to
   `approvedPermissionMatrix` in
   `backend/internal/archtest/matrix_test.go`.
6. Keep the forced-RLS migration boilerplate (section 3) intact for
   every tenant table you add.
7. Every mutating handler starts with `authn.TenantPrincipal(c)`
   (section 5) and responds only through `respond.*` (section 4).
8. Guard every status transition with a conditional `UPDATE` +
   `RowsAffected` check, never read-then-write (section 4).
9. Publish events on the same `tx` as the row they describe, inside
   `WithTenant`/`WithSystem` (section 6).
10. Write the five required test cases against
    `testutil.ModuleHarness` (section 9).
11. `make lint-go`, `make coverage-go`, and `go test -race ./...` all
    green before calling it done.

## 11. Authorization

Every route declares an `authz.Permission` through `*platform.Router`
(`backend/internal/platform/router.go`), never a raw
`*gin.RouterGroup`. This is enforced at compile time, not just by
convention: `Router` wraps its `*gin.RouterGroup` in an unexported
field, so `Module.Routes(r *platform.Router, deps Deps)` has no way to
reach it — an undeclared route is inexpressible, not merely
disallowed. `TestModulesDoNotUseRawGinGroups`
(`backend/internal/archtest/arch_test.go`) backs this at the package
graph level, failing any module that imports `gin.RouterGroup`
directly. `authz.Public` (`backend/pkg/authz/authz.go`) is the explicit
opt-out for a deliberately unguarded route — a real value, not an
omitted argument, so it's greppable:

```go
g.GET("/me/permissions", authz.Public, func(c *gin.Context) { /* ... */ })
g.POST("/medications", PermMedicationWrite, func(c *gin.Context) { /* ... */ })
```

(`backend/internal/modules/iam/me.go`,
`backend/internal/modules/pharmacy/module.go`)

**Modules declare grants, never `tenant_admin`.**
`Module.Permissions() []authz.Grant` lists which system roles hold
each permission the module's routes use:

```go
func (m *Module) Permissions() []authz.Grant {
	return []authz.Grant{
		{Permission: PermDispenseRead, Roles: []authz.Role{authz.RolePharmacist, authz.RoleDoctor}},
		{Permission: PermDispenseFulfil, Roles: []authz.Role{authz.RolePharmacist}},
	}
}
```

(`backend/internal/modules/pharmacy/module.go`) `platform.GrantsFor`
(`backend/internal/platform/reconcile.go`) appends
`authz.RoleTenantAdmin` to every grant before writing tuples — a
module never lists it itself. `TestEveryDeclaredPermissionIsGranted`
(arch_test.go) fails if a route uses a permission `Permissions()`
doesn't declare.

**Naming**: `<module>.<resource>.<action>`, e.g.
`pharmacy.medication.write`, `lab.order.fulfil`.

**Roles are data.** The five system roles — `tenant_admin`, `doctor`,
`nurse`, `pharmacist`, `lab_tech` (`authz.Role` constants,
`backend/pkg/authz/authz.go`) — are seeded per tenant, and
permission/role bindings are OpenFGA tuples, not Go model relations.
Adding a zone, a permission, or changing who holds one is a
`Permissions()` edit plus a reconcile, never a schema migration.

**Status codes fail closed.** `403` means the caller is a member of
the tenant but lacks the permission; `404` is still the cross-tenant
answer (section 4 — RLS means the row was never visible to the query
in the first place, so authorization never gets a chance to
distinguish "missing" from "someone else's"); an OpenFGA error is
`503 authz_unavailable`, never a silent allow.

**Membership and grant writes are eventually consistent.** The
membership/role-grant endpoints publish through the outbox inside the
same transaction as the row they describe (section 6), so they return
`202`, not `200`/`201` — the FGA tuple lands once the dispatcher drains
the outbox. Re-granting an already-held role is allowed and publishes
unconditionally on purpose: it's the operator's repair mechanism for
FGA drift, not just the first-grant path.

**The reconciler makes OpenFGA rebuildable from Postgres, and prunes
what Postgres no longer backs.** Two functions in
`backend/internal/platform/reconcile.go`, two different jobs:

- **`platform.Reconcile(ctx, reg, db, w)`** — boot-path, authoritative.
  Runs once at boot (`cmd/api/main.go`), reads every tenant's
  `iam_members` rows in one pass using `WithAdmin` (section 3), writes
  every role/permission tuple Postgres backs, and **deletes** every
  `role:`/`perm:` tuple Postgres does not back. Deletion is scoped per
  tenant: a tuple is only ever considered for deletion from the tenant
  bucket it was read into, never across tenants. This is what makes
  "OpenFGA is fully rebuildable from Postgres" true — both locally (the
  dev OpenFGA store is in-memory and loses every tuple on restart) and
  as the backstop for a `member_revoked` event that was lost before
  `iam-fga-sync` applied it. As a safety floor, `Reconcile` refuses to
  write or delete anything, and returns an error instead, if the
  `iam_members` read comes back with zero *usable* rows — rows whose
  `role_key` resolves to a known `authz.Role`, since a row with an
  unrecognized `role_key` is skipped and contributes nothing to the
  desired tuple set — across every tenant while OpenFGA still holds
  tuples. That shape covers both a misconfigured `ADMIN_DATABASE_URL` (a
  role that does not bypass RLS, so the read comes back with truly zero
  rows) and a corrupted-but-nonempty table (e.g. an un-migrated role-key
  rename, or manual/seed drift, leaving rows present but all
  unrecognized) — neither is a real everyone-was-revoked event. A tripped
  guard returns an error from `run()`, which `main.go` treats as fatal
  (`os.Exit(1)`): the process crash-loops rather than boot with
  authorization data it cannot trust. That is deliberate — a boot that
  silently reconciled against corrupted or misread membership data would
  be worse than one that refuses to start.

  One consequence of running at boot on every replica: during a mixed-
  version deploy or rollback, an old-version replica that boots *after*
  a deploy added or renamed a permission will run `Reconcile` against a
  registry that does not yet know about it, and will delete that
  permission's `perm:` tuples for every tenant — including tenants a
  still-running new-version replica already reconciled. This converges
  once every replica is on the new version (the next new-version boot,
  or the next scheduled reconcile, re-adds them), but it opens a window
  during the mixed-version boot sequence where access gated on the new
  permission is denied rather than granted. Treat that window as
  expected churn during a rollout, not a bug — it self-heals as soon as
  the old replica is gone.
- **`platform.ReconcileTenant(ctx, reg, w, tenantID)`** — grant-path,
  additive only. Called inside `iam`'s own transaction on every
  membership grant (`internal/modules/iam/sync.go`), and writes only
  that tenant's permission tuples. It never reads Postgres and never
  deletes anything, so permission sets reached through this path can
  only grow: narrowing a role's declared permissions in code only takes
  effect for a tenant the next time `Reconcile` runs.

**Revocation self-heals at boot, not continuously.** The normal path
(`member_revoked` event → outbox → `iam-fga-sync`) converges within the
usual outbox lag. If that event is permanently lost, the resulting stale
grant is not self-healing while the process keeps running — there is no
periodic reconcile ticker or cron — it persists until the next time that
specific process boots and `Reconcile` runs. Treat `Reconcile` as a
boot-time correctness backstop for lost revokes, not a live revocation
mechanism.

**The adversarial matrix suite**
(`backend/internal/archtest/matrix_test.go`) is the phase gate for any
change to a module's `Permissions()`:

- `TestPermissionMatrix` — every role × every declared permission,
  asserted allow/deny against a real OpenFGA, in both tenants.
- `TestCrossTenantDenial` — a fully-privileged `tenant_admin` in one
  tenant holds nothing in another.
- `TestEveryGuardedRouteIsCoveredByTheMatrix` — a route guarded by a
  permission that only `tenant_admin` holds is flagged as a likely
  declaration bug (unreachable for every real role).
- `TestDeclaredPermissionsMatchTheApprovedMatrix` — checks
  `platform.GrantsFor` against `approvedPermissionMatrix`, a table
  hand-transcribed from the design spec, independent of the code it
  checks. **This is deliberate, not friction to work around**: it
  means changing any module's `Permissions()` fails CI until someone
  edits `approvedPermissionMatrix` by hand, forcing a reviewable,
  intentional edit every time a role↔permission mapping changes rather
  than silently trusting whatever the code currently does.

`TestMainRegistersExactlyAllModules` (arch_test.go) rounds this out by
failing if `bootstrap.Modules()` and `allModules()` disagree on the
module set — see section 1's two-places rule.

**Tenant switching — production prerequisites.** `POST /v1/iam/me/tenant`
(`backend/internal/modules/iam/me.go`) verifies the caller's current HMS
session, checks membership of the target tenant in OpenFGA, and re-mints
the session with `session.Signer.Mint` (`backend/pkg/session/signer.go`)
— the SAME Ed25519 signer `cmd/api/main.go` builds for login, carried in
via `platform.Deps.SessionSigner`. There is no external identity provider
in this path as of #838 (spec D3): the only production prerequisite is
the session signing key itself (`SESSION_SIGNING_KEY`), which the process
already refuses to boot without (§5 above) — there is no separate
signing-credential grant to check, unlike the GIP-backed design this
replaced. A misconfigured signer reaching the route as `nil` (a
half-wired entrypoint) fails closed as `503 session_unavailable`, mirroring
the shape of the old failure mode but for a different, entirely local
cause.
