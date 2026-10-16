# Country policy implementation plan

Child #1182, parent #1134, epic #1133. Specification: [reviewed country policy authority](../specs/2026-10-16-country-policy-authority.md).

- [x] Exact-scope evaluation with independent-policy, expiry and incomplete-review denial tests.
- [x] Additive migration, platform authority, forced RLS, immutable provenance, explicit scope grants and independent review against real PostgreSQL.
- [x] Bounded configuration/review APIs, cursor pagination, replay/conflict controls, audit and transactional outbox.
- [x] Shared Settings UI, form focus, native UTC input and display timezone; real owner/reviewer browser acceptance, desktop and 390px English layout.
- [x] Full local Go race/coverage, web tests/type checks/lint/formatting, build and security checks; exact database audit/outbox and real JetStream evidence.
- [ ] Hosted PR checks and review of named migration `0002_workspace_country_policies`.
- [ ] Specific production migration/rollout approval; merge, release and deployed acceptance for #1182.
- [ ] Integrate restricted listing/order/dispatch/dispense inside business transactions; verify all negative bypass paths before closing #1134.
- [ ] Urdu/RTL, country regulatory review and country-specific adapters remain parent acceptance.

Local delivery evidence and code recovery: [country-policy-1182.md](../../delivery/country-policy-1182.md). No automatic regulatory validation, payment processing or Temporal reminder workflow is claimed by this slice.
