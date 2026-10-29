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

## Patient lookup (slice 1416b)

### The pharmacy's list of registered patients

Pharmacy keeps `pharmacy_registered_patients (tenant_id, patient_id, hospital_id)`, built by
`pharmacy-patient-registered` from `helivanta.in.patientmaster.registered.v1`. Ids only.

**Ruling (2026-10-29):** the list keeps every registration in the tenant, not only those of
hospitals with a verified affiliation. Authority is checked when a patient id is used, never
when it arrives.

- MediCore's `medicore_patients` already works this way: same event, same tenant, ids only.
- Filtering on arrival would mean a replay on every acceptance and a purge on every
  revocation, and the purge would be one more message that can be lost.
- Pharmacy already receives every one of these events, so storing the ids discloses nothing
  new.

Registrations made before pharmacy's consumer existed arrive through the #1377 replay
(`POST /patients/hospitals/:hospital/registration-replays`, tenant administrators).

B-2c will accept a patient id on a sale only if the id is in this list for the hospital, and
the outlet's pharmacy holds a `verified` affiliation with that hospital in pharmacy's own
history when the sale posts.

### When a link is active

Patientmaster keeps `patient_pharmacy_links (tenant_id, hospital_id, pharmacy_id, request_id)`.
A row is written only when all of these hold:

- Pharmacy has confirmed the acceptance and said it **applied** it:
  `affiliation_decision_received.v1` carries `applied: true`. A decision for a round pharmacy had
  already closed is still confirmed, so redelivery stops, but with `applied: false`, so no link
  opens.
- The review is still `accepted`, read under a `FOR SHARE` lock on the review row. A revocation
  or withdrawal in flight holds that row `FOR UPDATE`, so the confirmation waits for it and then
  sees the outcome. Without the lock, a confirmation could read the old snapshot and open a link
  nothing would ever close.
- No older round's link still stands for that pharmacy and hospital. If one does, the
  confirmation fails and is re-sent until the older link ends. A duplicate of the same round is
  idempotent.

The row is deleted in the same transaction as a revocation, or as a withdrawal reaching
patientmaster.

So a lookup works only after both sides agree the link is verified. A hospital revocation
stops it at once. A pharmacy withdrawal stops it as soon as patientmaster receives it, which
the redelivery loop guarantees.

### The lookup grant

`patient_pharmacy_lookup_access (tenant_id, hospital_id, pharmacy_id, request_id, subject,
granted_by, created_at)` lists the named pharmacists the hospital allows to look up its
patients for that pharmacy.

- It is a separate table, never read by `patient_hospital_allowed`, so it confers no registry
  access.
- Each grant is tied to the link's `request_id`. When a link ends, its grants stop working,
  and re-verifying the link does not bring them back.
- Routes:
  - `PUT /patients/hospitals/:hospital/pharmacy-lookup-access/:pharmacy/:subject` grants;
  - `DELETE` on the same path revokes;
  - `GET /patients/hospitals/:hospital/pharmacy-lookup-access?pharmacy_id=` lists the grants.
- All three need `patient.pharmacy-affiliation.review` plus a registrar grant. A grant needs an
  active link; without one the answer is 409.
- Grants and revocations are audited in `patient_access_audit` (`grant` and `revoke`, access
  level `pharmacy_lookup`, with `pharmacy_id`). A repeat that changes nothing gets the same
  answer and records nothing.

### The lookup

`GET /patients/pharmacy-lookup?hospital_id=&pharmacy_id=&mrn=` needs the new permission
`patient.pharmacy-lookup` (role: pharmacist).

- **One transaction.** It locks the caller's grant and the active link `FOR SHARE`, then reads
  the patient whose MRN matches exactly (trimmed, upper-cased; MRNs are unique per tenant).
  The `patients` row is visible through a new RLS branch, `patient_pharmacy_lookup_allowed`. It
  needs the grant and the link for the session's subject, hospital and pharmacy, **and** the
  row's MRN to equal the session's (`app.pharmacy_lookup_mrn`), so the database admits one
  patient at most. While lookup settings are present, restrictive policies refuse every row of
  `patient_consents`, `patient_identifiers`, `patient_duplicate_overrides` and
  `patient_reported_blood_groups`. The registry path is unchanged.
- **Answer:** `{patient_id, name, age_years, age_estimated, sex}`. Age is in whole years from
  the date of birth; `age_estimated` repeats `dob_estimated`. No MRN echo, date of birth,
  mobile, address or identifiers.
- **Ruling: age uses the UTC calendar.** Patientmaster holds no hospital time zone, and
  registration already checks dates of birth against UTC. In India that makes the age one year
  low from 00:00 to 05:30 on a birthday. Age is an aid to recognising the person, never a dosing
  or eligibility input.
- **No grant, no active link and no match all return the same `404`,** so the answer never
  shows which of the three failed.
- **Every lookup is audited** in `patient_access_audit`, action `pharmacy_lookup`, with
  `pharmacy_id` and the matched id. A miss has empty `resource_ids` and is audited too.
  Pharmacy's `/patients` access stays as it is.

### Not covered in 1416b

- The counter UI that calls the lookup, and its use on a sale: B-2c.
- Rate limiting beyond the platform's per-principal limit. MRN guessing is bounded by that
  limit and fully audited.

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
