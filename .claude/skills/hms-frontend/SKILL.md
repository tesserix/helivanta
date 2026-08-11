---
name: hms-frontend
description: Use when writing or modifying any HMS frontend code (apps/*, packages/ui, packages/api) — loads the binding standards, reference implementations, and known pitfalls.
---

# HMS frontend standards

Read `docs/standards/frontend.md` for the full rules. The short version and where to copy from:

| Concern       | Rule                                              | Reference                                     |
| ------------- | ------------------------------------------------- | --------------------------------------------- |
| Data          | useApiQuery/useApiMutation from @hms/api          | apps/medicore/components/visit-panel.tsx      |
| Forms         | useZodForm + Field, noValidate, inline errors     | apps/shell/app/login/page.tsx                 |
| Feedback      | sonner toasts; ConfirmDialog for destructive only | packages/ui/src/confirm-dialog.tsx            |
| Empty/loading | EmptyState + app/loading.tsx skeletons            | apps/pharmacy/components/dispense-list.tsx    |
| Nav           | plain <a>; registry packages/ui/src/zones.ts      | packages/ui/src/hms-shell.tsx                 |
| Authz         | Can/usePermissions gate UI, convenience only — the API enforces; every zone/page in zones.ts declares a permission | packages/api/src/permissions.tsx              |
| Tokens        | no hardcoded colors; read styles.css pitfalls     | packages/ui/styles.css                        |
| New zone      | pnpm new-zone <name>                              | scripts/new-zone.mjs                          |
| Tests         | renderWithProviders per panel                     | apps/medicore/components/visit-panel.test.tsx |

Never: alert/confirm/prompt, native form validation, raw fetch in components, next/link for cross-zone hops, hex colors in classNames, treating `can()` as a security boundary.
