# Pharmacy credential reviews, validity and reminders

Status: implementation plan for slice 4e of the
[pharmacy HR lifecycle specification](../specs/2026-11-08-pharmacy-hr-lifecycle.md).

Refs #19. Implements part of
[ADR-0054](../../adr/0054-typed-employing-establishments.md) decision 3. The branch is
`feat/19-pharmacy-credential-reviews`, cut from `main` after slice 4b merged.

## Scope

A pharmacy HR manager reviews a pharmacy Staff record's recorded credentials
against the credential evidence files stored in 4d, and a second manager with
the review permission approves or rejects. Approved validity drives standing,
the expiry register and the reminder schedule, on a calendar given by a new
pharmacy HR time zone. Roster standing stays hospital-only, and an approval
grants no PharmaConnect role or authority.

## Actors and objects

| Actor or object              | Authority kind | Scope                   | 404                                                                    | 403                                                  | 503                                      |
| ---------------------------- | -------------- | ----------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------- | ---------------------------------------- |
| Pharmacy HR manager          | Administration | One pharmacy            | No manager row, hospital, supplier, unknown or foreign-tenant ID       | Missing `workforce.enrollment.manage`                | Registry, database or authorization down |
| Pharmacy credential reviewer | Administration | One pharmacy            | As the manager                                                         | Missing `workforce.credentials.review` on a decision | As the manager                           |
| Pharmacy Workforce reader    | Administration | One pharmacy            | Always, as on a hospital                                               | —                                                    | —                                        |
| Reminder worker              | Service        | One credential revision | Not applicable; RLS limits it to the establishment in its settings     | —                                                    | Temporal retries the activity            |
| Credential, review, reminder | Workforce      | Pharmacy                | Row under another establishment, or a hospital row on a pharmacy route | —                                                    | —                                        |
| Pharmacy time zone           | Workforce      | Pharmacy                | Hospital or supplier ID (guard), and every manager 404 above           | Missing `workforce.enrollment.manage`                | As the manager                           |
| Hospital HR manager          | Administration | One hospital            | A pharmacy ID, including one registered after admission                | Unchanged                                            | Unchanged                                |

## Decisions settled by this plan

1. **Migration `0100_workforce_pharmacy_credential_reviews`**
   (`pharmacy_credential_reviews.sql`):
   - recreates the `workforce_hospital_establishment` triggers on
     `workforce_credential_reviews` and
     `workforce_credential_reminder_policies`, with the same names and timing,
     on `workforce_require_enrollment_establishment()` (0095);
   - adds a `workforce_hospital_establishment` trigger on
     `workforce_roster_credential_standing`, on
     `workforce_require_hospital_establishment()`;
   - replaces `workforce_roster_standing_sync()` so it copies a validity row
     only when its establishment is a hospital;
   - creates `workforce_require_pharmacy_establishment()` and the table
     `workforce_pharmacy_time_zones` as the spec states: key and FK, the
     pharmacy guard, `workforce_roster_timezone_valid()`, a version trigger
     (insert at 1, update at `OLD.version + 1`, identity immutable),
     `workforce_prevent_removal`, forced RLS with the worker's read-only branch,
     and the worker column grant on `(tenant_id, hospital_id, timezone)`.
2. **Inventory.** `pharmacyAdmittingTables` gains reviews, reminder policies,
   validity, reminders and the time-zone table. The enrollment guard list gains
   reviews and reminder policies, the hospital guard list gains roster standing,
   and a new pharmacy guard list holds only the time-zone table.
3. **One core, one target.** `credentialTarget{p, owner establishment, authority
establishmentAuthority}` replaces the loose `(p, hospital)` pair in every
   credential, register, policy and reminder handler.
   `credentialTarget.transaction(required, key, fn)` runs the family authority,
   then `owner.requireKind`, then, for a non-empty key, the advisory lock on
   `tenant:establishment:key`, then `fn`. Keys are `<staff id>` for proposals
   and transitions (shared with enrollment), `credential-reminder-policy`,
   `credential-reminder:<id>` and `time-zone`. Reads pass no key.
4. **Authorities.** The hospital authority opens `h.transaction` and runs
   `enrollmentScope`: the manager row under share lock and
   `app.workforce_enrollment`, which is what `lockHospitalManager` did before its
   advisory lock. The route permission stays the router's. The pharmacy
   authority is 4a's `pharmacyAuthority`, given `PermEnrollmentManage` or, on a
   decision, `PermCredentialReview`.
5. **Request shape.** Each pharmacy handler runs `pharmacyStaffScope` (400 on
   `hr_intake`) and `enrollmentSession` (409 `session_changed`), then the
   existing `credentialIDs` parsing. Hospital handlers keep `scope(c)` and send
   no expected headers, unchanged.
6. **Clock.** `credentialClock` reads `COALESCE` of the roster settings zone
   and the pharmacy time zone for the establishment. The guards make them
   disjoint. The worker reads both through its column grants.
7. **Events.** `approveCredential` publishes `EnrollmentChanged` only when
   `owner.kind` is hospital. The four credential events keep their payloads,
   with `hospital_id` naming the owning establishment.
8. **Unset zone.** The register's `errCredentialZoneUnset` answer is chosen by
   owner kind: the hospital text is unchanged; a pharmacy reads "Set this
   pharmacy's time zone before filtering by expiry." Both keep reason
   `time_zone_unset`.
