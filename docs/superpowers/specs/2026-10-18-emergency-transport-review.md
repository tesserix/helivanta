# Attributed transport requirements before reservation

Programme #1220; canonical case #121 and dispatch #123. This increment is a
prerequisite for atomic reservation, not proof that a crew has been dispatched.
Build on the exact current production web package, `apps/shell`, and the fleet
foundation from #1233. Do not touch the existing pharmacy/supply implementation.

## Clinical authority

A role alone must not approve dispatch requirements. A tenant administrator
records an explicit, expiring clinical-reviewer verification for an identity
subject at an operating base, including an opaque reviewed evidence reference.
This grants no implicit case access. The reviewer needs ordinary explicit base
access AND `emergency.case.assess` (doctor role or explicitly privileged tenant
administration) AND the current active clinical verification. Tenant admin has
no implicit operational/clinical object access. A supervisor cannot approve a
requirement through their emergency-supervisor role alone.

Verification changes are immutable append-only versions. Revoking or replacing
one invalidates future assessments under the old verification. Reservation must
also check that the assessment's verification remains the current active one.
These are attributed manual verifications, not a claimed licensing-authority or
workforce integration. A named medical lead owns the policy before activation.
Enabling or renewing one's own clinical verification is prohibited; self-revocation
is permitted. The API and SQL both require an independent verifier for activation.

This is an emergency review-privilege grant, not a second professional-licence
register. Workforce's maker-checker credential validity from main `6dfa658`
(#1231) remains the canonical source of reviewed professional credentials. Its
credential numbers, issuers and dates stay in the workforce module; its published
change events intentionally carry opaque record/version references only. Before
live reservation, integrate a bounded, fail-closed eligibility proof and explicit
staff-to-identity binding. A privilege grant alone cannot satisfy that check.

## Assessment and concurrency

The reviewer sees protected incident detail using the existing audited API and
records a supported transport class, approved policy reference and validity
deadline against the exact case version. No diagnosis, triage score, ETA or
equipment threshold is invented. This is a transport requirement; clinical
triage remains the clinical module's responsibility. Unsupported neonatal,
mortuary and home-visit requests cannot be relabelled into supported dispatch.

Lock the base before the case. Verification updates exclusively lock the base;
assessment takes a shared base lock then a case lock. A case-local monotonically
increasing sequence, assigned under the case lock, determines the current review;
request arrival timestamps and client UUID ordering do not. Retries bind an
immutable command UUID to the exact payload and actor. Stale case versions,
changed payloads, expired/revoked verification and closed cases fail closed.

SQL repeats policy/identity/sequence/expiry checks. Assessment, audit and an
opaque metadata-only outbox event commit together. The incident/contact never
enters an event or Temporal history. There is no new secret or plaintext clinical
narrative. Review lists are scoped and cursor paginated.

## Subsequent dispatch boundary

Reservation must lock case, vehicle, crew and duty in a stable order, recheck
database-time readiness/current review, and allocate unique vehicle/case/crew
slots. Durable acknowledgement/reassignment/receiving waits reuse Temporal.
Use a separate non-superuser, non-BYPASSRLS `helivanta_emergency_worker` database
identity with exact column grants and transaction-local tenant/base/dispatch
scope, following the existing document-worker pattern. No app role, admin DSN,
service-account token or PHI decrypt capability in that worker. Bootstrap its
new OpenBao-backed credential and all readers/writers together before activation.

Unacknowledged expiry escalates for human reconciliation; it must not silently
release a vehicle that may have received an offer offline. Case, duty and vehicle
slots must participate in the same transaction, including cancellation/close/end
guards, to avoid a check-then-lock race with staff actions. Hospital preference,
receiving acceptance and signed handover stay distinct attributed facts.

## Rollout and limits

Use additive `0003_emergency` after the foundation. No live schema action without
the named approval required by the working agreements. Preserve append-only
history on code rollback. This increment uses the existing API/Postgres/outbox;
there is no extra service cost. Full pilot activation still requires the
programme's clinical, operator, receiving-hospital, device and recovery gates.
