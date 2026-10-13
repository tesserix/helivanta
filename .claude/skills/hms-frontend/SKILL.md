---
name: hms-frontend
description: Use when writing or modifying any Helivanta frontend code (apps/*, packages/ui, packages/api) — loads the binding standards, reference implementations, and known pitfalls.
---

# Helivanta frontend standards

Read `docs/standards/frontend.md` for the full rules. The short version and where to copy from:

| Concern       | Rule                                              | Reference                                     |
| ------------- | ------------------------------------------------- | --------------------------------------------- |
| Data          | useApiQuery/useApiMutation from @helivanta/api          | apps/shell/components/medicore/visit-panel.tsx |
| Forms         | useZodForm + Field, noValidate, inline errors     | apps/shell/app/login/page.tsx                 |
| Feedback      | sonner toasts; ConfirmDialog for destructive only | packages/ui/src/confirm-dialog.tsx            |
| Empty/loading | EmptyState + app/loading.tsx skeletons            | apps/shell/components/pharmacy/dispense-list.tsx |
| Nav           | next/link; registry packages/ui/src/zones.ts      | packages/ui/src/hms-shell.tsx                 |
| Authz         | Can/usePermissions gate UI, convenience only — the API enforces; every zone/page in zones.ts declares a permission | packages/api/src/permissions.tsx              |
| Tokens        | no hardcoded colors; read styles.css pitfalls     | packages/ui/styles.css                        |
| New zone      | route segment in apps/shell/app/<name>/ + zones.ts entry | apps/shell/app/pharmacy/            |
| Tests         | renderWithProviders per panel                     | apps/shell/components/medicore/visit-panel.test.tsx |

Never: alert/confirm/prompt, native form validation, raw fetch in components, a plain <a> for an in-app route, hex colors in classNames, treating `can()` as a security boundary.
