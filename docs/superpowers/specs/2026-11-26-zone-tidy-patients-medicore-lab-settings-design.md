# Patients, MediCore, Lab and Settings: tidy to the one-look design — design

Issues: #1256 (Patients), #1257 (MediCore), #1259 (Lab), #1263 (Settings). This applies #1261's design.
Date: 2026-11-26

## Why

None of these zones' files were on the surface legacy list (#1262 retires it), so the lint rule already holds their cards, eyebrows, display type, raw colours and pills. A survey on 2026-11-26 found what lint does not catch:

- **Local control classes.** Each of these files keeps its own `const inputClass` copy instead of the shared `controlClass`:
  - MediCore: bed-setup, cash-desk, cash-payments, discharge-order, discounts, opd-encounters, patient-safety, refunds, ward-board;
  - Patients: patient-workspace;
  - Settings: branding page, razorpay-account.
- **Tables without the shared style.** These four tables use neither the 44px rows nor right-aligned numbers:
  - MediCore daily collection shifts;
  - MediCore GST lines;
  - Patients list;
  - organisation structure.
- **Boxed rows.** MediCore has three boxed `rounded-md border p-*` rows.
- **Stale offsets.** Four `scroll-mt-24` offsets remain, in country-policy detail and editor.
- **Long description.** The organisation structure description runs to two sentences.
- **Lab.** Two files; the survey found nothing to change.

## Decisions

### D1 Reuse #1261's primitives, unchanged

D1–D3 of `2026-11-25-workforce-tidy-design.md` apply:

- `controlClass` replaces every local copy;
- `tableStyles` is used for tables, with `number` on amounts and counts;
- boxed rows become divided lists or `Panel` insets;
- `scroll-mt-24` becomes `scroll-mt-4`;
- a description is cut to one sentence. A cut sentence that states a real rule moves beside the control it governs.

### D2 One PR for the four zones

The remaining work is small and of one kind, so it ships together and closes all four issues. Lab closes on the survey result: it has nothing left to change.

### D3 Clinical and money values stay

MediCore and Patients show patient, bed, discharge, safety, billing, GST and refund data. The tidy changes classes only. No value, label, rounding, ordering, status or safety text changes.

## Not covered

- No behaviour, API, permission or copy change beyond the one description.
- Not checked visually.
