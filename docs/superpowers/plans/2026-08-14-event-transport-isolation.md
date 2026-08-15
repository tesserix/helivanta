# Event transport isolation and retention — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: use superpowers:subagent-driven-development
> to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Stop patient names sitting forever in a table with no access control,
without breaking the dispatcher, sign-out, or at-least-once delivery.

**Spec:** `docs/superpowers/specs/2026-08-14-event-transport-isolation-design.md`
**Issue:** #835. Branch: `feat/835-event-transport-isolation`.

## Global constraints

- `docs/standards/engineering-principles.md` is binding. Especially §3 (this is
  a **data/isolation** control, so it fails **closed** — the opposite of #689's
  rate limiter), §4 (compile error > boot failure > CI failure > convention),
  §5 (**every new assertion must be observed failing before it passes**).
- `make lint-go` clean; `cd backend && go test -count=1 -race ./...` green;
  `cd backend && ./scripts/coverage-gate.sh` green — **run it unpiped**.
- Docker must be running (testcontainers). Always run from the repo root.
- Migrations are **append-only**. Never edit `0001_events_outbox`.

## The three traps this plan exists to navigate

Each has been verified in the code, and each fails **silently**:

1. **RLS on the outbox blinds the dispatcher.** `drainOnce` uses `WithSystem`,
   which sets no tenant GUC (`tenantdb/db.go:160`), so a normal policy makes it
   select zero rows. `drainOnce` returns no error on an empty result — the bus
   stops platform-wide with nothing in the log.
2. **A symmetric `WITH CHECK` breaks sign-out.** `SubjectCredentialRevoked`
   publishes with no `TenantID` by design (`iam/signout.go:106-114`). If the
   policy's `WITH CHECK` does not permit NULL, that insert is rejected inside
   the sign-out transaction.
3. **A prune under `WithSystem` deletes nothing.** Once the outbox has RLS and
   the GUC is unset, `DELETE` matches no rows and reports success.

---

## Task 1: bring the outbox inside the boundary

One task, one commit: the migration and the dispatcher change cannot land apart
without breaking the bus.

**Files:** `backend/pkg/events/bus.go`, `backend/pkg/events/outbox_rls_test.go` (new),
`backend/pkg/tenantdb/db.go`

- [ ] **Step 1: make `WithAdmin` reachable, as a compile-time forcing function**

Add `WithAdmin(ctx context.Context, fn func(tx *gorm.DB) error) error` to
`OutboxStore`. `*tenantdb.DB` already implements it. This is deliberate: the
dispatcher cannot be switched to `WithAdmin` without the interface change, and
the interface change makes every test double declare it, so no caller silently
keeps the old path.

- [ ] **Step 2: write the failing tests**

In a new `backend/pkg/events/outbox_rls_test.go`, against the real Postgres
testcontainer:

1. `TestDispatcherPublishesEveryTenant` — publish one event for tenant A and one
   for tenant B, run one drain, assert **both** reach the stream. This is trap 1.
2. `TestTenantlessEventStillPublishes` — publish an event with `TenantID: ""`
   from inside a transaction that HAS a tenant GUC set (mirroring `signOut`),
   assert the insert succeeds and the event reaches the stream. This is trap 2.
3. `TestOutboxRowIsInvisibleToAnotherTenant` — write a row for tenant A, then
   under `WithTenant(B)` select it, assert zero rows.
4. `TestOutboxRowIsInvisibleUnderWithSystem` — the reader the allowlist comment
   warned about: under `WithSystem`, assert a tenant row is **not** readable.
5. `TestTenantlessRowIsReadableByNobody` — under `WithSystem` and under
   `WithTenant(A)`, assert the NULL-tenant row is not returned.

- [ ] **Step 3: run and confirm failure**

```bash
cd /Users/Mahesh.Sangawar/personal/tesserix-new/hms/backend && go test ./pkg/events/ -run 'TestDispatcherPublishesEveryTenant|TestOutbox|TestTenantless' -v
```

Tests 3–5 must fail *now* (no policy exists, so everything is readable). If any
of them passes before the migration, it is asserting nothing — say so.

- [ ] **Step 4: add migration `0002_events_outbox_tenant`**

Appended to `Migrations()` in `pkg/events/bus.go`, never editing `0001`:

```sql
ALTER TABLE outbox_events ADD COLUMN tenant_id uuid;

-- Backfill from the envelope Publish has always written into payload.
-- The regex guard matters: rows for tenant-less events carry "" and a bare
-- ::uuid cast would abort the whole migration on the first one.
UPDATE outbox_events
   SET tenant_id = (payload->>'tenant_id')::uuid
 WHERE payload->>'tenant_id' ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$';

ALTER TABLE outbox_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE outbox_events FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation ON outbox_events
  USING (hms_tenant_visible(tenant_id))
  WITH CHECK (tenant_id IS NULL
              OR tenant_id = current_setting('app.tenant_id', true)::uuid);

CREATE INDEX ON outbox_events (tenant_id);
```

