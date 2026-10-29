# Pharmacy hospital affiliation verification

Resolves #1416. Unblocks #1386 slice B-2c (registered patients at the counter) and
the admitted-patient billing in #1387. Applies [ADR-0024](../../adr/0024-pharmacy-ownership-and-outlet-tenancy.md).

## Why

A pharmacy can declare a link to a hospital (`owner`, `serving` or `partner`), but
the link is stuck at `pending_validation`: nothing can verify it. A self-declared
link, or a supplied hospital id, is not authority to look up, read or cite that
hospital's patients (AGENTS.md). B-2c needs a link that the hospital itself has
accepted before the counter may reference a registered patient.

## Decisions (accepted 2026-10-29)

1. **Same tenant only.** A hospital-owned pharmacy is in the hospital's tenant
   (hospital onboarding runbook). A link to another customer's hospital crosses
   tenants and needs the separate directional data-sharing agreement ADR-0024
   describes. That is a separate issue, not an extension of this one.
2. **The hospital approves.** Patientmaster owns hospital identity and per-hospital
   patient-access grants, so it owns the decision. The reviewer must hold the new
   `patient.pharmacy-affiliation.review` permission (no role: tenant administrators
   only, like the other patientmaster review permissions) **and** a current
   `registrar` grant at that hospital, locked through commit. The person who
   requested verification cannot accept it. The reviewer records a reason. The
   evidence is the attributed act of a hospital registrar-administrator accepting
   the named pharmacy; no document upload is part of this slice.
3. **Counter lookup is by exact MRN only** (slice 1416b), under a dedicated
   hospital-scoped grant that works only while the affiliation is accepted, and
   returns the minimum the pharmacist needs to confirm the person. Every lookup
   is audited.

## Owners and data

| Fact                                               | Owner         | Copy elsewhere                                  |
| -------------------------------------------------- | ------------- | ----------------------------------------------- |
| The declaration (pharmacy, hospital, relationship) | pharmacy      | patientmaster keeps the claim with each request |
| A verification request and its withdrawal          | pharmacy      | patientmaster review queue (via events)         |
| Accept, decline, revoke, with reviewer and reason  | patientmaster | pharmacy history row (no reviewer identity)     |

The declaration row stays immutable. Verification is a new append-only history,
`pharmacy_affiliation_verifications`, one row per step, numbered per affiliation.
The current status is the latest row: `pending_validation` (no request yet),
`requested`, `verified`, `declined`, `revoked` or `withdrawn`. Nothing is edited;
revocation and withdrawal are new rows.

## Lifecycle

```
pending_validation ──request──▶ requested ──accept──▶ verified
        ▲                        │  │                   │  │
        │                        │  └─decline─▶ declined │  └─revoke (hospital)─▶ revoked
        └──── request again ◀────┴──withdraw (pharmacy)──┴────withdraw──────────▶ withdrawn
```

- **Request** (pharmacy, `pharmacy.access.manage`): allowed when the latest status is
  `pending_validation`, `declined`, `revoked` or `withdrawn`. Each request is a new
  round with its own client-generated UUIDv7, so a retry is idempotent. Pharmacy
  publishes `helivanta.in.pharmacy.affiliation_verification_requested.v1`.
- **Withdraw** (pharmacy): allowed while `requested` or `verified`. Ends the round.
  Publishes `helivanta.in.pharmacy.affiliation_verification_withdrawn.v1`. Withdraw
  carries the same claim as the request so either can arrive first.
- **Decide** (patientmaster): `accepted` or `declined` from pending; `revoked` from
  accepted. Publishes `helivanta.in.patientmaster.pharmacy_affiliation_decided.v1`.
- **Applying a decision in pharmacy:** a decision applies only to the round that is
  still open with that request id. A decision for a withdrawn or superseded round is
  a no-op. `revoked` may arrive before its `accepted` (redelivery); it closes the
  round directly, and the late `accepted` is then a no-op. Both sides converge to
  closed. A database trigger enforces the transitions and the numbering, so no code
  path can skip a step.

## Delivery guarantee

The bus dead-letters an event after 5 failed deliveries (about 2.5 minutes). Nothing replays
the dead-letter subject, so a lost step would leave the two sides disagreeing:

- **A withdrawal is lost.** Patientmaster keeps an accepted link the pharmacy has ended, and the
  #1416b lookup would still answer.
- **A revocation is lost.** Pharmacy keeps a verified link the hospital has ended, and B-2c would
  still accept the hospital's patients.

Both are unsafe. So every step is re-sent until the other side confirms it:

- Each side keeps one mutable delivery row per round. Pharmacy keeps
  `pharmacy_affiliation_deliveries` (the round's latest pharmacy step: `requested` or
  `withdrawn`). Patientmaster keeps `patient_pharmacy_affiliation_deliveries` (the round's latest
  decision). Writing a step sets `next_attempt_at` in the same transaction as the step and its
  event.
