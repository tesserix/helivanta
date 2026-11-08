# Pharmacy outlet receiving under a reviewed operating duty (#19 slice 3b-ii)

Issue: #19.
Decisions consumed: ADR-0054, ADR-0063 and ADR-0064.
Delivery ledger row: "PharmaConnect's receiving template still lacks its actual product consumer"
([ledger](../../design/staff-and-rbac/independent-collaboration-delivery.md)).
Re-plan: [2026-11-08 hospital link discovery](../plans/2026-11-08-hospital-link-discovery.md).

## Problem

ADR-0064 already lets a pharmacy owner prepare `pharmacy.inventory_receiver.v1`
for a reviewed account at an exact owned outlet. A different reviewer approves it,
and the projection takes effect only after its graph revision is acknowledged.

Nothing consumes that duty yet. PharmaConnect stock routes need two things:

- the tenant role permission `pharmacy.stock.receive`;
- an outlet `editor` row in `pharmacy_outlet_access`.

The RLS `WITH CHECK` on the stock tables requires that row. So an approved
receiver cannot record a receipt, and a reviewed duty is not operating authority
anywhere.

## Actors and authority

| Actor | Authority kind | Scope | Preconditions |
| --- | --- | --- | --- |
| Receiver | Independent operating assignment, nonclinical | One `(pharmacy, outlet)`, independent or chain ownership, country `IN` | Acknowledged active duty. Current Staff and link basis. Uninterrupted membership. Credential not revoked since the command's verified authentication |
| Outlet editor (existing) | Pharmacy outlet grant | One outlet | Unchanged |

The duty adds no tenant-wide permission. A hospital-owned pharmacy is refused by
the location guard, because its staff use hospital workforce authority.

## Behaviour

New route family, owned by PharmaConnect:
`/v1/pharmacy/pharmacies/:pharmacy/operating-locations/:outlet/stock`.
It is declared `authz.Public` at the router. Admission happens only inside the
handler, through `OperatingAuthorizer.WithOperatingScope`.

| Route | Duty permission | Effect |
| --- | --- | --- |
| `GET /products` | `pharmacy.stock.read` | Active catalogue identity needed to name a lot: SKU, names, dosage form, ingredients, pack, unit and currency. MRP, schedule, tax and price controls stay editor-only |
| `GET /locations`, `GET /batches`, `GET /batches/:id`, `GET /receipts`, `GET /receipts/:id` | `pharmacy.stock.read` | Outlet stock history only |
| `PUT /batches/:id` | `pharmacy.stock.receive` | Create-or-replay a batch. Its identity is immutable |
| `PUT /receipts/:id` | `pharmacy.stock.receive` | Create-or-replay a `receipt` movement into a quarantine location |

- **Same ledger.** Writes go to the existing `pharmacy_stock_batches` and
  `pharmacy_stock_movements`, through the existing validation, `stockLock` and
  `StockMovementRecorded` outbox. There is no second stock master.
- **Replay.** As on the existing editor routes, the first write and an identical
  replay both answer 200 with the stored record. The same ID with a different
  input answers 409 (`errStockIdentity`).
- **Quarantine only.** `pharmacy_stock_movement_location_guard` already enforces
  this. The duty policy also refuses `correction` and `release` kinds
  structurally. Release still needs `pharmacy.stock.release`, which the template
  does not carry.
- **Status codes.**

  | Case | Status |
  | --- | --- |
  | Unknown, foreign or hospital-owned pharmacy or outlet | 404 |
  | No duty, or a revoked, expired or superseded duty | 404 |
  | Staff or link basis changed | 404 |
  | Permission outside the template | 404 |
  | Credential revoked, or the assignment is invalid, since the command's verified authentication | 401 `session_changed` |
  | `X-Helivanta-Expected-Subject` differs from the verified subject | 409 `session_changed` |
  | Authority, graph, membership, identity or source unavailable | 503 `authorization_unavailable` |
  | Duty evidence could not be written; nothing was saved | 503 `audit_unavailable` |
  | Invalid input | 400 |
  | Changed replay | 409 |

  There is no 403, because a 403 would confirm that the outlet exists.
- **Attribution and audit.**
  - `actor_subject` is the verified subject.
  - A new append-only `pharmacy_operating_stock_audit` row records the movement
    or batch ID, `duty_id`, `snapshot_version` and `authority_epoch`. It is
    written in the same transaction as the write and the outbox entry, and both
    roll back together.
  - Reads also write an audit row with action `pharmacy.operating_stock.read`.
- **Data minimization.** Responses carry the existing batch and movement shapes
  for this outlet only. There is no cross-outlet overview and no outlet licence or
  contact data.

