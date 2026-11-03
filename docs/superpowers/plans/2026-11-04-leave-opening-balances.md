# Plan: opening leave balances (#1537)

Spec: [opening leave balances](../specs/2026-11-04-leave-opening-balances.md).

1. **Red, backend.** In `leave_ledger_test.go`, assert zero balances with
   `in_structure` for a newly assigned staff member, refusal of an
   out-of-structure correction and of a correction before any assignment, and a
   successful opening credit. Run against Postgres and watch them fail.
2. **Contract.** Add `in_structure` to `leaveBalance` in
   `contracts/workforce-leave.json`; run `node scripts/generate-workforce-leave.mjs`.
3. **Green, backend.** `loadLeaveBalances` unions structure types in force on
   `as_of` with ledger sums in one bounded query. `putLeaveCorrection` checks
   the structure in force on `effective_on`, returning a new
   `errLeaveNotInStructure` (400 `invalid_reference`).
4. **Existing tests.** Update the 2025 balance assertions in
   `TestLeaveLedgerPostsCorrectsAndCarriesExactlyOnce`, which now list the
   structure's annual leave at zero. This is the intended behavior change.
5. **Frontend.** Gate **Correct balance** and its options on `in_structure` in
   `balances-panel.tsx`; Vitest in `page.test.tsx` covers the zero-ledger case.
6. **Docs.** `docs/api/workforce.md` leave balances section; regenerate
   contract docs with `go run ./cmd/contractdocs` and `-check`.
7. **Gates.** `make lint-go`, `./scripts/coverage-gate.sh`,
   `go test -race ./...`, `pnpm turbo lint type-check test build format:check`.
8. **Local acceptance.** On the dev stack, open a synthetic staff member with a
   new assignment and no entries, record an opening credit from the panel and
   file leave against it.
