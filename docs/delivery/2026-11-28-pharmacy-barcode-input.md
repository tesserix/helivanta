# Issue #1688 — desktop barcode input

Base: `9f235f4c` (fetched `origin/main`, 2026-11-28). Implementation is uncommitted in the isolated `helivanta-barcode-1688-20261128` worktree. The original checkout retains only its two pre-existing inventory design files.

## Acceptance and ownership

| Issue requirement                                              | Implementation and evidence                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| -------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Counter and receiving accept USB/Bluetooth keyboard scanners   | A labelled Barcode field accepts keyboard entry and Enter, with a Find product button. Component tests exercise keyboard submission in both screens. Physical hardware has not been attached or tested.                                                                                                                                                                                                                                                                |
| A GTIN finds an outlet product and adds it to the sale/receipt | Exact outlet links resolve first. Otherwise the existing drug-master catalogue boundary performs exact GTIN lookup, matched to the current imported product **and formulation** versions at that outlet. A real PostgreSQL/Global API journey checks initial lookup, new pack publication and formulation revision. Counter scans add a full catalogue pack in base units; receiving selects the product, then uses the existing exact-lot/quarantine receipt command. |
| Unknown codes show a message and offer linking                 | Only `barcode_unknown` opens the paged active-product linker. Catalogue writers with editor access can link; others are directed to an editor. SKU, brand, manufacturer and pack facts help compare the physical label. Failed saves retain the product and barcode for retry.                                                                                                                                                                                         |

`GET` / `PUT …/catalogue/barcodes/{gtin}` require existing catalogue read/write permissions and current outlet grants. Migration `0077_pharmacy_barcodes` adds the tenant/outlet/GTIN key, composite product FK, forced RLS, immutable links and audit evidence. Equivalent GTIN-8/12/13/14 forms share a GTIN-14 key. The existing drug-master check-digit rule is shared through `pkg/gtin`.

No name/SKU guessing, automatic catalogue creation or clinical rule is introduced. Prescription selection remains explicit for a new regulated line; repeated packs retain an already chosen prescription. Unavailable/ambiguous/inactive matches add nothing. Scan results are cancelled across outlet changes and payment holds. A changed basket invalidates pricing, including late quote responses. Scanning never posts a sale, payment or stock movement by itself.

Both routes remain classified under the existing `pharmacy_catalogue` release capability. No release capability, pilot, deployment or live migration was enabled. Camera scanning (#569), supplier-shipment receiving and independent operating-duty receiving retain their separate contracts.

## Validation

Confirmed on the final implementation:

- Four affected UI suites: **107 tests passed**; the final scanner suite independently passed all **8 tests**.
- Barcode PostgreSQL/RLS/concurrency tests plus shared GTIN validation passed with the race detector.
- The real Global API/DB import journey passed with current pack and formulation revisions.
- `make lint-go`, `go build ./...`, affected `go vet`, focused architecture checks, canonical generator checks and `contractdocs -check` passed.
- The affected frontend lint/typecheck/build/format tasks passed (7/7). The production build used explicit synthetic CI auth values and a local Global API origin; this is build evidence, not a deployed login check.
- Shared frontend suites passed: API 2,085 tests, UI 373, configuration 106 and emergency access 9. The final full shell rerun passed **2,466 tests across 279 files**.

The broad backend coverage attempt encountered a concrete local infrastructure failure before application assertions:

```text
docker info: Get .../v1.54/info: EOF
rootless Docker not found, failed to create Docker provider
```

The coverage floor and repository-wide backend suite are therefore not established by this work. Failing coverage shards were not skipped or treated as passing. Focused checks passed after a runtime restart. The runtime and all its Docker data were then deleted at the user's request; no further Docker/Colima use is authorized.

A one-line root development dependency on the already-used `@types/react` version fixes published UI declaration resolution. The type-resolution problem was reproduced in the starting checkout; normal typechecking passes with the declared dependency. The lockfile change is limited to that importer entry.

Representative regressions were observed failing before correction: missing GTIN route/master lookup (404), pack conversion (1 instead of 10 units), repeat prescription quantity (10 instead of 20), late quote restoration, and loss of the selected product after a failed link. The final scanner test also rejects oversized input without truncating it into a potentially valid code.

Browser login, physical scanner hardware, deployment and qualified clinical/regulatory acceptance remain unverified. Component receipt tests verify the actual emitted request, not a live posted receipt; the real API/DB barcode journey is reported separately.

Cleanup: the task-created Colima profile, VM and Docker data were deleted, and the Docker context was restored to `default`. About 6.7 GB of runtime/temporary build files were removed, followed by generated application build outputs. Source, dependencies and the original inventory design files were preserved. No commit, push or deployment was performed.