- The receiving consumer confirms each step it has handled, including a duplicate or a no-op.
  Patientmaster publishes `helivanta.in.patientmaster.pharmacy_affiliation_received.v1`
  `{request_id, affiliation_id, state}`. Pharmacy publishes
  `helivanta.in.pharmacy.affiliation_decision_received.v1`
  `{request_id, hospital_id, decision_id}`. A confirmation clears `next_attempt_at` only if it
  names the step still current. A confirmation of a superseded step changes nothing.
- A reconciler in each module (30 s tick, 50 rows a pass) re-publishes any due step. Each
  re-send pushes `next_attempt_at` back exponentially, from 2 minutes up to 60. A step is never
  given up on. The only cross-tenant read is ids and `next_attempt_at`; the re-send itself runs
  under the row's tenant, as the named system actor.
- A step that can never be applied (a malformed or mismatched event) is never confirmed. It is
  re-sent hourly and logged each time, rather than silently dropped.

Disagreement lasts only until a re-send is confirmed: minutes while both sides are up, not
forever.

## Event payloads

All same-tenant, no patient data.

- `AffiliationVerificationData` (requested and withdrawn): `request_id`,
  `affiliation_id`, `pharmacy_id`, `pharmacy_name` (the reviewer needs to recognise
  who is asking; organisation name, not personal data), `hospital_id`,
  `relationship`, `requester_subject` (opaque, used only to reject self-acceptance,
  same justification as mediconnect's registration review).
- `PharmacyAffiliationDecidedData`: `request_id`, `affiliation_id`, `hospital_id`,
  `decision_id`, `outcome`, `decided_at`. The reviewer's identity and reason stay
  in patientmaster.

## HTTP

Pharmacy (`/v1/pharmacy`):

- `PUT /pharmacies/:pharmacy/hospital-affiliations/:id/verification-requests/:request`
  `{}` → 200 with the affiliation. Replaying the same request id is 200; asking for
  a new round while one is open is `409 verification_conflict`.
- `POST /pharmacies/:pharmacy/hospital-affiliations/:id/verification-requests/:request/withdraw`
  `{ "reason" }` → 200. A retry with the same reason is 200; a different reason, or
  a round that is not open, is 409.
- `GET /pharmacies/:pharmacy/hospital-affiliations/:id/verifications`: paginated
  history, `pharmacy.catalogue.read`.
- The list gains `verification_status`, `verification_request_id` and
  `verified_at`. `verification_status` gains the values listed above. This is
  additive.

Patientmaster (`/v1/patients`, `?hospital_id=` selector, checked server-side):

- `GET /pharmacy-affiliations`: the hospital's requests, newest first.
- `PUT /pharmacy-affiliations/:request/decision/:decision`
  `{ "outcome": "accepted"|"declined"|"revoked", "reason" }`. Reason is 1 to 500
  characters. The decision id makes a retry idempotent; the same id with different
  input is 409. Self-acceptance and the wrong state are `409 review_conflict`.
- Both need `patient.pharmacy-affiliation.review` plus a registrar grant. With no
  grant the answer is 404, as everywhere else in patientmaster.

## Patient lookup (slice 1416b, decided here, built next)

- Pharmacy keeps an id-only list of registered patients (`patient_id`,
  `hospital_id`), built from `helivanta.in.patientmaster.registered.v1`, kept only
  for hospitals with a verified affiliation. On acceptance, patientmaster
  re-announces that hospital's registrations to pharmacy, using the replay
  mechanism from #1377. Pharmacy accepts a patient id on a sale only if the id is
  in that list and the outlet's pharmacy holds a verified affiliation at the time
  the sale posts.
- The hospital grants named pharmacists a separate `pharmacy_lookup` grant in
  patientmaster. The existing reader path must reject it, so it confers no
  registry access. `GET /patients/pharmacy-lookup?hospital_id=&pharmacy_id=&mrn=`
  matches the exact MRN (MRNs are unique per tenant, `UNIQUE (tenant_id, mrn)`).
  It works only while that pharmacy's affiliation with the hospital is accepted.
  It returns the patient id, name, age in years and sex. Every lookup, including a
  miss, writes `patient_access_audit`.

## Not covered

- Cross-tenant affiliations (needs the data-sharing agreement design).
- Uploaded evidence documents or a second approver.
- Notifying the reviewer of a new request. The queue is visible on the Patients
  page.
- Picking a hospital from a directory when declaring. The declaration still takes
  the canonical hospital id from onboarding.

## Tests that must exist

- Pharmacy and patientmaster integration tests: request → accept → verified;
  decline; revoke; withdraw before and after acceptance; withdraw arriving before
  the request in patientmaster; a decision for a withdrawn round is a no-op;
  revoke before accept converges.
- Self-acceptance refused; no grant gives 404; a reader grant is refused; the same
  decision id with different input is 409.
- Database: the history is immutable, transitions are enforced, numbering has no
  gaps, and two concurrent requests cannot both open a round.
- Delivery: a step whose event is lost is re-sent by the reconciler until it is
  confirmed. Confirming it stops the re-sends; confirming a superseded step does not.
  This holds in both directions.
- Frontend: the status labels; request and withdraw in the registry; accept,
  decline and revoke in the hospital queue.
