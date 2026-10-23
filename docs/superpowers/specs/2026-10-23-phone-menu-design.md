# Phone menu: rail and page panel behind one header button

Resolves #1299. Builds on the sidebar spec (#1270) and the floating rail (#1286, #1296). This
replaces #1270 D4/D5 below `md` only; nothing changes from `md` up. Mock-up approved
2026-10-23.

## Problem

On a phone the zone rail is a permanent 64px column, about a sixth of a 390px screen. The page
panel opens from a separate header button. Two navigation controls compete with the content.

## Decisions

- **D1 No rail below `md`.** The rail and the page panel are not rendered on a phone, and the
  page uses the full width.
- **D2 One menu.**
  - A header button, "Open menu" (☰), with `aria-expanded` and `aria-controls`, opens a
    left-hand drawer: a modal dialog named "Menu".
  - The drawer lists every zone the user may see, grouped as on the rail, with the admin zones
    after a separator.
  - The current zone is expanded in place, showing its pages under their section headings
    (#1258). The current zone and page carry `aria-current="page"`.
- **D3 Closing.**
  - Choosing any link, tapping the backdrop, the close button or Escape closes the drawer.
  - On open, focus moves to the close button. On close, focus returns to "Open menu", unless
    the drawer closed because a link navigated.
  - While permissions load, the drawer shows placeholders, never a partial list (#1270 D2).
- **D4 From `md` up, unchanged.** The floating rail, the expand/collapse rail control and the
  always-shown page panel stay as they are. The stored rail-expanded flag has no effect on a
  phone.

## Not covered

- Swipe gestures to open or close.
- A bottom tab bar.

## Verification

- Vitest (`hms-shell.test`):
  - The menu lists the zones and the current zone's pages and sections, and marks the
    current ones.
  - Escape closes the drawer and returns focus.
  - Choosing a page closes the drawer.
  - While loading, the drawer shows placeholders.
- e2e at 390×844 (`phone-navigation.spec`):
  - No rail is visible, and the content starts within the screen padding.
  - The menu reaches a page in another zone and closes.
  - `dashboard-phone` still passes.
