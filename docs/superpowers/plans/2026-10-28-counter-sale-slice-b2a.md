# Counter sale slice B-2a implementation plan: the prescription and its scanned image

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this
> plan task by task.

**Goal:** a pharmacist records a prescription at the outlet (prescriber, registration, council,
date, patient) and attaches scanned pages. Each page is released only after a ClamAV scan by
the pharmacy evidence worker.

**Spec:** `docs/superpowers/specs/2026-10-27-counter-sale-design.md` (#1386), B5, the "Slice B
pull requests" table and "B-2a decisions".

**Architecture:** migration `0016_pharmacy` (`prescriptions.sql`) adds two outlet-scoped tables
with forced RLS and audit:

- `pharmacy_prescriptions`, insert-only;
- `pharmacy_prescription_images`, the supply licence evidence lifecycle with a worker branch in
  its policy.

The image chain is copied from supply, not shared, because modules never import each other:

- intent, then upload;
- the `prescription_image_scan_requested.v1` outbox event;
- the consumer `pharmacy-prescription-image-scan`, which starts a Temporal workflow;
- `cmd/pharmacy-evidence-worker`, which runs the processor as `helivanta_pharmacy_evidence_worker`;
- release, then `prescription_image_changed.v1`.

**Tech stack:** Go, Gin, GORM, Postgres RLS, Temporal, `pkg/attachments` (GCS and ClamAV),
Next.js shell with `@helivanta/api`.

## Global constraints

- **Routes** are under `/pharmacy/pharmacies/:pharmacy/outlets/:outlet/`:

  | Route                                                     | Permission                    |
  | --------------------------------------------------------- | ----------------------------- |
  | `PUT /prescriptions/:id`                                  | `pharmacy.prescription.record` |
  | `GET /prescriptions/:id`                                  | `pharmacy.prescription.read`   |
  | `GET /prescriptions` (ListRoute)                          | `pharmacy.prescription.read`   |
  | `PUT /prescriptions/:id/images/:file` (intent)            | `pharmacy.prescription.record` |
  | `PUT /prescriptions/:id/images/:file/content`             | `pharmacy.prescription.record` |
  | `POST /prescriptions/:id/images/:file/retry/:attempt`     | `pharmacy.prescription.record` |
  | `GET /prescriptions/:id/images/:file/download`            | `pharmacy.prescription.read`   |
  | `GET /prescriptions/:id/images` (ListRoute)               | `pharmacy.prescription.read`   |

- **Permissions:** both are held by `RolePharmacist` only. Writes need the outlet `editor`
  grant and reads any outlet grant. Other tenants and outlets get 404.
- **Prescription fields:**
  - `prescriber_name`: 1–200 characters;
  - `prescriber_registration`: 1–40, matching `^[A-Za-z0-9][A-Za-z0-9/ .-]*$`;
  - `prescriber_council`: 1–120;
  - `prescribed_on`: a date no later than `outletToday`;
  - `patient_name`: 1–200;
  - `patient_address`: optional, 1–500.

  Text is trimmed-equal (no leading or trailing space) and contains no control characters. A
  replay with the same id and body returns 200; a different body returns 409 `conflict`.
- **Images:**
  - `image/png` or `image/jpeg`, 1 B to 10 MiB;
  - filename 1–180 characters, with no `/`, `\` or control characters;
  - `sha256` as 64 lowercase hex characters.

  Status: `upload_pending` → `scan_pending` → `available`, `rejected` or `scan_error`, and
  `scan_error` → `scan_pending` on the next attempt. There is no `revoked` state in B-2a.
  Intents expire after 30 minutes.
- **Object scope:** owner kind `pharmacy_outlet`, owner id is the outlet, record id is the
  prescription. It is tenant-scoped, with no hospital.
- **Names:**
  - worker identity `helivanta_pharmacy_evidence_worker`, actor
    `service:pharmacy-evidence-worker`, env `PHARMACY_EVIDENCE_WORKER_DATABASE_URL`;
  - workflow `pharmacy-prescription-image-processing-v1`;
  - activity `pharmacy-prescription-image-process-v1`;
  - task queue `pharmacy-prescription-image-v1`;
  - session settings `app.pharmacy_outlet`, `app.pharmacy_prescription`, `app.pharmacy_file`.
- **Events** (`contract/events.go`):
  - `helivanta.in.pharmacy.prescription_image_scan_requested.v1` carries
    `{outlet_id, prescription_id, file_id, scan_attempt}`;
  - `helivanta.in.pharmacy.prescription_image_changed.v1` carries the same plus `status`.

  Neither carries prescriber or patient fields.
- **Not covered:** revocation, selling against a prescription, prescription validity periods,
  OCR, and owner access. All of these are stated in the spec.

## Review focus

1. A worker connection scoped to one file can read or update no other row: another file,
   prescription, outlet or tenant, nor any `pharmacy_prescriptions` column.
2. A download is refused for anything except `available` with a matching released generation,
   size and checksum, including after a worker restart that changes the generation.
3. The upload body must match the intent: size, type, checksum and a passive image. A mismatch
   leaves nothing in quarantine.
4. A reader with a `viewer` grant on outlet A gets 404, never 403, for outlet B's prescription
   and its images, and a pharmacy owner gets 403 on every route.
5. A replayed intent with a changed filename, size or checksum is 409, and nothing is
   overwritten.

---

### Task 1: The `pharmacy_outlet` object scope

**Files:** modify `backend/pkg/attachments/object_key.go`; test `object_key_test.go`.

- [ ] **Step 1: Write the failing test.** Add a case to the key table:
  - `pharmacy_outlet` with no hospital produces `t/<tenant>/o/pharmacy_outlet/<outlet>/r/<rx>/d/<yyyy/mm/dd>/f/<file>/v/1`;
  - with a hospital it errors.
- [ ] **Step 2: Run** `go test ./pkg/attachments -run ObjectKey`. Expected: FAIL, "unsupported".
- [ ] **Step 3: Implement.** Add `pharmacy_outlet` to the tenant-scoped kinds.
- [ ] **Step 4: Run it again.** Expected: PASS. Commit.

### Task 2: Migration `0016_pharmacy` and the worker identity

**Files:**
- create `backend/internal/modules/pharmacy/prescriptions.sql`;
- modify `module.go` (embed it as `0016_pharmacy`);
- modify `dev/init-db.sql` and `backend/internal/testinfra/containers.go` (the LOGIN role);
- modify `backend/internal/testinfra/document_worker.go` (`PharmacyEvidenceWorkerDSN`);
- test `backend/internal/modules/pharmacy/prescription_schema_test.go`.

**Tables:**
- `pharmacy_prescriptions(tenant_id, outlet_id, id, the fields, recorded_by, created_at)`:
  - PK `(tenant_id, outlet_id, id)`, with a UUIDv7 CHECK and field CHECKs;
  - the `pharmacy_catalogue_immutable` trigger for UPDATE and DELETE;
  - audit.
- `pharmacy_prescription_images`: the columns of `supply_license_evidence`, with
  `party_id, license_id, license_version` replaced by `outlet_id, prescription_id`:
  - FK to the prescription;
  - creation, identity and transition triggers copied from supply, with no `revoked` state;
  - retain on DELETE;
  - audit.

**Policies:**
- `pharmacy_prescriptions`: `pharmacy_outlet_allowed(tenant_id, outlet_id, false)` for USING and
  `true` for CHECK.
- `pharmacy_prescription_images`: `CASE WHEN current_user='helivanta_pharmacy_evidence_worker'
  THEN pharmacy_evidence_worker_allowed(tenant_id, outlet_id, prescription_id, id) ELSE
  pharmacy_outlet_allowed(…) END`.

**Grants:**
- the worker gets column SELECT and UPDATE on images only, and nothing on prescriptions;
- it can INSERT into `audit_events` and `outbox_events`, under restrictive policies copied from
  supply with pharmacy names and subject `…prescription_image_changed.v1`.

- [ ] **Step 1: Write the failing tests.**
  - `TestPrescriptionsAreInsertOnlyAndOutletScoped`: an UPDATE or DELETE raises; another outlet's
    session sees 0 rows.
  - `TestPrescriptionImageTransitionsNeedTheScopedWorker`:
    - the app role cannot set `available`;
    - the worker scoped to another file sees 0 rows;
    - the worker cannot SELECT `pharmacy_prescriptions`;
    - `available` → `upload_pending` raises.
- [ ] **Step 2: Run** `go test ./internal/modules/pharmacy -run 'Prescription.*Schema|PrescriptionsAre|PrescriptionImageTransitions'`.
  Expected: FAIL (no table).
- [ ] **Step 3: Implement** the SQL, the role and the DSN helper.
- [ ] **Step 4: Run it again.** Expected: PASS. Commit.

### Task 3: Recording and reading prescriptions

**Files:**
- create `prescriptions.go`;
- modify `module.go`: `PermPrescriptionRecord` and `PermPrescriptionRead` (pharmacist), and the
  routes;
- modify `input_types.go`;
- test `prescriptions_test.go`.

**Interfaces:** produces `type prescription struct` (JSON fields as in the global constraints,
plus `id, outlet_id, recorded_by, created_at`) and `loadPrescription(tx, tenant, outlet, id)
(prescription, error)`.

- [ ] **Step 1: Write the failing test** `TestAPrescriptionIsRecordedOnceAndReadOnlyByThePharmacist`:
  - PUT returns 201 and a replay returns 200;
  - a changed body returns 409;
  - a future `prescribed_on` (in the outlet's timezone, set with `SetClock`) returns 400;
  - GET by id returns 200;
  - the list is paged;
  - an owner token gets 403;
  - a pharmacist with no grant on the outlet gets 404;
  - tenant B gets 404.
- [ ] **Step 2: Run it.** Expected: 404, because there is no route.
- [ ] **Step 3: Implement**, using `h.transaction(ctx, p, outlet, writing, fn)` with
  `decodeCatalogue` and `catalogueID`.
- [ ] **Step 4: Run it again.** Expected: PASS. Commit.

### Task 4: Image intent, upload, retry, download and list

**Files:**
- create `prescription_images.go` (intent and list), `prescription_image_upload.go` and
  `prescription_image_access.go` (download and retry), each mirroring the supply file of the
  same role;
- modify `contract/events.go`;
- test `prescription_images_test.go`.

**Interfaces:**
- produces `image.objectScope() attachments.ObjectScope`;
- `pharmacycontract.PrescriptionImageScanInput{TenantID, OutletID, PrescriptionID, FileID string;
  ScanAttempt int64}`;
- `PrescriptionImageScanRequestedData` and `PrescriptionImageChangedData`.

The handler holds `files *attachments.GCS` from `deps.Documents`. A nil value is 503
`evidence_unavailable`.

- [ ] **Step 1: Write the failing tests:**
  - `TestPrescriptionImageIntentsArePinnedAndExactlyReplayable`:
    - 201, then a replay returns 200;
    - a changed checksum returns 409;
    - a download before the scan returns 404;
    - a viewer on another outlet gets 404.
  - `TestPrescriptionImageUploadValidatesBytesBeforeQuarantine`:
    - a wrong size, type or checksum, or a non-image, returns 400;
    - the quarantine bucket stays empty;
    - a good upload returns 202, sets the status to `scan_pending` and puts exactly one outbox
      event in the outbox.
  - `TestTheOutboxCarriesNoPatientOrPrescriberDetails`: the decoded event data keys are exactly
    `outlet_id, prescription_id, file_id, scan_attempt`.
- [ ] **Step 2: Run them.** Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run them again.** Expected: PASS. Commit.

### Task 5: Processor, workflow, consumer and worker binary

**Files:**
- create `prescription_image_processor.go`, `prescription_image_workflow.go` and
  `prescription_image_consumer.go`, mirroring supply;
- modify `module.go`: `Consumers(deps)` returns the scan consumer, and Publishes gains both
  subjects;
- create `backend/cmd/pharmacy-evidence-worker/main.go` and `main_test.go`, mirroring
  `cmd/supply-evidence-worker`, with `PHARMACY_EVIDENCE_WORKER_DATABASE_URL` and no fallback;
- modify `Dockerfile.api` (build and copy the binary);
- tests `prescription_image_worker_test.go` and `prescription_image_workflow_test.go`.

- [ ] **Step 1: Write the failing tests:**
  - `TestTheWorkerReleasesOnlyCleanPinnedBytesAndDownloadsAreAudited`:
    - with a fake ClamAV `stream: OK`, the status becomes `available`;
    - the download bytes equal the upload;
    - the response headers are `no-store`, `nosniff` and sandbox CSP;
    - an `audit_events` row has action `read`;
    - tenant B gets 404.
  - `TestAnInfectedPageIsRejectedAndNeverDownloadable`: `FOUND` sets `rejected` with
    `malware_detected`, and a download returns 404.
  - `TestAScanErrorRetriesOnlyOnTheNextAttempt`: an unavailable scanner sets `scan_error`. A
    retry with the wrong attempt returns 409; with attempt 2 it returns 202 and queues an event.
  - `TestTheWorkerIdentityIsRefusedOutsideItsRole`: `NewPrescriptionImageProcessor` on the app
    DSN errors.
  - Workflow tests copied from supply: invalid input is non-retryable, and the activity maps
    errors to safe codes.
  - Consumer acceptance: a duplicate start with the same input is accepted, and a mismatched
    input is refused.
- [ ] **Step 2: Run them.** Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run them again.** Expected: PASS. Commit.

### Task 6: Contracts, archtests and docs

**Files:**
- `internal/archtest/matrix_test.go`, `arch_test.go` (the unpaginated allowlist for single-object
  GETs and the download) and `event_payload_test.go`;
- `operational_roles_test.go`;
- `docs/api/domains/pharmacy.md`, a "Prescription evidence (#1386 B-2a)" section;
- the regenerated `docs/api/reference/*`.

- [ ] **Step 1: Run** `go run ./cmd/contractdocs && go run ./cmd/contractdocs -check` and
  `go test ./internal/archtest/...`. Expected: failures naming the new routes and events.
- [ ] **Step 2: Add** the entries and justifications.
- [ ] **Step 3: Run them again.** Expected: PASS. Commit.

### Task 7: The prescription screen

**Files:**
- `packages/api/src/pharmacy.ts` (types `PharmacyPrescription` and `PharmacyPrescriptionImage`)
  and a `pharmacy-prescription-images.ts` upload client that reuses `attachment-file.ts`, as
  `supply-evidence.ts` does;
- `apps/shell/components/pharmacy/prescriptions.tsx` and its test;
- `pharmacy-workspace.tsx`: a `prescriptions` view;
- `apps/shell/app/(workspace)/pharmacy/prescriptions/page.tsx`;
- `packages/ui/src/zones.ts`: "Prescriptions" under Dispensing, gated on
  `pharmacy.prescription.read`, plus `zones.test.ts`.

**Behaviour:**
- A `useZodForm` form records a prescription. Its date limit is the server's 400; there is no
  native validation.
- The list shows prescriber, date and patient.
- The detail shows pages with status `Scanning`, `Ready`, `Rejected — {reason}` or
  `Scan failed — Try again` (the retry uses the next attempt).
- Download is offered only for `available`.

- [ ] **Step 1: Write the failing tests:**
  - record → toast "Prescription recorded";
  - a 409 shows "This prescription id was already used with different details";
  - a page that is not `available` has no download link;
  - `scan_error` offers "Try again".
- [ ] **Step 2: Run** `pnpm --filter shell test prescriptions`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run the full frontend gate.** Expected: green. Commit.

### Task 8: Deployment (tesserix-k8s, a separate PR)

Mirror the supply worker:
- `supply-evidence-worker.yaml`;
- `supply-evidence-alerts.yaml`;
- its network policy entries;
- the `helivanta-postgres` credential, giving `pharmacy-evidence-worker`, its secret and the
  role.

Bump the chart version. Until it is deployed, uploaded pages stay `scan_pending` and are never
downloadable. That fails closed, and the PR body says so.
