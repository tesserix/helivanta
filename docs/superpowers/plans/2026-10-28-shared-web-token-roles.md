# One set of web colour tokens — plan

Spec: `docs/superpowers/specs/2026-10-28-shared-web-token-roles.md`
Issue: #542

1. **Baseline.** Record the D5 computed-colour capture of `/` at 1440px and
   390px on the unchanged branch. Capture it twice and confirm the two runs
   are identical, so that later differences come from the change and not from
   the capture.
2. **Gate first (red).**
   - Add `packages/ui/src/token-source.test.ts` (D4).
   - Extend `contrast.test.ts` with the D3 pairs.
   - Run both. The stylesheet check must fail on `.home-slate`'s eleven
     colour declarations, and the new amber and violet pairs on `--accent`
     must fail on the current values. The undeclared-read check passes at this
     point, because `--home-*` is still declared. Step 5 proves that it can
     fail.
3. **Tokens (D2).** In `packages/ui/styles.css`, set `--hms-amber: #92400e`
   and `--hms-violet: #4f46e5`.
4. **Replace `--home-*` (D1).** Make one mechanical pass over the 14 files,
   using the spec's table. Theme utilities replace the arbitrary form where one
   exists; `var(--home-x)` in style objects and SVG becomes `var(--token)`.
   Then delete the `.home-slate` palette from `globals.css`, keeping
   `color-scheme` and the `border-color` revert-layer rule.
5. **Gate green.** Both tests pass. Prove D4 can fail: inject a
   `--home-ink: #0f172a` declaration in `globals.css` and a
   `text-(--home-ink)` read in a landing component, confirm both checks fail
   with messages naming them, then revert.
6. **Capture after.** Repeat the D5 capture and diff it against the baseline.
   The two must be identical.
7. **Docs (D6).** Update `CLAUDE.md`, `docs/standards/frontend.md` §7 and the
   one-look spec's D2 note.
8. **Pipeline.** Run `pnpm turbo lint format:check type-check test build`,
   then the `smoke` and `login-appearance` e2e specs if the stack is
   available. Then open the PR with `Closes` omitted, because #542 stays open
   for the rest of the epic, and with "Part of #542".