**No `SET NOT NULL`** — see spec D1a. The `WITH CHECK` must not reference
`hms_tenant_visible`: `LintRLS` fails that explicitly (`db.go:315`), because
reads may widen while writes stay pinned.

- [ ] **Step 5: set the column in `Publish`**

In `Publish`, parse `evt.TenantID` as a UUID and set `outboxRow.TenantID` to it,
or leave NULL when absent/unparseable. Derive it from the envelope rather than a
new parameter, so the column and the payload cannot disagree.

- [ ] **Step 6: move the dispatcher to `WithAdmin`**

`drainOnce`: `db.WithSystem(...)` → `db.WithAdmin(...)`, with a comment naming
trap 1 and pointing at `TestDispatcherPublishesEveryTenant`.

- [ ] **Step 7: remove `outbox_events` from `lintAllowlist`**

Delete the entry and its "under protest" comment in `pkg/tenantdb/db.go`.
**Rewrite `processed_events`'s entry with its own reason** (spec D2): it carries
no tenant because broadcasts have none, and holds no PHI — consumer, event id,
timestamp — the same argument `iam_credential_revocations` makes. Do not leave it
inheriting the outbox's comment.

- [ ] **Step 8: run, then prove each assertion can fail**

| Mutation | Must break |
|---|---|
| revert `drainOnce` to `WithSystem` | `TestDispatcherPublishesEveryTenant` — **zero** published |
| drop `tenant_id IS NULL` from `WITH CHECK` | `TestTenantlessEventStillPublishes` |
| add `OR tenant_id IS NULL` to `USING` | `TestTenantlessRowIsReadableByNobody` |
| drop the policy | tests 3 and 4 |

Report each with its real output. The first is the one the design rests on:
**it must be observed publishing zero events**, or it proves nothing about
trap 1.

- [ ] **Step 9: confirm the lint now covers the outbox**

`LintRLS` must return no complaint about `outbox_events` (it is no longer
allowlisted, so a wrong policy shape now fails). Prove it: temporarily change
`WITH CHECK` to call `hms_tenant_visible` and confirm the lint reports
*"WITH CHECK calls hms_tenant_visible instead of pinning to one tenant"*.
Restore.

- [ ] **Step 10: gates and commit**

```
feat: bring the outbox inside the RLS boundary, with the dispatcher explicitly privileged (#835)
```

---

## Task 2: bound the lifetime of both tables

**Files:** `backend/pkg/events/bus.go`, `backend/pkg/events/retention.go` (new),
`backend/pkg/events/retention_test.go` (new), `backend/cmd/api/main.go`

- [ ] **Step 1: constants, with the coupling in code**

In `pkg/events`:

```go
// streamMaxAge bounds how long a payload — patient names included — lives on
// the stream. Was 7 days; with maxDeliver=5 and ackWait=30s a failing message
// reaches the DLQ in ~2.5 minutes, so everything past that was retained PHI
// rather than recoverability.
const streamMaxAge = 24 * time.Hour

// processedRetention MUST exceed streamMaxAge. processed_events is the
// idempotency ledger: JetStream redelivers for streamMaxAge, so pruning the
// ledger sooner makes a redelivered event indistinguishable from a new one and
// the consumer inserts a SECOND pharmacy_dispenses row — a duplicate clinical
// record produced by a cleanup job. Derived, not written as a second number,
// so the two cannot drift.
const processedRetention = streamMaxAge + 24*time.Hour
```

Change the stream config's `MaxAge` to `streamMaxAge`.

- [ ] **Step 2: write the failing tests**

1. `TestProcessedRetentionOutlivesRedelivery` — `require.Greater(processedRetention, streamMaxAge)`.
2. `TestPruneDeletesPublishedOutboxRows` — insert a published row aged past the
   window, prune, assert **gone**; assert the count actually dropped rather than
   asserting the call returned nil.
3. `TestPruneKeepsUnpublishedRows` — an *unpublished* row aged past the window
   must survive. An old unpublished row is a dispatcher failure to investigate,
   not garbage.
4. `TestPruneKeepsLedgerWithinRedeliveryWindow` — a ledger row younger than
   `processedRetention` survives.
5. `TestPruneCannotResurrectADuplicate` — the load-bearing one: process an
   event, prune, redeliver the same event id, assert the handler does **not**
   run twice and no second row appears.

- [ ] **Step 3: implement `Prune`**

