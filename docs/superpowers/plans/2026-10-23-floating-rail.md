# Floating rail and page panel: implementation plan

**Spec:** `docs/superpowers/specs/2026-10-23-floating-rail-design.md` (#1286)

One task: the change is a single component, its zone data and its tests.

## Task 1: Floating rail and panel

**Files:** `packages/ui/src/zones.ts`, `packages/ui/src/zones.test.ts`,
`packages/ui/src/hms-shell.tsx`, `packages/ui/src/hms-shell.test.tsx`,
`packages/ui/styles.css`, `e2e/tests/navigation.spec.ts`.

1. Tests first:
   - `zones.test`: every zone has a `railLabel` of 1–9 characters.
   - `hms-shell.test`: a collapsed rail shows "Pharmacy" under the PharmaConnect icon, and the
     link's name is "PharmaConnect".
   - Run both and watch them fail.
2. Add `railLabel: string` to `Zone`, with these values:

   | Zone             | `railLabel` |
   | ---------------- | ----------- |
   | Dashboard        | Dashboard   |
   | Patients         | Patients    |
   | MediCore         | MediCore    |
   | Lab              | Lab         |
   | PharmaConnect    | Pharmacy    |
   | EmergencyConnect | Emergency   |
   | Supply Connect   | Supply      |
   | Staff            | Staff       |
   | Settings         | Settings    |

3. `ZoneLink` collapsed: a 64px-wide column with the icon, then the `railLabel` at `text-[11px]`,
   truncated. The rail becomes `w-20`, and the phone spacer becomes `w-24`.
4. Rail and open panel from `md`:
   - `md:m-2 md:self-start md:max-h-[calc(100dvh-1rem)] md:rounded-2xl md:border md:shadow-sm`,
     on the `--sidebar-rail` background.
   - The panel uses `md:ml-0` so the two cards sit 0.5rem apart.
   - A closed panel has `md:m-0 md:border-0`, and `styles.css` gains the same in its closed rule.
5. e2e: the rail and panel bounding boxes are below the window height, with `y > 0`.
6. Gates: `pnpm turbo lint type-check test build format:check`, then Playwright
   `navigation.spec.ts`, `smoke.spec.ts` and `csp.spec.ts` on a production build.
