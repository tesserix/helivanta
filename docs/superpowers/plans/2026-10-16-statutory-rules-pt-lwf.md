# Statutory rule sets (PT and LWF): plan

Spec: [PT and LWF rule sets](../specs/2026-10-16-statutory-rules-pt-lwf-design.md). Issue: #1041 slice 2b.

1. Migration `statutory_pt_lwf.sql` (`0022_workforce_statutory_pt_lwf`). It adds:
   - the widened kind CHECK
   - the PT and LWF params and slab tables
   - the replaced parameter-presence trigger
   - the deferred slab-completeness trigger
   - immutability, creation-transaction and RLS loops for the new tables
2. Contract `contracts/workforce-payroll.json`: PT and LWF params and slab models, the preview `gender`, and the new statuses. Then regenerate.
3. Calculator `statutory_calc.go`:
   - `calculatePT` and `calculateLWF`, plus a shared slab-partition validator used by the normaliser.
   - Boundary tests.
4. Backend `statutory_rules.go`:
   - Normalise and validate PT and LWF.
   - Insert params and slabs.
   - Load them back in slab order.
   - Preview results for the two new kinds, with a `gender` input.
5. Tests:
   - The real-PostgreSQL lifecycle for PT and LWF.
   - API 400 and trigger refusals for every completeness failure.
   - Immutability, plus audit and event payloads.
6. `packages/api` `payroll.ts`:
   - PT and LWF form schemas: rupees to paise, percent to ppm, months and gender sets.
   - The completeness check, mirrored with inline errors.
   - Status labels and tests.
7. Shell:
   - PT and LWF cards and slab editors.
   - The preview gender select and status labels.
   - Page tests.
8. `scripts/verify-statutory-rules.mjs` gains PT and LWF.
9. Docs: `docs/api/workforce.md`, the coverage doc and ADR 0029.
10. Gates, then a PR "Part of #1041". Once CI is green, admin merge and verify the rollout read-only.
