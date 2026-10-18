# Implementation plan — #1229

1. Observe failing public API test, then add contract, canonical validation,
   additive migration and immutable persistence.
2. Prove list/stock projection, bounds, old-schema upgrade, audit/concurrency
   and tenant/outlet controls against PostgreSQL.
3. Observe failing UI test; add optional labelled fields and shared address
   formatting for choices/register, preserving retry/scope behavior.
4. Run Go race/coverage/lint/build and web tests/typecheck/lint/format/build;
   exercise genuine local API/browser flows with synthetic records.
5. Push reviewable PR, request named migration/release approval, deploy
   through GitOps and close only after production verification.
