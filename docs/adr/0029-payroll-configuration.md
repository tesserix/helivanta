# ADR 0029: Payroll configuration — salary heads, structures and statutory rules

Status: accepted for implementation. Parent: #1041 / #979; consumed by #1040. Spec (slice 1): [salary heads and structures](../superpowers/specs/2026-10-16-salary-heads-structures-design.md). Research: [Indian statutory landscape](../payroll-india-statutory-reference-2026-10-16.md).

## Requirements and evidence

Legacy Salary Setup, Tax Setup, Salary Template and HR Settings were reviewed read-only between 2026-10-12 and 2026-10-14 ([legacy research](../legacy-hr-policy-research-2026-10-14.md)). Indian payroll law changed repeatedly in 2025–26: the Labour Codes came into force with a 50% wage rule, the Income-tax Act 2025 renumbered salary TDS, and the EPF ceiling rose on 17 September 2026, mid-month. Planning assumptions: up to 2,000 staff per hospital, up to 60 heads per structure, a monthly run of 2,000 payslips within minutes, one PostgreSQL and no new service.

## Decision

1. **No statutory values are seeded.** Each hospital enters tax slabs, rates, ceilings and state levies with a citation, and they are versioned and attributed. The product owner decided this on 2026-10-16, and the issue requires HR/legal approval rather than copied defaults.
2. **Money is integer paise; rates are parts per million.** No floating point is used anywhere, and every rounding step is explicit in a fixed order: proration, then percentage, then rounding to the unit, then clamping to floor and cap.
3. **Effective dating is by day.** Configuration resolves to the revision in force on a date, so a payroll period can span two revisions (as the EPF change did) and the run apportions them.
4. **Structures reference heads, not head revisions.** An effective-dated head change applies to every structure that uses it. A deferred database validator proves, at every change date from the write onwards, that head graphs are acyclic, that bases are active earnings and that every active structure can be computed. A write that would break any of these is refused; a break is never discovered at run time.
5. **Every head declares a Labour Code wage class.** The calculator, not each statutory rule, derives statutory wages with the 50% add-back, so PF, ESI, gratuity and bonus all read one definition.
6. **One pure calculator** is shared by previews and payroll runs, so a preview is exactly what a run would compute for the same inputs.
7. **Historical runs keep their configuration** by storing the revision ids they used. A revision effective inside a processed payroll period is refused by a lock trigger added with #1040.

## Consequences

The hospital must enter and approve its statutory values before the first run; the shell says so plainly rather than silently using defaults. PF and ESI rules with maker–checker approval are delivered ([spec](../superpowers/specs/2026-10-16-statutory-rules-pf-esi-design.md)); separation of duties is a table constraint, not only a handler check. Monthly professional tax and LWF follow on the same lifecycle ([spec](../superpowers/specs/2026-10-16-statutory-rules-pt-lwf-design.md)); their slab tables must partition every wage, enforced by a deferred constraint trigger as well as the API. Income-tax rule sets for both regimes follow too ([spec](../superpowers/specs/2026-10-17-income-tax-rule-sets-design.md)): one revision per tax year whose date range is enforced, so a new year needs new rates rather than inheriting the old ones. Staff salary assignment and tax-regime elections follow ([spec](../superpowers/specs/2026-10-17-staff-salary-assignment-design.md)): a deferred check refuses any head or structure change that would leave an approved assignment uncomputable, and a missing election is reported rather than defaulted. Later slices add half-yearly PT, declarations and private bank/tax identifiers once field-level encryption is decided.
