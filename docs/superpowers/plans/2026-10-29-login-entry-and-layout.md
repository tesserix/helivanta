# Login entry and layout — plan

Spec: `docs/superpowers/specs/2026-10-29-login-entry-and-layout.md`
Issue: #1431

1. **Red tests.** Add the D1 cases to `login.test.tsx` and the D2/D3 cases to
   `branding.test.tsx`. Run them and see them fail.
2. **D1.**
   - `LoginPageContent` reads `start` and passes `autoStart` to
     `RedirectLanding`.
   - The existing mark-reading effect starts sign-in when no mark is present,
     after a `history.replaceState` that drops `start`.
   - Point the three entry links at `/login?start=1`.
3. **D2/D3.** In `branding.tsx`:
   - add the `homeHref` prop and the mark link;
   - remove the back link, the eyebrow, the accent span and the lock tile;
   - tighten the panel's spacing.

   `TenantLoginLayout` passes `homeHref` when no tenant branding resolved.
   `RedirectLanding` uses `TenantLoginHeading`.

4. **Measure.** Check document height at 1280x720 and 1440x807, take
   screenshots of both login states, and walk the click path from the home
   page.
5. **Pipeline.** Run `pnpm turbo lint format:check type-check test build`,
   then open the PR with "Closes #1431".
