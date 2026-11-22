# Tally accounting export delivery (#1690)

Spec: [design](../specs/2026-11-21-tally-accounting-export-design.md),
[ADR-0076](../../adr/0076-pharmacy-tally-accounting-export.md).

1. Head grammar and labels: tests first for parsing, refusal of unknown or
   malformed heads, rate labels and the supplier reference.
2. Voucher encoder: tests first for the envelope, sign convention, two-decimal
   rupees, merged entries, ordering, XML escaping, the balance refusal and
   byte-identical output.
3. Migration `0062_pharmacy_tally_ledger_maps` with forced RLS, outlet grant,
   audit, immutability, entry count and the single-successor key. Canonical
   input models in `contracts/pharmacy.json`, regenerated.
4. Routes under `/accounts/tally` with `pharmacy.accounts.export` (owner only).
   Map PUT/GET (replay, stale or second successor 409, merge, 1,000-entry
   bound), preview and `vouchers.xml`. Native tests cover cross-tenant and
   foreign-outlet 404, missing permission, unmapped 409, non-INR 409, 413
   bounds, audit failure leaving no file, and identical
   bytes on repeat.
5. Contracts and docs: release capabilities (`pharmacy_payables`, panel
   `pharmacy.tally_export`), contractdocs, API reference inventory,
   Staff-access inventory and reviews, the unpaginated-GET allowlist, the
   operational-roles test and the pharmacy domain document.
6. SDK and UI: fetch helpers with tenant pinning and strict filename and
   content-type checks; a **Tally export** tab on Supplier bills (period,
   counts, ledger-name form, download), with Vitest coverage.
7. Gates: `make lint-go`, the coverage gate, `go test -race ./...`,
   `contractdocs -check`, the generators' `--check`, and `pnpm turbo lint
type-check test build format:check`. Remove the build output afterwards.
8. Follow-up issue for supplier purchase vouchers, which needs purchase GST
   capture. Real-TallyPrime import and migration rollout approval remain open
   review items.
