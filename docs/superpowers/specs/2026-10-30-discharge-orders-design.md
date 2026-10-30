# Discharge orders: the doctor marks an admission fit for discharge (#96 slice 1)

Resolves: #96 (slice 1 of discharge). Unblocks the final bill (#197), which needs to know
when discharge begins, not only when the bed is released.

## Why

MediCore records one discharge-related fact today: the release, when the patient leaves the
bed. Clinical readiness is not recorded anywhere, so billing cannot start the final bill
when the doctor decides, the ward cannot see who is going home today, and the discharge-day
wait cannot be measured. IN-CL-07 keeps clinical readiness, physical departure, financial
close and bed cleaning as separate facts; this slice adds the first.

## What it does

- **A doctor orders discharge** for an admitted patient (`medicore.discharge.order`,
  doctors, plus the MediCore hospital grant): disposition `home` or `transfer_out`, and an
  optional note of up to 500 characters. The order is recorded with who and when, and
  `medicore.discharge_ordered.v1` is published.
- **One active order per admission.** A second order while one is active is
  `409 already_ordered`. Ordering needs the admission still in its bed: otherwise
  `409 not_admitted`.
- **Withdrawn, never edited.** The doctor withdraws an order with a reason (1–500 characters)
  while the patient is still admitted: `medicore.discharge_withdrawn.v1` is published, and a
  new order may follow. A withdrawn order keeps everything it said.
- **Release completes it.** The release (ward staff, as today) is the departure; the active
  order stays as the record of who decided it and when. A withdrawal after release is
  `409 not_admitted`.
- **Idempotent** by client UUIDv7 command id, like every bed command: the same order or
  withdrawal again returns the record; a different one under the same id is `409 conflict`.
- **Seen on the ward board and the admission.** The bed board carries the active order's
  id, disposition and time; `GET /admissions/:id` carries the whole order (`discharge`),
  including who ordered it. The ward board shows "Discharge ordered", and doctors see Order
  discharge / Withdraw, labelled with the bed.

## Rules enforced in the database

- An order is inserted only for an admitted admission, which it share-locks; a withdrawal
  only while it is still admitted.
- At most one order per admission without a withdrawal (partial unique index); a
  withdrawal id is used once per tenant.
- An order's fields never change; it is withdrawn once, with its reason, actor and time.
- No delete. Forced row security by the MediCore hospital grant; audited.

## Calls I made (builder's)

- **Dispositions are `home` and `transfer_out` only.** Leaving against advice, absconding and
  death are departures recorded at release, not decisions a doctor makes in advance; they
  need their own required fields (#96 lists them) and are not this slice.
- **Release does not require an order** in this slice: a release for `discharged` without
  one stays possible, as today, so wards whose doctors do not yet use the system are not
  blocked. Requiring it is a one-line check once hospitals adopt orders, and is a decision
  for the product owner.
- **Any doctor holding the hospital grant may order.** MediCore has no treating-consultant
  assignment yet; the record names who ordered.
- **The note stays in MediCore.** The events carry ids, disposition and times only.

## Not covered

Discharge summary, medication reconciliation, departmental clearances (#1043), discharge
types with their own fields (DAMA, absconded, death), referrals, Temporal orchestration,
the final bill (#197 next slice), a planned discharge time, discharge worklists (#983).
