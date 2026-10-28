# OPD encounter slice 1 Implementation Plan

**Goal:** a MediCore visit becomes an OPD encounter tied to one hospital and one patient registered there, with a lifecycle.

**Spec:** [OPD encounter slice 1](../specs/2026-10-27-opd-encounter-design.md) (#80)

### Task 1: Schema and patient projection

- [x] Migration `0005_medicore` (`encounters.sql`): `medicore_patients`, `medicore_hospital_access`,
      encounter columns on `medicore_visits` with a kind CHECK (legacy vs encounter), a composite FK
      to the projection, `medicore_encounter_transitions`, and guard triggers (identity immutable,
      status moves only with a matching attributable transition row, history retained).
- [x] Consumer `medicore-patient-registered` on `patientmaster.registered.v1`, idempotent.

### Task 2: Routes

- [x] Hospital grants: `PUT|DELETE /medicore/hospitals/:hospital/access/:subject`
      (`medicore.access.manage`, tenant admin only), `GET …/access` (own grant).
- [x] `PUT …/encounters/:id` (UUIDv7, intent digest, 201/200/409), paged `GET …/encounters`
      with `status`, `GET …/encounters/:id`, `POST …/encounters/:id/transitions`.
- [x] Every route checks the hospital grant in its transaction; no grant is 404.
- [x] Legacy `GET /medicore/visits` lists legacy visits only.
- [x] Events `encounter_opened.v1`, `encounter_status_changed.v1` through the outbox.
- [x] Tests (`encounters_test.go`): patient at another hospital/tenant/unknown → 404; no grant →
      404 on every route; replay/conflict; lifecycle, stale, reason, closed final; DB guards refuse
      direct UPDATE/DELETE. Each main test failed with its check removed.
- [x] Archtests: permission matrix, admin-only list, unpaginated GET allowlist, payload
      classification. Contract docs regenerated.

### Task 3: OPD page

- [x] `OpdEncounters`: hospital picker, access check, patient search and open form, encounter
      list with patient names read from the registry, status actions with a required reason for
      cancel / left without being seen. Vitest: list and move, reason, open under UUIDv7, no
      access, no permissions.

### Final review fixes (fresh reviewer, Opus)

- [x] Important 1: the page keeps one encounter id per open across retries — "retries an open under the same encounter id" RED→GREEN.
- [x] Important 2: a PUT that loses the insert race replays the winner (`ON CONFLICT DO NOTHING` + intent checks) — `TestAnOpenThatLosesTheInsertRaceReplays` (race forced with a held, uncommitted row) RED (500) → GREEN.
- [x] Important 3: guard assertions now require SQLSTATE 23514 and the guard's own message, on statements no other constraint rejects.
- [x] Important 4: `TestLegacyVisitsStayLegacy` — legacy create still works; legacy rows 404 on hospital routes.
- [x] Important 5: patient names cached 10 minutes (`staleTime`), "does not re-read a patient when the list is filtered" RED→GREEN; batched audited lookup filed as #1378.
- [x] Important 6: a refused move reloads the list (`invalidateOnError`) — RED→GREEN.
- [x] Minors fixed: transition history must match the locked encounter (23514); unknown `to` is 400; encounter constraints added `NOT VALID` then validated; reason toggle `aria-expanded` and disabled while saving.
- Minors deferred: grants accept any subject string; insights totals include encounters from every hospital (aggregates only); a re-registration under the same patient id at another hospital is ignored rather than raised.
