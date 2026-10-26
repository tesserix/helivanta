# PharmaConnect zone width Implementation Plan

**Goal:** Every PharmaConnect page uses one content width.

**Spec:** [pharmacy zone width](../specs/2026-10-26-pharmacy-zone-width-design.md) (#1366)

### Task 1: Width is declared per zone

- [x] `zones.test.ts` "zone width": only `pharmacy` is full width. Fails while width is per page.
- [x] `hms-shell.test.tsx`: `/pharmacy` (no pane) and `/pharmacy/accounts` are `data-width="full"`;
      `/patients` is capped. Fails while width is per page.
- [x] Move `width?: "full"` from `ZonePage` to `Zone`, set it on the pharmacy zone, and drop the
      six page-level declarations. `HmsShell` reads `zone.width`.

### Task 2: Pharmacy content fills the width

- [x] `navigation.spec.ts` "every pharmacy page's content fills one width" at 1920px. Fails on
      Medication list (672 of 1536).
- [x] Drop `max-w-2xl` from the Medication list panel.
- [x] Update `docs/standards/frontend.md` and mark D8 and E0 superseded.
