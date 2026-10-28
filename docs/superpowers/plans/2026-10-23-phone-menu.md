# Phone menu: implementation plan

**Spec:** `docs/superpowers/specs/2026-10-23-phone-menu-design.md` (#1299)

One task. The change is one component, `packages/ui/src/hms-shell.tsx`, plus its tests.

1. Tests first:
   - `hms-shell.test`: "Open menu" opens dialog "Menu" with the zones, the PharmaConnect pages
     and the "Inventory" heading, with `aria-current` on the current zone and page.
   - Escape closes it, and focus returns to "Open menu".
   - Choosing a page link closes it.
   - Loading shows placeholders.
   - `phone-navigation.spec`, rewritten for the menu: no visible `aside.hms-rail` at 390px,
     main content x ≤ 24, and Staff → Shifts reached through the menu with the menu closed
     afterwards.
   - Run, watch fail.
2. Implement:
   - Add a local `PhoneMenu` (backdrop button plus `role="dialog" aria-modal` drawer, `md:hidden`).
   - The header button replaces "Expand page panel".
   - The rail and page panel become `hidden md:flex`.
   - Remove `pagesOverlayOpen`, the phone spacer and the shared backdrop.
   - `closeOverlays` becomes closing the menu.
3. Gates: `pnpm turbo lint type-check test build format:check`, then e2e on a production build.
