# #169 execution and evidence

Follow the [completion specification](../specs/2026-11-26-pharmacist-verification-completion.md) and [full acceptance matrix](../../design/pharmacist-verification.md).

- [x] Verify main and actual issue state; reopen the incorrectly closed issue.
- [x] Build recorded-source review UI using the audited, account-bound SDK.
- [x] Observe failing component assertions before implementation; cover names, unavailable facts, scoped reads, refresh failures, account/outlet changes and late responses.
- [x] Verify unreleased UI and API gates; run focused tests, typecheck, lint and affected build. Remove artifacts created by validation.
- [ ] Complete professional authority and signed-source integration.
- [ ] Complete queue, decisions, receipts, second checks, prescriber loop and patient status.
- [ ] Enforce current approval/authority in actual supply transactions, including stock/financial effects.
- [ ] Resolve and implement approved emergency policy and retrospective workflow.
- [ ] Pass full acceptance, required checks and deployment verification before activation and closure.

Keep tested, merged, deployed and live-accepted states separate. The first unchecked item does not replace the remaining goal.

## Local review-workspace validation

Final checks used Node 24.11.1; no local application, database, Docker or Colima started.

- Initial focused component run failed because the new component did not exist.
- Shell: `pnpm --filter @helivanta/shell exec vitest run components/pharmacy/prescription-review-context.test.tsx components/pharmacy/prescriptions.test.tsx components/pharmacy/pharmacy-workspace.test.tsx` — 64 tests passed across three files.
- SDK: `pnpm --filter @helivanta/api exec vitest run src/pharmacy-review-context.test.ts` — four tests passed.
- Release policy: `node --test scripts/release-capabilities.test.mjs` — passed.
- Scoped ESLint on the changed shell files and shared API query file — passed after replacing a custom refresh button with the shared `RefreshButton` and correcting unused test arguments.
- `pnpm --filter @helivanta/api type-check` (including its generator checks) and `pnpm --filter @helivanta/shell type-check` — passed.
- `pnpm --filter @helivanta/shell build` — passed using non-routable build-check configuration; generated build artifacts are removed after validation. This was a compilation check, not a running local app or a deployable production configuration.

These results cover the recorded-source review candidate only. Protected CI,
merge/deployment and live clinical workflow acceptance remain separate gates.
