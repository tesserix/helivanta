# Public pages and login on the app's buttons and panels — plan

Spec: `docs/superpowers/specs/2026-10-29-one-shape-scale.md`
Issue: #1434

1. **Baseline.** Capture radius, height, weight and colour of every element
   on `/` (every story step), `/login`, `/workspace`, `/signup` and
   `/ambulance` on the unchanged branch.
2. **Gate first (red).** Add the D5 shape rules to `DESIGN_SYNTAX` in
   `packages/config/eslint.config.mjs` (so `hmsPublicPages` inherits them),
   with a case in `eslint.config.test.mjs`. Run the shell and ui lint: it must
   report the current pills and off-scale radii.
3. **Buttons (D2).** Public CTAs become `Button` (`asChild`); tags become
   `rounded-md`; story pickers become `rounded-lg`.
4. **Cards (D3).**
   - Public cards become `Panel`.
   - `LoginSurface`'s panel and organisation card take `rounded-lg`, with
     `rounded-md` tiles inside.
   - Landing boards: frame `rounded-lg`, inner nodes `rounded-md`.
5. **Login (D4).** Remove the login-only button shape, and point
   `--auth-radius` at `--radius-lg`.
6. **Gate green.** Lint passes. Inject a `rounded-3xl` and a pill CTA, see
   both fail, then revert.
7. **After capture.** Every radius is on the scale, colours are identical,
   and the primary-button table goes into the PR.
8. **Docs and follow-up.** Update `docs/standards/frontend.md` §7 and note
   the change on #1275's and #1255's specs. File the control-height
   follow-up.
9. **Pipeline,** then the PR with "Closes #1434".