```go
// Prune runs on WithAdmin, not WithSystem. Once Task 1's policy is on
// outbox_events, a DELETE under WithSystem matches zero rows and reports
// success — a prune that silently never prunes (trap 3).
func (b *Bus) Prune(ctx context.Context, db OutboxStore) error
```

Delete `outbox_events` where `published_at IS NOT NULL AND published_at < now() - interval`,
and `processed_events` where `processed_at < now() - interval`. Return the counts
so the caller can log them; a prune whose effect nobody can observe is the same
blind spot as an unlogged exemption.

- [ ] **Step 4: wire it in `main.go`**

A separate goroutine with its own ticker (hourly), beside `RunDispatcher` — not
inside the dispatcher tick, which would put a `DELETE` on the publish path every
500ms (spec D3). Wrap in the same panic-containment shape as `drainSafely`, and
log the deleted counts.

- [ ] **Step 5: prove the assertions can fail**

| Mutation | Must break |
|---|---|
| `processedRetention = streamMaxAge - time.Hour` | `TestProcessedRetentionOutlivesRedelivery` |
| prune the ledger on `streamMaxAge` instead | `TestPruneCannotResurrectADuplicate` |
| drop `published_at IS NOT NULL` | `TestPruneKeepsUnpublishedRows` |
| `Prune` uses `WithSystem` | `TestPruneDeletesPublishedOutboxRows` — deletes nothing, silently |

The last is trap 3, and it is the reason test 2 asserts on the row count rather
than on the error being nil.

- [ ] **Step 6: gates and commit**

```
test: bound outbox and ledger retention, with the ledger provably outliving redelivery (#835)
```

---

## Task 3: pin the PHI-carrying payload fields

**Files:** `backend/internal/archtest/event_payload_test.go` (new)

- [ ] **Step 1: the allowlist and the test**

An allowlist of `"<package>.<Type>.<Field>"` → reason, covering every field in a
`contract` package that carries clinical or identifying data. Today exactly one:
`medicore.VisitCreatedData.PatientName` — "pharmacy and lab display it on their
work queues; there is no cross-module read path to fetch it instead".

The test reflects over the contract packages' payload structs and requires every
field either to be in the allowlist or to be recognisably non-PHI
(ids, enums, timestamps). Prefer a **positive** allowlist over a name denylist:
a denylist guesses at names, fails on the legitimate field, and misses `notes`
or `reason_for_visit` (spec D4).

- [ ] **Step 2: prove it can fail**

Add `PatientPhone string` to a contract payload. The test must fail naming it.
Restore.

- [ ] **Step 3: commit**

```
test: pin which event payload fields may carry PHI, so adding one is a visible decision (#835)
```

---

## Task 4: the standards entry

**Files:** `docs/standards/backend.md`

- [ ] Document, in the events section: the outbox is inside the RLS boundary;
  the dispatcher and pruner are the only `WithAdmin` callers on this path and
  why; `WITH CHECK` permits NULL only for genuinely tenant-less events;
  retention windows are derived and why the ledger must outlive the stream; and
  that adding a PHI field to a payload requires an allowlist entry.

- [ ] Commit: `docs: record the event transport isolation and retention rules (#835)`

---

## Task 5: verification and PR

- [ ] Full gates: `go build ./... && go vet ./... && go test -count=1 -race ./...`;
  `./scripts/coverage-gate.sh` unpiped; `make lint-go`;
  `pnpm turbo lint type-check test build`; the Playwright suite twice.
- [ ] **Verify by hand on the running stack**, not only in tests: create a visit,
  then confirm (a) the row reaches `outbox_events` with the right `tenant_id`,
  (b) a `WithSystem`-shaped query cannot read it, (c) sign-out still works and
  its tenant-less row is published. Assert on the rows actually produced.
- [ ] PR body carries: the three traps and the test pinning each; why the
  dispatcher is privileged and what that does *not* fix; the nullable-tenant
  decision and the sign-out case that forced it; the retention coupling; which
  assertions were observed failing. `Closes #835`.

---

## Known limitations (carry into the PR)

- **The dispatcher and pruner bypass RLS.** This narrows cross-tenant reach from
  "any `WithSystem` caller in any module" to "two allowlisted background loops".
  A reduction, not an elimination.
- **Consumers still hold PHI** in their own tenant-scoped tables. Correct, and
  out of scope.
- **JetStream payloads are unencrypted at rest** for 24 hours. Bounded, not
  removed; revisit with #45.
- **The PHI allowlist is CI-guarded convention**, which §4 ranks below
  "impossible to express".
- **Retention windows are guesses** — 24h/48h span an overnight outage; no
  production traffic to calibrate against.
- **No backfill batching** unless Task 1 finds the table large enough to need it.
