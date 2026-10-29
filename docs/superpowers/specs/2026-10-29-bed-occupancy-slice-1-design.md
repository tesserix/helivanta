# Beds and occupancy, slice 1

Resolves part of #106 (bed management). Unblocks #1387 (pharmacy billing to the admission in a
bed), alongside the admission account in #197. Applies the [bed and admission
draft](../../api/domains/beds-and-admissions.md) and IN-CL-02, IN-CL-03, IN-CL-07 and IN-CL-09.

## Why

MediCore has outpatient (OPD) encounters only. No record says which beds a hospital has or who
is in them. Pharmacy cannot bill a ward request to an admission, and nothing downstream can
trust a bed's state.

## Decisions (accepted 2026-10-29)

1. **Direct admission into an available bed.** There are no reservations or hold expiry in this
   slice. An admission names a patient registered at that hospital (`medicore_patients`) and is
   made by an authorised user. Choosing a bed or taking a payment is not an admission.
2. **Who may admit, transfer and release.**
   - The new permission `medicore.admission.manage`, held by the receptionist and nurse roles,
     plus the caller's MediCore grant at that hospital, checked in the same transaction (as
     encounters are).
   - Each admission records the actor and its intent: `elective` or `emergency`.
   - No admitting consultant is recorded, because that needs verified practitioner privileges.
3. **A stay ends only by release from the bed,** with a required reason:
   - `discharged`;
   - `transferred_out` (to another hospital);
   - `died`;
   - `left_against_advice`;
   - `other`, which requires a note.

   These record the departure and decide nothing else. Clinical discharge (#96), departmental
   clearances and billing close are separate facts.

4. **A released bed is never immediately available.** It moves to `cleaning` until a ward user
   marks it ready. An empty bed can be taken out of service (with a reason); it returns to
   service through `cleaning`.
5. **The hierarchy is ward → room → bed, owned by MediCore.**
   - Bed classes are those of #106: `general`, `semi_private`, `private`, `icu`, `hdu`,
     `isolation`, `neonatal` and `day_care`.
   - Codes are unique per hospital, and a bed's identity never changes.
   - Gender, ICU and occupancy-inclusion rules are #1043's, which is shared with Sam123ben.
   - Buildings and floors are a later parent of wards.

## Model

| Table                                               | Holds                                                                                              | Rules                                                                                                                                                        |
| --------------------------------------------------- | -------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `medicore_wards`, `medicore_rooms`, `medicore_beds` | The hierarchy; a bed's class, `state` and `version`                                                | Identity immutable. `state` is one of `available`, `occupied`, `cleaning` or `out_of_service`, and moves only with a transition row for exactly that version |
| `medicore_bed_transitions`                          | Every bed state change: from, to, reason, actor and time                                           | Append-only. A trigger allows only the legal moves                                                                                                           |
| `medicore_admissions`                               | Patient, hospital, intent, `admitted` or `released`, the release reason and note, actors and times | One active admission per patient per hospital                                                                                                                |
| `medicore_bed_occupancies`                          | One row per stay in a bed: bed, admission, `started_at`, `ended_at`, end (`transfer` or `release`) | No overlapping intervals per bed (gist exclusion on `tstzrange`). At most one open occupancy per bed and per admission. `ended_at` is set once               |

**Legal bed moves:**

- `available → occupied` (admit, or transfer in);
- `occupied → cleaning` (release, or transfer out);
- `cleaning → available` (ready);
- `available` or `cleaning` → `out_of_service` (with a reason);
- `out_of_service → cleaning` (returned to service).

Each command runs in one transaction:

1. Take the caller's grant.
2. Lock the beds involved in id order, with `FOR UPDATE`.
3. Check the expected versions.
4. Write the occupancy and transition rows.
5. Publish to the outbox.

A transfer closes the source occupancy and opens the target's at the same `clock_timestamp()`.

## HTTP

All routes are under `/v1/medicore/hospitals/:hospital`. All need the MediCore hospital grant;
without it the answer is `404`. Ids are client UUIDv7s and every command is idempotent: the same
id with the same body replays, and with a different body is `409`.

**Setup** (`medicore.bed.configure`, tenant administrators only):

- `PUT wards/:id`
- `PUT wards/:ward/rooms/:id`
- `PUT rooms/:room/beds/:id` `{code, bed_class}`

**Board** (`medicore.visit.read`):

- `GET beds?ward_id=`: a page of beds with state, version, ward and room, and the current
  occupancy's admission and patient ids.
- `GET wards`

**Admission** (`medicore.admission.manage`):

- `PUT admissions/:id` `{patient_id, bed_id, intent, expected_bed_version}`
- `PUT admissions/:id/transfers/:transfer` `{to_bed_id, expected_bed_version}`
- `PUT admissions/:id/release` `{reason, note}`
- `GET admissions/:id`, which includes its occupancies

**Bed state** (`medicore.admission.manage`):

- `PUT beds/:id/transitions/:transition` `{to: "available"|"out_of_service"|"cleaning", reason,
expected_version}`, for the moves that are not admission moves.

**Who occupies a bed or room now** (`medicore.visit.read`):

- `GET beds/:id/occupant` and `GET rooms/:id/occupant` return
  `{admission_id, patient_id, bed_id, occupancy_id, started_at}`.
- A bed with no occupant is `404 no_occupant`.
- A room with more than one occupied bed is `409 ambiguous`, naming no patient and never guessing.

Refusals, all `409`:

- `bed_not_available`: the bed is not `available`.
- `stale_bed`: the version moved.
- `already_admitted`: the patient already has an active admission at that hospital.

## Events

Same tenant. Ids and times only, no names.

- `helivanta.in.medicore.bed_occupancy_started.v1` `{occupancy_id, admission_id, patient_id,
hospital_id, ward_id, room_id, bed_id, started_at}`
- `helivanta.in.medicore.bed_occupancy_ended.v1` `{occupancy_id, admission_id, hospital_id,
bed_id, ended_at, end}`
- `helivanta.in.medicore.admission_released.v1` `{admission_id, patient_id, hospital_id,
released_at, reason}`

#1387 will build pharmacy's occupancy projection from these, because modules never read each
other's tables.

## UI

The MediCore IPD page, currently a placeholder, becomes a ward board:

- a hospital picker;
- beds by ward and room, showing state and occupant (the patient name comes from patientmaster
  under the caller's own patient grant, as OPD does);
- admit from a registry search;
- transfer to an available bed;
- release with a reason;
- mark ready, and take out of or return to service.

Ward, room and bed setup is a separate admin panel.

\1- A freshness indicator on the board (IN-CL-09). The board refreshes after every command and the server rechecks each bed under its lock, so a stale board can mislead the eye but never place a patient. Admit and transfer offer the available beds from the first 200 loaded.

- Reservations and hold expiry, blocking, housekeeping tasks and their evidence, and the live
  availability cache.
- The admitting consultant and clinical discharge (#96); gender, ICU and occupancy rules
  (#1043).
- Buildings and floors, and cross-hospital transfer.
- Bed-day charges and the admission account (#197).

## Tests that must exist

- Concurrency: two admissions racing for one bed; one patient admitted twice; a transfer racing
  a release.
- History: no overlapping intervals; a transfer's end equals its start; bed and occupancy
  history are append-only; illegal state moves are refused in the database.
- Authority: no grant gives 404; a reader cannot admit; another tenant sees nothing.
- The occupant query: empty, single and ambiguous.
- Events carry no names, and are published once per change.
