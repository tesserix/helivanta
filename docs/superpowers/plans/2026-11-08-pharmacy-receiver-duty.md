# Pharmacy receiver duty implementation plan (#19 slice 3b-ii)

See the [specification](../specs/2026-11-08-pharmacy-receiver-duty.md). Each step
starts with a failing test, and each failure is shown before its fix.

1. **Policy (red first).** Write a Postgres test for
   `0038_pharmacy_operating_receipts`. It covers:
   - stock that stays invisible without the settings, plus an architecture test
     that only one file sets `app.pharmacy_stock_access`;
   - a mismatched subject, outlet or permission;
   - a `correction` or `release` attempt under the duty;
   - creating a location or catalogue product under the duty;
   - the append-only audit table, which rolls back with the write.

   Then implement the function, the `OR` branches and the audit table.
2. **Admission.** Give PharmaConnect handlers the `OperatingAuthorizer`
   dependency. Add `operatingStock`, which mirrors supply `managedStock`. It
   checks the exact scope and permission, sets the settings, restores them on
   exit, and writes the audit row.
3. **Routes.** Add the `operating-locations` family, reusing the existing batch
   and movement validation, locking and outbox code. A kind other than `receipt`
   is refused before the write.
4. **Acceptance.** Add a globalapi test mirroring the supply receiver test. It
   covers the end-to-end journey, replay, revocation cutoff, sibling, foreign and
   hospital-owned outlets, Staff/link change, credential revocation, expiry and
   graph-unavailable cases. The editor routes must be byte-identical.
5. **Contracts and SDK.**
   - Regenerate `contractdocs` and the canonical schemas.
   - Add `packages/api` hooks.
   - Add a receiver panel with `renderWithProviders` tests.
   - Update the API docs and the delivery ledger.
6. **Gates.**
   - Workforce, pharmacy, IAM and globalapi `-race`.
   - `make lint-go`.
   - `pnpm turbo lint type-check test build format:check`.
   - The inventory `--check`. The user must grant the exact re-pin command.
   - Remove the build artifacts.
7. **Release.** Open the PR with `Refs #19` and merge with `--admin` once it is
   green. Then follow the release with the Kargo stage `prod` in
   `kargo-helivanta`, the migrate Job and the health checks.
