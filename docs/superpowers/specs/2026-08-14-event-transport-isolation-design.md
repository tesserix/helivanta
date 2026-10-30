# Event transport isolation and retention — design

**Issue:** [#835](https://github.com/tesserix/helivanta/issues/835) — PHI leaves the RLS
boundary in event payloads, and nothing ever prunes the outbox.
**Status:** draft 2026-08-14.
**Related:** #774 (Tier 2, the finding), #827/#829 (contract packages, merged),
#685 (the events package), #480/#65/#69 (clinical retention — different
concern), ADR-0005.

---

## Problem

Three facts, each re-verified against `main` at `0c02bc6` rather than inherited
from the audit:

1. **`medicore.contract.VisitCreatedData` carries `patient_name`.** It is
   written to `outbox_events` and published to JetStream.
2. **`outbox_events` has no `tenant_id`**, therefore no RLS policy, and sits on
   `lintAllowlist` in `pkg/tenantdb/db.go:186`. The code already records the
   problem against itself: *"here under protest: it holds event payloads that
   today include patient names, outside any RLS boundary, readable by any
   `WithSystem` transaction."*
3. **Nothing ever deletes `outbox_events` or `processed_events`.** No
   `DELETE FROM` for either exists anywhere in the tree.

The consequence is not that PHI reaches pharmacy — that part is correct and
must keep working. It is that PHI sits **forever**, in a table with no access
control, on a shared `db-f1-micro`, readable by any `WithSystem` transaction in
any module.

### What this is *not*, and why the original framing was wrong

#774 proposed "stop putting identifiers in payloads, let consumers read what
they are entitled to". **That does not hold in this codebase**, and #835 has
been corrected accordingly:

- `pharmacy-visit-intake` and `lab-order-intake` write `patient_name` into
  `pharmacy_dispenses` / `lab_orders` — both tenant-scoped and RLS-forced. A
  pharmacist's dispense queue displays it. That destination is legitimate.
- Modules may not import modules; data flows via events only. There is **no
  cross-module read path** an ID-only event could fall back on. Stripping the
  field would leave pharmacy structurally unable to show a patient name.

**The destination is fine. The transport is the defect.** This spec protects and
bounds the transport, and does not change what consumers end up holding.

---

## Decisions

### D1 — `outbox_events` gets `tenant_id` and forced RLS; the dispatcher moves to `WithAdmin`

This is the whole change, and its hazard is the reason it needs a design rather
than a migration.

`drainOnce` reads the outbox **across every tenant** by design:

```go
return db.WithSystem(ctx, func(tx *gorm.DB) error {
    tx.Raw(`SELECT id, subject, payload FROM outbox_events
            WHERE published_at IS NULL ORDER BY created_at LIMIT 100
            FOR UPDATE SKIP LOCKED`)
```

`WithSystem` runs on the app pool **with no tenant GUC** (`db.go:160`). So
adding a policy of the usual shape — `tenant_id = current_setting('app.tenant_id',
true)::uuid` — makes `current_setting` return NULL, the predicate match nothing,
and the dispatcher select **zero rows on every tick**.

**That failure is silent.** `drainOnce` returns no error on an empty result and
logs nothing; the outbox simply fills while every consumer starves. A migration
that "adds RLS to the outbox" and is merged on a green suite would take the
entire event bus down in production with no error anywhere.

**Decision: the dispatcher moves from `WithSystem` to `WithAdmin`**, whose
documented purpose is exactly this — *"a whole-system read that genuinely needs
to cross tenant boundaries"*, on the admin pool, which bypasses RLS as the
migration role. `OutboxStore` gains `WithAdmin` and the dispatcher stops being
able to compile against `WithSystem` alone, so the two halves of this change
cannot land apart.

What that buys, precisely: **every other access path is scoped.** A module
handler running under `WithSystem` — the exact reader the allowlist comment
warns about — can no longer read another tenant's payloads. The dispatcher, a
single background loop already reviewed as ops code, is the one caller with
cross-tenant reach, and it becomes a visible, allowlisted decision instead of
an ambient property of the table.

*Rejected: a GUC escape in the policy* (`OR current_setting('app.dispatcher',
true) = 'on'`). Any `WithSystem` caller could set that GUC in its own session,
so the policy would be advisory. A bypass that the thing being bypassed can
enable is not a control.

*Rejected: leave the outbox unprotected and shorten retention only.* Retention
bounds *how long* every module can read every tenant's PHI. It does not stop it.

*Rejected: encrypt the payload column.* Bounds exposure without changing
contracts, but there is no key management story — #45 (secrets) is still open —
and it leaves unbounded growth untouched. Revisit if #45 lands with a KMS.

### D1a — `tenant_id` is nullable, and the policy is deliberately asymmetric

The obvious migration — backfill from `payload->>'tenant_id'`, then `SET NOT
NULL` — **would break sign-out.** `SubjectCredentialRevoked` is published with
no `TenantID` at all, and that is a documented decision, not an oversight
(`internal/modules/iam/signout.go:106-114`): revocation is subject-scoped and
ends every session for a subject *in every tenant*, so there is no tenant to
name. Broadcasts travel the same `Publish` → outbox → dispatcher path as
module events, so the outbox legitimately holds tenant-less rows.

