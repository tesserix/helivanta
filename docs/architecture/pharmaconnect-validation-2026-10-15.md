# Pharma Connect local verification — 15–16 October 2026

> **2026-10-23, #1287:** Pakistan was removed; India is the only supported country.

This records local verification of the pharmacy/outlet, catalogue, quarantine receiving and operational expense/payable slices. It is not completion evidence for the entire Pharma Connect backlog, production identity verification, clinical dispensing approval, or SOC 2/HIPAA certification. No production migration or deployment was performed.

## Automated checks actually run

- Go 1.27.2: `go test -v -race -cover ./internal/modules/pharmacy/... ./internal/archtest/... -count=1` passed.
- `GOFLAGS='-p=2 -v' bash scripts/coverage-gate.sh` passed the complete backend suite and its coverage thresholds. Pharmacy coverage: 80.6%.
- After adding a dedicated financial event delivery test, the complete pharmacy package was rerun with `go test -v -race -cover ./internal/modules/pharmacy -count=1`: passed, 80.6% coverage. Real NATS JetStream delivered the opaque payable and settlement references without financial or payer data in the payload.
- After batching multi-line document inserts and adding atomic multi-line rollback coverage, the complete pharmacy/architecture race suites passed again; final pharmacy coverage: 80.9%. Explicit financial foreign keys retain history with `ON DELETE RESTRICT`.
- `go vet ./...`, `go build ./...` and the repository's Go 1.27-compatible `golangci-lint run`: passed.
- Node 24: `pnpm turbo run test lint` passed all nine tasks after the responsive financial layout change; shell: 198 tests. `pnpm turbo run build` passed; `pnpm turbo run type-check` then passed all four tasks.
- The pinned repository shellcheck gate passed against a byte-identical copy of tracked shell files in a Colima-accessible local cache. Running `make lint-shell` directly in the `/tmp` worktree first failed because that path is not mounted into the VM; no shell source was changed to work around the environment.
- The initial all-at-once frontend invocation exposed a Next generated-route-types race between build and typecheck. The final build and typecheck were run sequentially. No typecheck failure was waived.
- Go vulnerability review found no reachable vulnerabilities in the original dependency check. The production dependency audit retained its documented baseline advisory/allowlist; no new financial dependencies were introduced.

Disposable PostgreSQL 16 runs with the real non-BYPASSRLS application role. Tests cover independent pharmacies without a hospital module, optional pending affiliations, explicit outlet grants, wrong tenant/outlet/parent, revocation and unavailable access lookup, duplicate IDs/business keys, optimistic versions, immutable revisions/ledger, quantity bounds and concurrent receipts. Finance tests cover invalid money/date/link input, duplicate supplier documents/receipt costing/payment references, partial payment, credit, exact once-only reversal, concurrent overpayment prevention, direct SQL protections, atomic outbox, confidential role denial and migration reapplication.

Local Colima's Docker socket and dynamic port forwarding dropped during the first full run. A task-local SSH forwarding helper restored the test environment; the successful full backend gate was rerun after recovery. Other worktrees' servers and containers were preserved.

## Real browser acceptance

The local Next app used actual API handlers with disposable PostgreSQL and NATS, a signed synthetic test session and entirely synthetic data. There is no fixture fallback in application components and no permanent test-auth route in the product.

1. Catalogue MRP edited through the UI; the original revision remained accessible. Duplicate receiving source was refused and the stock balance stayed unchanged.
2. A synthetic standalone pharmacy received 25 capsules. The acquisition document recorded net INR 250.00 and recoverable tax INR 25.00, producing liability INR 275.00 and acquisition/stock value INR 250.00.
3. The UI recorded an external payment of INR 100.00. Both bill summary and history displayed paid INR 100.00 / outstanding INR 175.00, with payer, method, reference, date, reason and authenticated recorder. This test initiated no transfer.
4. The UI created a separate synthetic electricity expense for INR 125.25. The stored line retained exact minor units; focus moved to the saved document heading.
5. Switching to an ungranted outlet removed the prior outlet's confidential rows and returned the explicit unavailable-access state. No access grant was created as part of that negative test.
6. At 390×844, dropdown parents stayed within the content grid and the document width remained 390px. Financial tables became stacked rows: each table measured 234px within its 236px card container, replacing the original 780/840px horizontally scrolling layouts. Paid and outstanding amounts and stock costs remained visible. Navigation expanded and collapsed; the viewport override was reset afterward.
7. The public live landing and sign-in surfaces rendered with their current styles. The user's unspecified additional broken screen has not been identified; local fixes are not described as deployed live fixes. Legacy MedNet redirected to login during the financial review; further supplier/expense field comparison needs the user's renewed session.

