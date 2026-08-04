# HMS — agent rules

Binding rules for all frontend work. Full document: docs/standards/frontend.md

- Data fetching: `useApiQuery`/`useApiMutation` from `@hms/api` only. Never raw fetch/useState/setInterval in components (sole exception: shell login session POST — see standards doc).
- Forms: `useZodForm` + `Field` from `@hms/ui`, `noValidate` on every form, inline zod errors. Native browser validation is banned.
- Feedback: sonner toasts (success verb matches the button verb). `ConfirmDialog` only for destructive confirmations. `alert`/`confirm`/`prompt` are lint errors.
- Navigation: cross-zone and sidebar links are plain `<a>`. Zone nav lives only in `packages/ui/src/zones.ts`.
- Styling: design tokens only — no hardcoded colors. Read the pitfall comments in `packages/ui/styles.css` before touching sidebar/border styles.
- New zone apps: run `pnpm new-zone <name>`; never hand-copy an app.
- Every new/changed panel component needs a Vitest test using `renderWithProviders` from `@hms/api/testing`.
- Before done: `pnpm turbo lint type-check test build` green; keep `e2e/tests/smoke.spec.ts` selectors working.
- Backend: modules under `backend/internal/modules/*` never import each other; tenant tables need forced RLS (see phase specs in docs/superpowers/specs/).