**Decision: `tenant_id uuid` NULL-able, with an asymmetric policy.**

```sql
CREATE POLICY tenant_isolation ON outbox_events
  USING (hms_tenant_visible(tenant_id))
  WITH CHECK (tenant_id IS NULL
              OR tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
```

> **`NULLIF` added during Task 1**, and it is not cosmetic. An "unset" custom
> GUC returns SQL NULL only the *first* time a session ever references the
> name; after any `WithTenant` transaction commits, it reverts to `''` for the
> life of that pooled connection. `''::uuid` is a hard type error, so without
> `NULLIF` the identical cross-tenant write reported **two different errors
> depending on pool scheduling** — `new row violates row-level security policy`
> on a fresh connection, `invalid input syntax for type uuid: ""` on a reused
> one. A security control whose rejection is indistinguishable from malformed
> input, nondeterministically, is not a control anyone can reason about.
> `USING` is safe because `hms_tenant_visible` carries the same `NULLIF`
> (migration `0002_platform_rls`); `WITH CHECK` cannot call that function — the
> lint forbids it — so it needs its own.

This is the house form (`medicore/module.go:52-61`) with exactly one deviation,
and both halves are constrained by `LintRLS`, which was read rather than
assumed:

- **`USING` calls `hms_tenant_visible`** — the lint fails a tenant table whose
  `USING` does not (`db.go:313`). It also deliberately omits the NULL case:
  `NULL = anything` is NULL, not true, so a tenant-less row is readable by
  **nobody** under RLS — strictly more protected than a tenant row, and
  correct, since a platform-wide event is not any single tenant's to read. The
  dispatcher still sees it, via `WithAdmin`.
- **`WITH CHECK` must *not* call `hms_tenant_visible`.** The lint fails that
  case explicitly — *"WITH CHECK calls hms_tenant_visible instead of pinning to
  one tenant"* (`db.go:315`) — because reads may widen to hospital groups later
  while writes must always land in exactly one tenant.
- **`WITH CHECK` must permit NULL**, or publishing a tenant-less event from
  inside a tenant-scoped transaction — exactly what `signOut` does — is
  rejected by the policy and sign-out fails. This is the clause whose omission
  would pass every existing test and break credential revocation in
  production.

The `tenant_id IS NULL` disjunct keeps `with_check` clear of the function name,
so this policy satisfies the lint rather than needing an exception to it.

`Publish` sets the column from `evt.TenantID` (parsed as a UUID, NULL when
absent or unparseable) rather than leaving it to callers, so the column and the
envelope cannot disagree.

**`LintRLS` accepts a nullable `tenant_id`** — confirmed by reading it: the
`has_tenant` clause tests column existence in `information_schema.columns` only
and never examines nullability (`db.go:233-237`). No lint change is needed.

### D2 — `processed_events` keeps no `tenant_id`, and says why

Not every event carries a tenant: `handle` only sets the GUC when
`uuid.Parse(evt.TenantID)` succeeds (`bus.go:384`), so broadcasts have none. A
`tenant_id NOT NULL` on the ledger would break them, and a nullable one gives a
policy nothing to enforce.

It also **holds no PHI** — consumer name, event id, timestamp. That is the same
argument `iam_credential_revocations` already makes on the allowlist.

**Decision: `processed_events` stays allowlisted, with its reason written out**
the way the revocation table's is, rather than inheriting the outbox's
"under protest" comment. It needs pruning (D3), not a policy.

This distinction matters: blanket-applying `tenant_id` to both tables because
both are on one allowlist would break broadcast dedup to solve a problem the
ledger does not have.

### D3 — Retention, with the coupling enforced rather than documented

Two windows, and **they are not independent**:

- **`outbox_events`**: rows with `published_at IS NOT NULL` older than the
  window are deleted. Unpublished rows are never pruned regardless of age — an
  old unpublished row is a dispatcher failure to investigate, not garbage.
- **`processed_events`**: the idempotency ledger. JetStream redelivers for
  `MaxAge`. **Pruning the ledger on a window shorter than `MaxAge` reintroduces
  duplicate processing**: a redelivered event whose ledger row was cleaned up is
  indistinguishable from a new one, and the consumer inserts a second
  `pharmacy_dispenses` row — a duplicate clinical record produced by a cleanup
  job.

**Decision: derive the ledger window from the stream's `MaxAge` in code**, not
by writing two numbers that happen to be in the right order:

```go
const streamMaxAge = 24 * time.Hour
// Strictly greater than streamMaxAge: see above.
const processedRetention = streamMaxAge + 24*time.Hour
```

with a test asserting `processedRetention > streamMaxAge`. Per §4, a developer
tidying the retention config cannot silently invert the relationship — the
constant is derived and the assertion is a CI failure, not a comment.