9. **Time zone.** `GET hr/time-zone` returns `{version: 0}` before a save.
   `PUT` takes `{expected_version, time_zone}` in at most 1 KiB, validates the
   zone with the same rule as roster settings (400), and follows the reminder
   policy's replay rule: the same zone at the current version returns the row
   unchanged, any other stale version is 409. Audit
   `workforce.pharmacy_time_zone.changed` carries the previous and new zone. No
   event.
10. **Contracts.** `contracts/workforce-credentials.json` gains
    `pharmacyTimeZone` and `pharmacyTimeZoneInput`;
    `scripts/generate-workforce-credentials.mjs` regenerates
    `credential_types.go` and `credential-contract.ts`. Existing views are
    unchanged and already name no establishment.
11. **Routes** (module.go, pharmacy group, after 4b's employment routes):

    | Method and path under `/:pharmacy`                                | Permission             |
    | ----------------------------------------------------------------- | ---------------------- |
    | `GET staff/:id/credentials`                                       | `PermEnrollmentManage` |
    | `GET staff/:id/credentials/:credential/reviews` (list)            | `PermEnrollmentManage` |
    | `PUT staff/:id/credentials/:credential/reviews/:review`           | `PermEnrollmentManage` |
    | `POST staff/:id/credentials/:credential/reviews/:review/decision` | `PermCredentialReview` |
    | `POST staff/:id/credentials/:credential/reviews/:review/withdraw` | `PermEnrollmentManage` |
    | `GET hr/credentials` (list)                                       | `PermEnrollmentManage` |
    | `GET`, `PUT hr/credential-reminder-policy`                        | `PermEnrollmentManage` |
    | `GET hr/credential-reminders` (list)                              | `PermEnrollmentManage` |
    | `POST hr/credential-reminders/:reminder/acknowledgement`          | `PermEnrollmentManage` |
    | `GET`, `PUT hr/time-zone`                                         | `PermEnrollmentManage` |

12. **SDK.** `CredentialScope` becomes a union naming exactly one of
    `hospitalId` and `pharmacyId`; the pharmacy member carries `tenantId` and
    `subject` and every pharmacy call sends both expected headers.
    `CredentialOwner` is the same union without `staffId`, taken by register,
    policy and reminder functions. Paths and query keys include the family.
    Time-zone path, key, fetch, save and a form schema checked by
    `isSupportedTimezone` are added.
13. **Shell.** `StaffCredentialPanel`, the register, the reminder inbox and the
    policy form take a credential scope or owner and a display name instead of
    a `Hospital`. The evidence picker passes the matching document scope to
    `useStaffDocuments`. The pharmacy Staff source gains a Credentials action;
    the pharmacy employer gains a Credential register action with the time-zone
    form. On a pharmacy, 401, 403, 404 and 409 `session_changed` end the panel
    and clear the source as in 4a.

## Tasks (each red before green)

1. **Guard inventory and standing.** Extend `establishment_kind_test.go` with
   the lists above, a direct pharmacy standing insert, a direct hospital
   time-zone insert and a supplier review insert. Red: the pharmacy review
   insert is refused by the 0092 trigger and the time-zone table is missing.
   Green: 0100.
2. **Pharmacy review lifecycle.** `TestPharmacyCredentialReviewLifecycle` on
   the `pharmacyDocuments` harness: list, propose citing a 4d credential
   evidence file, approve by a second manager after release, list with reviewed
   dates and standing, enrollment mirror, Staff version, audit under the
   pharmacy, outbox review and validity events with the pharmacy ID and no
   `enrollment_changed.v1`, no roster standing row. Red: 404 route not found.
3. **Separation of duties.** Own decision and linked-subject decision (409, no
   write), withdrawal by another manager (409), rejection without a note (400),
   proposal replay and decision replay.
4. **Time zone and standing.** Version 0, save, replay, stale (409),
   unsupported zone (400); `date_unknown` and `time_zone_unset` before, the
   expiring credential after.
5. **Reminders.** Save a policy; advance a pharmacy schedule with
   `CredentialReminderAdvancer` on the document worker; list, acknowledge and
   supersede by the next approval. Without a zone the worker writes nothing.
6. **Refusal matrix.** Every route: no row, reader row, hospital ID, supplier
   ID, foreign tenant (404); missing permission (403); `hr_intake=v1` (400);
   changed expected subject (409).
7. **Cross-family and kind race.** Hospital Staff, credential, review and
   reminder IDs under the pharmacy family (404) and the reverse. A hospital
   proposal and policy save on an ID registered as a pharmacy after admission
   answer 404 and write nothing.
8. **Hospital suites unchanged.** Credential, reminder, reminder worker and
   roster clash suites pass after the refactor.
9. **Generator and SDK.** `--check` fails until the time-zone contract is
   generated; SDK tests for pharmacy paths, keys, headers, the scope union and
   the time-zone schema.
10. **Shell.** `renderWithProviders` tests: loading, unset zone text and
    disabled filter, 404 clears the source, a version 409 keeps the form,
    success, and the hidden decision on one's own proposal.
11. **Contracts and docs.** `go run ./cmd/contractdocs`, re-pin `module.go`,
    regenerate the access inventory, update `docs/api/workforce.md`.

## Gates

`make lint-go`, `./scripts/coverage-gate.sh`, `go build ./...`,
`go vet ./...`, `go test -race ./...`, `go run ./cmd/contractdocs -check`, the
workforce generators with `--check`,
`node scripts/staff-operational-access-inventory.mjs --check`,
`pnpm turbo lint type-check test build format:check` and `make lint-shell`.
