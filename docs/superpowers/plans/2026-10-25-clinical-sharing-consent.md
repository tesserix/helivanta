# Clinical sharing consent implementation

1. Observe owner client protocol/security failures, then add the canonical typed
   HTTP client and explicit optional composition wiring. Test token rotation,
   strict response/body bounds, redirects, unavailable dependencies and deadlines.
2. Observe dedicated HMAC key creation/verification failures against real OpenBao;
   implement consent-only namespace support and separately scoped policy. Do not
   change a live policy or reuse staff/emergency keys.
3. Implement reviewed named-recipient binding with independent approval/revocation,
   exact tenant/hospital/principal RLS and immutable history, using TDD.
4. Implement explicit signed self grant, reconciliation, patient receipt/list and
   dependency-independent withdrawal with atomic audit/outbox. Recheck local
   authority/notice/recipient/registration versions after network operations.
5. Implement named-recipient consent gate, tamper quarantine/security incident and
   tests for expiry, wrong scope, stale state, concurrency and dependency failure.
6. Exercise real HTTP Global API -> patientmaster -> live MediConnect owner, real
   Postgres/OpenBao and fake external Kubernetes API only. Preserve unsigned/
   unreleased-record denial until the actual record owner workflow is complete.
7. Update contracts, permissions, schemas, events, independent gates and delivery
   evidence; run generators, race/coverage, migration/replay/old-binary and bounded
   query-plan checks. Review infrastructure for explicit projected credentials,
   TokenReview permissions, dedicated crypto policy and strict transport before
   named production activation. Broad stories stay open until all their criteria,
   signed rendering, proxy, record release and real approved wording are met.
