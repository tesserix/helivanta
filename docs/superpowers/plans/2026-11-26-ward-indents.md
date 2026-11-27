# Ward indents, slice 1 (patient indents) — plan

Issue: #1682. Design: [ward indents](../specs/2026-11-26-ward-indents-design.md).
Status: in delivery. Slice 2 (ward-stock indents) follows in its own PR; #1682 stays open
until both are deployed.

## Fixed points

- Migrations: Pharmacy `0077_pharmacy_ward_indents`, IAM `0058_iam_pharmacy_ward_indent_templates`.
- Capability: new `pharmacy_ward_indents` (owner Pharmacy, depends on `pharmacy_counter`),
  compiled ineligible. Every new route, both pages and the
  `staff.assignments.pharmacy_ward_indent` panel map to it.
- Assignment family `pharmacy_ward_indent`, catalog `department-pharmacy-ward-indents-v1`.
- Hospital work purposes in `pkg/authz` are a closed set (`StocktakeWork`, `WardIndentWork`).
  Their fields are unexported, so no other package can mint a third purpose.

## Tasks, each red before green

1. **authz purposes.** `WardIndentActorUse`, `HospitalWorkPurpose` with
   `ReadScopeChecks`; `StocktakeReadScopeChecks` delegates unchanged. Tests: the zero purpose
   and each wrong permission/grant shape refuse; stocktake tests stay as they are.
2. **Workforce and Organization readers.** `wardIndentActorGuard` (purpose allowlist in
   `withCurrentEmployeeActor`), hospital and department name readers generalised over the
   purpose, with purpose-specific audit actions. Platform wiring, `cmd/api`, test harness.
3. **Templates.** Generator family `pharmacy_ward_indent`, contract entries with digests,
   IAM `0057` wrapped by before/after seal checks as `0056` is, regenerated outputs and
   append fixture, `.gitleaks.toml`, migration test (unchanged, tampered template source,
   tampered unrelated guard).
4. **IAM listings.** `/me/hospital-ward-indent-scopes` (the stocktake listing generalised over
   the purpose) and `/iam/pharmacy-ward-indent-assignment-proposals`. Tests: reader/requester
   rows only, stocktake rows excluded, missing actor grant excluded, dependency failure `503`.
5. **Release.** Capability, routes, pages and panel in `contracts/release-capabilities.json`;
   regenerate; test that provider-on still leaves the family locked.
6. **Schema 0076.** Three tables, forced RLS, outlet and requester-frame policies, purpose
   policies, catalogue purpose scope, triggers, seal checks before and re-pins after.
   Tests against the database: frame isolation, line immutability, transition guard, issue
   trigger (wrong occupancy, wrong tender, product not on indent, over-issue with the handler
   bypassed), cancel after issue, seal tamper refusal.
7. **Requester API.** Outlet choices, products, occupant, raise (`PUT`, replay-safe), list,
   get, cancel. Tests: every refusal in the spec's authority, outlet and raise lists.
8. **Pharmacist API.** Queue, get, issue (`postSale` extracted from `putSale` with no
   behaviour change), close short. Tests: partial issues to closure, over-issue, stay ended,
   price changed, replay, return through the credit note lowers the balance.
9. **Contracts.** Input models in `contracts/pharmacy.json`, events, `contractdocs`,
   `docs/api/domains/pharmacy.md`.
10. **UI.** `/pharmacy/ward-indents` and `/pharmacy/indents`, `zones.ts`, ward-board link,
    staff assignment family label, Vitest tests.
11. **Focused CI.** `.github/workflows/ward-indents.yml` running the affected Go packages and
    shell tests; then the protected checks.

## Not in this plan

Ward-stock indents, in-charge approval, substitution, e-prescribing, out-of-custody returns
and indent MIS, as listed in the design.
