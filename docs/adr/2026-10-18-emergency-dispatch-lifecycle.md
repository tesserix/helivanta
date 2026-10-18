# EmergencyConnect dispatch lifecycle and activation

Status: implementation in progress; extends the operational-dispatch specification
and programme #1220. The planning envelope remains 20 commands/s, 200 metadata
reads/s, 8 KiB commands, API p99 below 500 ms and 99.9% monthly availability.
These are validation targets, not measured production results.

Use the existing emergency module and Postgres for reservation and command truth.
A committed outbox intent starts a Temporal workflow per dispatch; Temporal owns
acknowledgement, reconciliation and receiving-response waits. Reservation receipt
does not mean that a crew acknowledged or an ambulance is on the way.

Reservation checks the latest case-version-bound clinical assessment, current
independent reviewer delegation, vehicle/crew validity, measured readiness and
persistent fleet constraints in one transaction. Lock base, case, vehicle, duty
and sorted crew in that order. Partial uniqueness prevents concurrent case,
vehicle and crew commitments. A later safety change requires reconciliation;
it cannot silently free capacity a crew might still be using. Closure and duty
end must not bypass active commitments.

Command IDs bind actor, immutable body and assignment generation. Retries return
the original result; changed bodies/actors conflict. Crew acknowledgement and
ordered progress require assigned identity and current version. Offline commands
preserve original IDs and sequence and remain pending until accepted server-side.
Ambiguous cancellation, timeout and reassignment need explicit reconciliation.

A read-only clinical-ledger eligibility policy permits scoped base dispatchers
to verify the latest reviewer revision. It grants no clinical writes or access
through administrator-only API routes. All new records enforce tenant/base RLS;
tenant administration grants no implicit operational access. Outbox and Temporal
carry opaque IDs and versions, never patient, contact, location or credentials.

Destination preference, bilateral service agreement, pre-alert, acceptance,
arrival and handover remain distinct facts. A cross-tenant recipient sees only
directed records under its approved agreement. Rejection, capacity changes and
destination reassignment preserve prior requests and responses. Hospital roles
cannot browse another provider's cases through an administrative connection.

Citizen requests need a bounded, random, scoped and revocable request capability
stored hashed, or a verified citizen identity. Credentials stay out of URLs/logs;
OpenBao encrypts contact and consented location. Request receipt is distinct from
dispatch confirmation. Discovery exposes approved service information, never
precise unassigned fleet positions or invented ETA guarantees. Public calling
works independently of booking dependencies.

Named clinical/dispatch owners, approved qualifications and receiving policies,
retention operation, emergency fallback and device evidence are activation gates.
Synthetic policies cannot satisfy them. Build and test while keeping production
booking disabled until those gates hold.

Postgres failure rejects commands. OpenBao failure prevents protected reads and
new intake. NATS failure retains outbox intent. Temporal failure retains capacity
with an unconfirmed workflow and visible reconciliation. Routing failure yields
no estimate. Retries cannot release a newer assignment's capacity.

Add forward-only migrations after 0003 without rewriting deployed migrations.
Rollback disables new entry points through GitOps and rolls code forward while
preserving reservations/history. Reuse the existing database and Temporal cluster;
price dedicated workers before activation. A mutable availability flag and a
second capacity datastore were rejected because they break atomic invariants.
