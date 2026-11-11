# Hospital names reach pharmacy after a dead-lettered delivery — design

- **Issue:** #1653 (follow-up to #1637)
- **Date:** 2026-11-11
- **Status:** proposed
- **Decision:** confirm and re-send, the pattern affiliation delivery already uses (#1416). Chosen on the issue over a periodic re-announcement of every hospital.

## Problem

Workforce announces each hospital name version once, as
`helivanta.in.workforce.hospital_named.v1` `{hospital_id, name, name_version}`
(`workforce/hospital_names.go`, `AnnounceHospitalNamesOnce`). The claim on
`workforce_hospital_name_announcements.announced_version` is recorded in the
same transaction as `events.Enqueue`, so a version is never sent twice.

Pharmacy's `pharmacy-hospital-names` consumer stores the newest version per
hospital (`pharmacy/hospital_names.go`). If its handler fails five times
(`pkg/events` `maxDeliver = 5`), the bus dead-letters the event and nothing
replays it. Workforce never re-sends that version. The result:

- after a rename, the counter keeps the previous name until the next rename;
- for a hospital's first announcement, the picker shows
  `Name unavailable · <hospital id>` indefinitely;
- the only signal is the `consumer dead-lettered event` error log.

The limitation is documented today in `docs/api/domains/workforce.md`,
`docs/api/domains/pharmacy.md` and `docs/api/integration-contracts.md`.

## Goal

A hospital's current name reaches pharmacy without an operator, however many
deliveries dead-letter, once pharmacy can handle the event again.

## Decisions

### D1. Pharmacy confirms every name it handles

Pharmacy publishes a new event,
`helivanta.in.pharmacy.hospital_name_received.v1` `{hospital_id, name_version}`
(constant `SubjectHospitalNameReceived`, type `HospitalNameReceivedData`, in
`pharmacy/contract/events.go`). The `pharmacy-hospital-names` handler enqueues
it through the outbox in the **same transaction** as its upsert, so a stored
name and its confirmation commit or roll back together.

It confirms the `name_version` it received, even when the upsert changed
nothing because a newer or equal version is already stored. A redelivered or
reordered event is therefore confirmed too, and its re-sending stops.

The confirmation carries no name: workforce only needs to know which version
arrived, and leaving the name out keeps the payload minimal.

An event the handler rejects as invalid is not confirmed. It dead-letters as
today, and workforce re-sends it (D4). This fails closed: an event pharmacy
could not store is never reported as received.

### D2. Workforce records what is still unconfirmed

Migration `0101_workforce_hospital_name_receipts` adds two columns to
`workforce_hospital_name_announcements`:

- `attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0)`: the number of
  re-sends of the announced version;
- `next_attempt_at timestamptz`: when to re-send it. `NULL` means the
  announced version is confirmed.

It also adds a partial index on `(next_attempt_at) WHERE next_attempt_at IS NOT
NULL`, and grants `helivanta_system` SELECT on `next_attempt_at`, which the
cross-tenant scan needs. The table has one row per hospital, so the index is
created inside the migration.

No `confirmed_version` column: "is the announced version confirmed" is the only
question asked of this table, and `next_attempt_at IS NULL` answers it. The
announced version is the only version ever re-sent (D4).

The existing claim statement also sets `attempts = 0` and
`next_attempt_at = clock_timestamp() + 2 minutes` (`firstRedelivery`). It does
this in the same statement, so every newly announced version starts
unconfirmed. Two minutes leaves time for normal delivery and confirmation; in
the normal case nothing is re-sent.

### D3. Workforce consumes the confirmation

New consumer `workforce-hospital-name-receipt` on
`SubjectHospitalNameReceived`. It validates the envelope the same way
`affiliationReceiptConsumer` does: `Type`, `Version == 1`, an empty
`DestinationTenantID`, a non-nil UUID `hospital_id`, and `name_version >= 1`.
Anything else is an error. Then it runs:

```sql
UPDATE workforce_hospital_name_announcements
   SET next_attempt_at = NULL, updated_at = clock_timestamp()
 WHERE tenant_id = ? AND hospital_id = ?
   AND announced_version <= ? AND next_attempt_at IS NOT NULL
```

A confirmation of an older version than the one announced matches nothing, so
a late receipt for v1 cannot stop the re-sending of v2. A confirmation for a
hospital with no marker matches nothing. Both are no-ops, not errors, so a
duplicate or reordered confirmation converges.

If a confirmation itself dead-letters, workforce re-sends the name. Pharmacy
stores nothing new and confirms again, so the loop still converges.

Workforce imports `pharmacy/contract` for the subject and type. That is
allowed: `.golangci.yml` permits cross-module `contract` imports. This is
workforce's first import of the pharmacy contract.

### D4. Re-sending runs inside the existing announcer pass

There is no second background loop. `AnnounceHospitalNamesOnce` already runs
every 30 seconds and holds the scan, the hospital row lock and the outbox
write, so it gains the re-send.

**Scan.** A hospital is due when its version is unannounced, as today, or when
`next_attempt_at <= now()`. The scan still reads ids only, never names, with
`LIMIT hospitalNameBatch` (200). It orders unannounced versions first, then the
oldest due re-send, so that a backlog of re-sends cannot starve new
announcements.

**Per hospital,** inside `WithTenant`, with the actor
`system:workforce-hospital-name-announcer`:

1. Lock the hospital row `FOR UPDATE`, as today.
2. If `name_version` is greater than the announced version, take the existing
   claim path. The claim resets `attempts` and `next_attempt_at` (D2), so an
   unconfirmed older version is superseded, not re-sent.
3. Otherwise, re-read the marker under the lock with
   `next_attempt_at <= now()`. A confirmation may have landed since the scan,
   and another replica may have re-sent already. If the row no longer
   qualifies, do nothing.
4. Enqueue `hospital_named.v1` with the current name and `name_version`. In
   this branch `name_version` equals `announced_version`, so the current name
   is exactly that version's name. Then set `attempts = attempts + 1` and
   `next_attempt_at = clock_timestamp() + redeliveryDelay(attempts + 1)`. Log a
   warning: `re-sent unconfirmed hospital name`, with `tenant_id`,
   `hospital_id`, `name_version` and `attempt`.

**Backoff.** The delay doubles from 2 minutes and stops growing at 60 minutes:
2, 4, 8, 16, 32, 60, 60, … minutes. A version is never given up on, matching
the affiliation posture. Workforce gets its own copy of `redeliveryDelay`,
because modules cannot share code. The unit test pins the same sequence.

**A permanently rejected name.** A name pharmacy keeps rejecting is re-sent
hourly, with one warning per attempt. Both modules cap names at 150
characters, so this should not happen by construction. If it does, the
warnings are the signal; the system does not go quiet.

### D5. Hospitals announced before this ships

The migration sets `next_attempt_at = clock_timestamp()` on every existing
marker row. Each hospital already announced is re-sent once, at 200 per
30-second pass, and confirmed. This recovers names that dead-lettered before
the change, which no other path would reach. The cost is one extra event per
existing hospital, handled as an idempotent no-op where the name already
arrived.

### D6. Exactly one consumer may rely on this confirmation

`next_attempt_at` records a single confirmation, so it is correct only while
`pharmacy-hospital-names` is the only consumer of `hospital_named.v1`. This is
enforced in `internal/archtest`: a test across `allModules()` fails if any
consumer other than `pharmacy-hospital-names` subscribes to
`SubjectHospitalNamed`. Its failure message says a second consumer needs
per-consumer confirmation.

### D7. Recovery bound

Once pharmacy can handle events again, a dead-lettered name arrives at its next
re-send. That is at most 60 minutes, plus one 30-second pass, plus any batch
backlog. The docs state it as "within about an hour after the failure clears".
If the failure lasted less than about 30 minutes in total, recovery is
correspondingly sooner, because of the backoff schedule.

## Out of scope

- Generic dead-letter inspection and replay (#600).
- Other consumers of `hospital_named.v1`. None exist, and D6 blocks adding one
  without per-consumer confirmation.
- Any frontend change. `Name unavailable · <id>` stays the honest state while a
  name is in flight.
- Supplier and pharmacy establishments, which are never announced.

## Contracts and docs

- `pharmacy/contract/events.go`: `SubjectHospitalNameReceived`,
  `HospitalNameReceivedData`. Pharmacy's `Publishes` gains the subject.
- `workforce/module.go`: migration `0101_workforce_hospital_name_receipts` and
  the new consumer. Recheck `0101` against main and the `codex/*` branches
  before merge.
- `docs/api/domains/workforce.md` (the `hospital_named.v1` paragraph) and
  `docs/api/domains/pharmacy.md` (the `hospital_name` paragraph, edited by
  hand, never with prettier): replace the dead-letter limitation with the
  confirmation, re-send and the D7 bound.
- `docs/api/integration-contracts.md`: add the confirmation to the
  workforce → pharmacy row.
- Regenerate with `go run ./cmd/contractdocs`, then `-check`.
- `globalapi/workforce_person_review_test.go`: add `0101` to both
  hand-maintained migration lists.
- Staff-access ledger: if `workforce/module.go` or `pharmacy/module.go` hashes
  change, the pinned reviews need recheck notes, applied only with the user's
  approval.

## Tests

Every new assertion must be shown to fail against a mutation of the code it
covers.

**Pharmacy (`hospital_names_test.go`)**

- A valid event writes the name and exactly one `hospital_name_received.v1`
  outbox row carrying the received `name_version`, in the same transaction.
  Mutation: enqueue outside the transaction, or drop the confirmation.
- An older or equal version leaves the stored name unchanged and is still
  confirmed with its own version.
- An invalid event writes no name and no confirmation.

**Workforce receipt consumer**

- A receipt for the announced version clears `next_attempt_at`.
- A receipt for a newer version also clears it (`<=`).
- A receipt for an older version leaves it set.
- An unknown hospital is a no-op.
- An invalid envelope (each rule in D3) returns an error.

**Announcer**

- A new claim sets `attempts = 0` and `next_attempt_at` about 2 minutes ahead.
- A due, unconfirmed version is re-sent with the same `name_version` and
  current name; `attempts` increments and `next_attempt_at` follows the
  backoff.
- A confirmed version (`NULL`) is never re-sent.
- A rename while the old version is unconfirmed announces the new version and
  never re-sends the old one.
- Two concurrent passes produce one re-send.
- `redeliveryDelay` table: 1 → 2 m, 2 → 4 m, … 6 → 60 m, 20 → 60 m.

**Recovery, end to end.** Announce a name, then let the delivery be lost: the
event is never handled, standing in for a dead-letter. Pharmacy has no name
and the marker is unconfirmed. Make it due and run one pass. Pharmacy now
stores the name, the confirmation is consumed, and `next_attempt_at` is
`NULL`. A further pass sends nothing. Cover both a first announcement and a
rename.

**Migration backfill.** Marker rows that existed before `0101` are due
immediately after it.

**Archtest (D6).** The test fails when a second consumer of
`SubjectHospitalNamed` is registered.
