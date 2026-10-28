# ADR-0041: Live MediConnect claim observation

Status: accepted for the owner observation boundary; clinical consumers remain
unfinished and production enablement is separately gated.

MediConnect owns an unverified intake claim and immediate withdrawal. Its queued
review event cannot prove current owner state to a later clinical decision.
Patientmaster owns reviewed identity/capacity and must separately establish
purpose consent, recipient scope and source-record release.

Provide an internal POST returning only an exact self-claim current predicate.
Authenticate the canonical patientmaster workload with the existing Kubernetes
TokenReview verifier, a dedicated audience and an exactly configured service
account. Derive the tuple at the caller from an authorized canonical receipt;
it must not be an unverified facility selector. Keep ordinary browser operations
behind Global API and keep workload credentials out of the client.

The owner pins tenant/claim/subject/hospital/patient in a transaction-only read
scope. RLS changes only that exact read branch; subject-owned writes retain their
checks. An audited read commits before any successful response, attributing the
verified service account. Unknown/wrong/withdrawn/proxy claims all return false.
Dependency failure is unavailable, not a successful negative observation. No
claim observation is cached or inferred from broker delivery.

Reuse the existing workload trust boundary instead of introducing a custom
service-signing protocol or forwarding a patient's browser token. Cross-module
SQL would violate ownership; an asynchronous projection would retain the
withdrawal race. The original freshness requirement is preserved.

The [specification](../superpowers/specs/2026-10-25-mediconnect-current-claim.md)
records sizing, failure behavior, transport/activation gates and residual trust in
a compromised canonical owner. This endpoint cannot authorize clinical access by
itself. New consumer/client wiring, scoped projected credentials, TokenReview RBAC,
strict transport/network policy and actual consent/release enforcement must be
verified before production activation; no policy or consent is seeded here.
