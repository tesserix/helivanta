# Worker outbox policy isolation plan

Refs #1135. Spec: ../specs/2026-10-16-worker-outbox-policy-isolation.md.

1. Reproduce clean Supply processing failure with all production migrations and
   the dedicated non-inheriting worker, asserting real object and outbox results.
2. Append the Workforce policy migration: common tenant predicate plus unchanged
   role-specific restrictive HR predicate. Keep Supply permissions unchanged.
3. Verify replay, foreign-tenant denial, clinical access denial and forged HR
   event rejection, plus existing HR/Supply lifecycle and full backend gates.
4. Prepare the reviewed PR and obtain approval for the named new production
   migration/rollout before merge; then verify the live queued workflow and UI.