**`MaxAge` drops from 7 days to 24 hours.** Seven days of PHI on a stream buys
nothing operationally: with `maxDeliver = 5` and `ackWait = 30s`
(`bus.go:32-42`), a failing message exhausts redelivery and routes to the DLQ in
**about two and a half minutes**. Everything after that is retained PHI, not
recoverability. 24 hours still spans an overnight outage by a wide margin.

Note the DLQ subject (`<root>.dlq.<consumer>`) is inside the same stream, so it
inherits `MaxAge` — a dead-lettered clinical event is bounded by the same
window rather than kept indefinitely.

*Rejected: prune inside the dispatcher tick.* Couples deletion latency to
publish throughput and puts a `DELETE` on the hot path every 500ms.

*Rejected: a `pg_cron` job.* Another moving part, and it lives outside the Go
tests, so the retention rule stops being provable in CI.

### D4 — Payload minimisation as a pinned allowlist, not a denylist

`patient_name` is legitimately on the wire (see Problem). A CI rule that fails
on PHI-shaped field names would fail on the field the product needs.

**Decision: pin the set of PHI-carrying payload fields to an allowlist with a
reason each**, asserted by an arch test over the `contract` packages — the same
shape as the rate-limit exemptions (#689) and the `NoTenantMembership`
allowlist (#781). Adding a clinical field to an event payload becomes a diff a
reviewer sees and must justify, rather than a thing that happens.

*Rejected: a name-pattern denylist.* Guesses at field names, fails on the
legitimate case, and misses `notes` or `reason_for_visit`.

---

## Architecture

```
pkg/events
  Migrations()        + 0002_outbox_tenant_scope  (tenant_id, policy, backfill)
  OutboxStore         + WithAdmin  ← dispatcher can no longer compile without it
  drainOnce           WithSystem → WithAdmin
  streamMaxAge        24h, was 7 days
  processedRetention  derived: streamMaxAge + 24h
  Prune(ctx, db)      deletes published outbox rows and expired ledger rows

pkg/tenantdb
  lintAllowlist       outbox_events REMOVED; processed_events keeps its slot,
                      with its own reason
```

Migration `0002_events_outbox_tenant_scope` is append-only. It adds the nullable
column, backfills from the envelope already stored in `payload`
(`(payload->>'tenant_id')::uuid`, which `Publish` has always written), then adds
the policy and `FORCE ROW LEVEL SECURITY`. There is no `SET NOT NULL` step — see
D1a. Rows whose envelope carries no tenant stay NULL, which is the correct and
strictest outcome rather than a gap.

The backfill must not assume a small table: the outbox has never been pruned, so
on a long-lived environment it is the largest table in the schema. The plan
should state whether the backfill is batched.

---

## Testing

Every assertion must be observed failing (§5). The ones that carry the design:

| Claim | Test | Mutation that must break it |
|---|---|---|
| The dispatcher still publishes across tenants | publish for two tenants, assert both reach the stream | revert dispatcher to `WithSystem` → **zero** published, silently |
| Sign-out still publishes from inside a tenant tx | call `signOut`, assert the revocation row reaches the outbox and the stream | drop `tenant_id IS NULL` from `WITH CHECK` → insert rejected, sign-out 500s |
| A tenant-less row is readable by nobody | `WithSystem` and `WithTenant` selects of the revocation row | add the NULL case to `USING` → row returned |
| A module handler cannot read another tenant's outbox | `WithSystem` select of tenant B's row from tenant A's context | drop the policy → row returned |
| Ledger outlives redelivery | assert `processedRetention > streamMaxAge` | invert the constants |
| Pruning cannot resurrect a duplicate | prune, redeliver a still-live event, assert no second row | prune the ledger on the stream window |
| Unpublished rows survive pruning | age an unpublished row past the window, prune, assert present | drop the `published_at IS NOT NULL` guard |
| PHI fields are pinned | add a field to a contract payload | not in the allowlist → arch test fails |

The first is the one this design rests on: **it must be observed failing with
zero published events**, or the test proves nothing about the hazard that
motivated D1.

---

## Limitations

- **Consumers still hold PHI** in their own tables. That is correct and out of
  scope; their tables are tenant-scoped and RLS-forced.
- **The dispatcher bypasses RLS.** D1 narrows cross-tenant reach from "any
  `WithSystem` caller" to "one allowlisted background loop", which is a
  reduction, not an elimination. Nothing in the type system stops a future
  caller adding itself to the allowlist.
- **JetStream payloads are still unencrypted at rest** for 24 hours. Bounded,
  not removed; revisit with #45.
- **The PHI allowlist is CI-guarded convention**, which §4 ranks below
  "impossible to express".
- **Retention windows are guesses.** No production traffic; 24h/48h are chosen
  to span an overnight outage, not calibrated.

---

## Out of scope

- Clinical-record retention and DPDP erasure workflows — #480, #65, #69.
- The event catalogue as a product artefact — #485.
- Encryption at rest — separate #774 item, blocked on #45.
- Changing what consumers store.
