# Scoped Staff reviewer bootstrap implementation

Design: [Staff governance bootstrap](../specs/2026-11-13-staff-governance-bootstrap.md).
No production migration or grant is authorized by this plan.

1. Confirm current canonical role/template generation, owner/topology providers,
   guarded-command consumers and latest RLS functions (not an earlier migration
   later replaced by another). Trace every affected symbol before changing it.
2. Add canonical governance command/receipt/review/notice contracts and fixed
   templates, with generator parity and strict input tests observed failing first.
3. Implement the additive IAM source ledger and real-PG tests for immutable intent,
   owner-count locking, exact target membership, provisional provenance, separate
   decisions, all-owner inbox notifications and outbox atomicity. Implement the
   multi-hospital topology admission invariant at its owner through a closed
   current-source guard. Do not add an exception to independent-approval records.
4. Implement source-fenced governance authority and its bounded FGA projection.
   Prove stale/removed tuples cannot authorize. Add the closed architecture gates
   before wiring Workforce consumers; no direct cross-module access.
5. Implement the full lifecycle consumer matrix in the design, including exact
   assignment review/settlement, nonclinical person/employment preparation, review
   and first activation, and independent operating-duty review. Preserve owner
   security/onboarding-only checks; represent governance actor provenance rather
   than inventing Staff actor references. Complete reviewers' real employment
   before ordinary posting/pharmacy relationship review.
   Update hospital and independent-employer account-link RLS/handlers for the
   exact nonclinical purpose, with concurrency, foreign-scope, revoked-membership,
   dependency-failure and callback-escape tests. Preserve existing owner behavior.
6. Add owner delegation, notification and independent auditor UI through Global
   API. Retain IDs on uncertainty, show provisional versus independently approved
   authority, and never enable issuer/recipient self-approval. Test both employers.
7. Run contractdocs/canonical generators and affected local unit/lint/type/build
   checks. Execute database/FGA/E2E cases remotely; remove generated local builds.
   Present the concrete migration/rollback evidence for specific deployment
   approval, then merge, promote signed images/chart through Kargo and verify.
8. Use actual authorized recipients and independent reviewers in the preserved
   tenant. Complete Staff links, employment/postings, hospital-owned pharmacy
   relationships and exact positive/negative receiving/read access. Record live
   evidence separately from CI. Keep the overall Staff goal active until done.