## Receiver panel

- Route: `/pharmacy/pharmacies/:pharmacy/operating-locations/:outlet/receiving`
  in the shell. It is a deep link with no `zones.ts` page: the duty is not a
  tenant permission, so no navigation entry could declare one. A malformed ID
  renders "not available" without calling the API.
- Data comes from `@helivanta/api` `pharmacy-receiving`. Every request sends the
  expected tenant and subject. Every answer is checked against this outlet and
  the requested ID, and a write answer must equal what was sent.
- Each command keeps one client-generated UUIDv7. A lost or 5xx answer freezes
  the form. The receiver can retry under the same ID, or recover it with a GET
  that writes nothing. A 400 or 409 means nothing was written, so the ID is
  released for corrected details.
- 404, 401 `session_changed` and 409 `session_changed` end the panel with a
  "no longer available" or "account changed" message and no retry, because only
  reopening the duty can recover.
- A new lot's MRP starts empty rather than at zero. A receipt goes only to a
  quarantine location; when the outlet has exactly one, it is preselected.

## Data and enforcement

Migration `0037_pharmacy_operating_receipts` (append-only):

- `pharmacy_operating_stock_scope(t, outlet, operation)` admits a row only when
  all of these hold:
  - `app.pharmacy_stock_access='authorized'`;
  - `app.pharmacy_stock_pharmacy` and `app.pharmacy_stock_outlet` match the row;
  - `app.pharmacy_subject` equals `app.actor_subject`;
  - the permission setting matches the operation.

  It mirrors `supply_stock_scope`. The settings are transaction-local, so they
  vanish when the transaction ends. Postgres cannot see the Go guarded command,
  so an architecture test enforces structurally that exactly one production
  file, the IAM-admitted `operatingStock` path, sets
  `app.pharmacy_stock_access`. That path restores the previous values on exit.
- `USING` on locations, batches, movements and catalogue products gains
  `OR pharmacy_operating_stock_scope(...,'read')`. Catalogue products need it for
  the batch `FOR SHARE` product check.
- `WITH CHECK` on batches gains `OR pharmacy_operating_stock_scope(...,'write')`.
  On movements it gains the same condition `AND kind='receipt'`. Locations and
  catalogue products stay editor-only.
- Forced RLS covers the audit table, and it refuses `UPDATE` and `DELETE`.

## Not covered by this slice

- **Creating stock locations or catalogue products.** These remain outlet editor
  configuration.
- **Receiver self-discovery and receiver-only navigation.** IAM has no
  caller-scoped "my operating locations" read. Supply has the same open ledger
  row. This is a separate shared slice: the receiver reaches the panel only from
  a known outlet link.
- **Drug master barcode lookup at pharmacy outlets.** The template's
  `drugmaster.stock_reference.read` is not consumed here.
- **Deployed runtime acceptance and real OIDC browser acceptance.**

## Numbers and failure behaviour

- **Load.** Receiving is human-paced: tens of receipts per outlet per day. Each
  write costs one IAM guarded command (graph check plus Postgres rechecks) plus
  one PharmaConnect transaction. That is well inside the existing p99 budget for
  supply receipts.
- **Failure.** When IAM, OpenFGA or Workforce is unavailable, duty routes answer
  503 and the existing editor routes are unaffected. When PharmaConnect is
  unavailable, both paths fail. Neither path degrades to the other.
- **Rollback.** Revert the code. The migration is additive: a new function, new
  `OR` branches and a new table. Without the settings the branches evaluate
  false, so old code is unaffected.
- **Rejected alternative.** Switching the existing routes to `authz.Public` with a
  dual check. That would weaken the router gate for every existing caller across
  about 15 routes to serve one new actor.

## Acceptance tests (red first)

1. A globalapi end-to-end test: prepare → approve → acknowledge, then the receiver
   records a batch and a quarantined receipt, sees the history, and replays both.
   After revocation the same reads answer 404.
2. The duty holder gets 404 for a sibling outlet, a foreign pharmacy, a
   hospital-owned pharmacy, a correction or release attempt, and creating a
   location.
3. A Staff edit or relink, a membership interruption, credential revocation,
   expiry and an unavailable graph each refuse the duty (404 or 503 as above).
   An editor's existing routes stay unchanged.
4. Postgres policy tests:
   - the stock tables stay invisible without the settings, and an architecture
     test fails if a second file sets `app.pharmacy_stock_access`;
   - a mismatched subject, outlet or permission is refused;
   - `kind<>'receipt'` is refused;
   - the audit row rolls back with the write.
5. An SDK hook and a receiver panel, using `renderWithProviders`, for loading,
   empty, 404 "access changed", 409 and success.
