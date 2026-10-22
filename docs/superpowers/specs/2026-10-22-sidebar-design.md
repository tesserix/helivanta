# Sidebar: stable order, no partial list, collapse out of the list

Resolves #1270. Follows the one-look chrome (#1255, spec D4).

## Problem

- **Random order.** The zone rail renders `ZONES` in declaration order, which is
  the order zones were added: EmergencyConnect first, Settings mid-list.
- **Items pop in.** Until `/iam/me/permissions` answers, `can()` allows only
  `public`, so the server HTML and the first paint list Dashboard and Staff
  alone, and the shell falls back to the dashboard zone (heading "Dashboard" on
  `/patients`). The real list and heading replace them a moment later.
- **Collapse reads as a zone.** "Collapse navigation" is a full-width row styled
  like a zone link, above the zones.

## Decisions

- **D1 One order, grouped by use.** Every zone declares a `group`:
  - `overview`: Dashboard;
  - `care`: Patients, MediCore, Lab, PharmaConnect, EmergencyConnect;
  - `operations`: Supply Connect, Staff;
  - `admin`: Settings.

  `ZONES` is written in that order, and a test fails if the order or the
  group contiguity drifts. The rail separates groups with a divider. `admin`
  is pinned to the foot of the rail, away from the daily zones.

- **D2 Nothing partial while permissions load.** While `usePermissions()` is
  loading, the rail and the page panel render neutral placeholder slots
  (`aria-hidden`, nav `aria-busy`), never links. Fail closed: a link the user
  may not hold is never shown. The zone and the heading come from the route
  (`activeZone(pathname)`), not from the dashboard fallback. The server HTML
  is the loading state, so hydration matches. A cached permission set still
  applies before the first client paint (permissions.tsx), so repeat visits
  show the real rail straight away.
- **D3 Collapse is chrome, not a zone.** An icon-only button (`ChevronsLeft` /
  `ChevronsRight`) at the foot of the rail, under Settings, keeps its
  `aria-label`, `aria-expanded` and `aria-controls`. It never has a text label.

- **D4 Pages reachable on a phone.** Below `md` the page panel was always
  hidden, and its open button only showed when the desktop panel was closed,
  so a multi-page zone exposed one page. Below `md` the header button always
  shows for a multi-page zone. It opens the panel as an overlay beside the
  rail, with a scrim, and it never touches the persisted desktop preference.
  The stored-closed width rule in `styles.css` applies from `md` up only.
- **D5 Overlays close on a phone.** Below `md`, choosing a zone or a page, or
  tapping the scrim, closes the expanded rail and the page overlay. Desktop
  keeps its persisted state. (Found by the Fable review of the shell.)

## Not covered

- Zones the user cannot see still fall back to the dashboard after loading
  (unchanged).
- Per-zone page tidying is #1256–#1263.