The local fixture intentionally omits production Zitadel renewal configuration. Production authentication, entitlement provisioning, deployed migrations, provider settlements, GCS evidence uploads, statutory reports and clinical workflows need their own acceptance runs; synthetic local authentication is not evidence for those controls.

## Query plans and performance boundary

`EXPLAIN (ANALYZE, BUFFERS)` ran against the synthetic browser database under `SET LOCAL ROLE hms_app` and the actual tenant/pharmacy/outlet settings. The payable list used `pharmacy_payable_page` and `pharmacy_settlement_page`: 1.847ms. The batch audit used batch, stock, receipt-cost and settlement indexes: 1.383ms. These timings are small-fixture diagnostics, not load-test results or proof of the ADR's 500ms p99 target. Validate larger ledger cardinalities, batch lookup indexes and noisy-neighbour behavior before asserting production capacity.

Both migration series and immutable financial history are forward-only; reverting application code preserves the additive tables. Broader acceptance gates and ownership boundaries remain in the [design](../superpowers/specs/2026-10-15-pharmaconnect-backend.md), [API reference](pharmaconnect-api.md), [ADR 0024](../adr/0024-pharmacy-ownership-and-outlet-tenancy.md) and [ADR 0025](../adr/0025-pharmacy-cost-and-payable-audit.md).

## 16 October: India configuration and procurement reconciliation

The user's confirmed first release is a procurement network: each seller is the contracting party and owns its invoices. External payment recording/reconciliation comes first. Platform-managed marketplace transactions and payment-provider processing remain separate optional work. [ADR 0026](../adr/0026-pharmaconnect-country-and-trading-boundaries.md), the [requirements and role workflows](../requirements/pharmaconnect-india.md), editable network diagram and [legacy procurement field map](../requirements/pharmaconnect-legacy-procurement.md) record those boundaries.

The restored legacy session allowed read-only inspection of the entire Procurement sidebar, vendor registration, Brand Mapping and receiving form. Only field labels and workflow structure were retained. No actual customer, vendor, bank, tax, licence or contact values were copied into code, fixtures or documentation; no legacy save, approve, upload or stock/payment operation was performed. New epic #1133 has 18 child stories assigned to Sam123ben; #979 links it because GitHub's 100-child limit is already reached. Existing procurement stories were extended rather than duplicated.

Country metadata now has one generated source for IN/INR/Kolkata, locale/direction metadata and explicit regulatory-review status. Outlet API validation rejects unsupported country/timezone pairs. The UI uses country-specific currency defaults for new products/documents, while existing recorded currency remains unchanged. This is configuration metadata and API validation, not a database-backed approved regulatory policy or completed localisation. Statutory tax/licence rules and transaction enablement still require their dedicated implementation and review.

After integrating origin/main's workforce leave changes, `pnpm install --frozen-lockfile`, `pnpm turbo run test lint` (nine tasks; shell 212 tests), `pnpm turbo build`, then `pnpm turbo type-check` (four tasks, including generated country/pharmacy/workforce contract drift checks) passed. The additional currency tests also passed separately (three tests). Go 1.27.2 `golangci-lint run` reported zero issues. The temporary synthetic browser API fixture was removed from the source tree; no permanent authentication bypass or fixture route is included.

The complete backend `GOFLAGS='-p=2 -v' bash scripts/coverage-gate.sh` was rerun after that integration, using the task-local Docker/SSH test environment and Go 1.27.2. It passed the full race suite and every coverage threshold: pharmacy 81.0%, countryprofile 100.0%, workforce 80.3%. `pnpm turbo format:check` also passed all six formatting tasks. These are local checks; hosted CI and production acceptance are separate evidence.

## Hosted Linux retry precision regression

After the approved repository visibility change, hosted CI could start. Web, browser end-to-end, scripts, secret scanning and both image validation/publishing pipelines passed for the initial commit. The full Go job exposed a real idempotent-response difference: Go's Linux nanosecond creation time differed from PostgreSQL's stored microsecond timestamp. The macOS clock had hidden that difference locally. A test-only nanosecond clock reproduced both failing JSON replay comparisons locally before the fix.

Pharmacy organisation, outlet, affiliation, location, batch and movement creation now use the existing PostgreSQL timestamp default and its returned persisted value. No additional migration or relaxation of retry assertions was needed. Location/batch/outlet replay assertions extend the existing organisation/affiliation/movement checks, and all pharmacy tests now exercise a nanosecond application clock. The complete pharmacy/architecture/country race suites and Go linter passed after the fix. Hosted CI for the follow-up commit remains a separate required check.
