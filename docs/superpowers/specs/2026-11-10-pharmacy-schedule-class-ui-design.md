# Declare and review a product's schedule class in the catalogue

**Issue:** #1636 · **Finding:** F-1, gate G-4 of `docs/qa/2026-11-09-pharmacy-counter-acceptance-plan.md`

## Problem (verified on main, 33b0493d)

A product's schedule class can only be declared and reviewed through the API. Nothing in `apps/shell` or `packages/api` calls `…/catalogue/products/{id}/schedule`.

A release needs an approved class other than X or NDPS (`docs/api/domains/pharmacy.md`, releases), and so does every sale. So today each product needs two people using the API. The only callers are the e2e seed (`e2e/tests/support/pharmacy-counter.ts:218-228`) and the acceptance plan's CAT-11 to CAT-17, which are API cases.

## What exists (no backend change)

| Route                                   | Permission                  | Behaviour                                                                                                                                                                                                                                                            |
| --------------------------------------- | --------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `PUT …/products/{id}/schedule/{uuidv7}` | `pharmacy.schedule.declare` | `{regulatory_class: none\|G\|H\|H1\|X, ndps: bool (required), source: 1-300}`. 201 when created, 200 on an identical replay. 409 when the id is reused with different details. 400 "a declaration names its source, such as the pack label" when the source is blank |
| `POST …/schedule/{declaration}/review`  | `pharmacy.schedule.review`  | `{decision: approved\|rejected, reason: 1-500}`. 409 `four_eyes` when the reviewer is the declarer (enforced in the database). 409 "this declaration has already been reviewed" on a second review. 400 "a review needs a reason" when the reason is blank           |
| `GET …/products/{id}/schedule`          | `pharmacy.catalogue.read`   | `{in_force, origin: declared_by_outlet, reason, history}`. History is newest first and bounded to 50 (`historyLimit`)                                                                                                                                                |

Rules the UI must not contradict:

- **Only the latest declaration counts, and only once approved.** A newer declaration that is pending or rejected leaves nothing in force (`schedule_class.sql`). Reviewing an older pending declaration is accepted by the API but changes nothing.
- **Who holds the grants.** The pharmacist and the pharmacy owner hold both grants. A write without the outlet editor grant returns 404.

## Decision

A **Schedule class** section in the open product, built like the existing `ProductCategorySection` (`item-categories.tsx`), which has the same declare-then-review shape. It goes above Tax, because a release checks the class first.

- **Client** (`packages/api/src/pharmacy-schedule.ts`, exported from the index):
  - the `PharmacyScheduleDeclaration` and `PharmacyScheduleState` types, matching the JSON above;
  - `pharmacyProductSchedulePath(scope, productId)`.
- **Component** (`apps/shell/components/pharmacy/schedule-class.tsx`): `ProductScheduleSection({scope, product, timeZone, canDeclare, canReview, onStateChange, onReviewStateChange})`, mounted in `OpenProduct`.
- **Permissions.** `pharmacy-workspace.tsx` passes `canDeclareSchedule` and `canReviewSchedule`, each as `access.data?.level === "editor" && can("pharmacy.schedule.declare" | "pharmacy.schedule.review")`, matching price and category. The API still enforces.

### Showing the state

- **When a class is in force:** "Schedule H1 · not NDPS, approved {reviewed_at}". The class `none` reads "Not scheduled". Times use the outlet time zone.
- **When nothing is in force:** "No schedule class in force: {reason}", using the API's reason. The product can't be released or sold, and the section says so.
- **X or NDPS in force:** a note that the product can't be released or sold until those controls exist (F-11). The class is still recordable, because it is the truth about the product.
- **History:** each declaration shows class, NDPS, source, declared by and when. It then shows one of:
  - "Awaiting review" for the newest pending declaration;
  - "Superseded by a newer declaration" for an older pending one;
  - "Approved by … : {reason}";
  - "Rejected by … : {reason}".
- **History cap:** with 50 rows, the list says it shows the latest 50.
- **Failed read:** `RequestFailure` with the read's error (#1595). The section never shows a loaded state it doesn't have.

### Declaring (`canDeclare`, product not withdrawn)

- **Fields:**
  - Class: a `FormSelectField` with Not scheduled, G, H, H1, X.
  - NDPS: a `FormSelectField` with Yes and No.
  - Source: an `Input`, trimmed, 1-300 characters.
- **No defaults on Class and NDPS.** Both start empty and are required: "Choose the schedule class", "Say whether it is under NDPS". The API refuses a missing NDPS for the same reason. A default would let an unread field record "not scheduled, not NDPS", which is the least restrictive answer. **Fail closed.**
- **Idempotent submit.** Each form instance holds one `newPharmacyId()`, so a retried submit replays rather than declaring twice. The form is keyed on the newest declaration's id, so a recorded declaration remounts it with a fresh id.
- **Feedback:** toast "Schedule class declared", button "Declare schedule class". It invalidates this product's schedule read and the counter's availability for the product (`pharmacyKey(scope, "counter", "availability", id)`). After a successful submit, that read's `regulatory_class` changes or becomes null.

### Reviewing (`canReview`, newest declaration pending)

- **Offered only for the newest pending declaration.** An older one would be a review with no effect.
- **The form:** shows the declared class, NDPS, source and declarer. The reason ("What you checked", 1-500 characters) is required. It has Approve (submit) and Reject buttons, toast "Review recorded", and the same invalidations.
- **Allowed on withdrawn products,** as categories allow.
- **Shown to the declarer too.** The UI has no reliable "me" to compare `declared_by` with, and hiding the form would only hide what the database refuses. **The database is the control.**

### Errors

- **Mutation errors** show inline (`role="alert"`) with the API's message, as the sibling sections do:
  - 409 `four_eyes`;
  - already reviewed;
  - id reuse;
  - 400;
  - 403;
  - 404 "not found or you do not have access".
- **Already reviewed, or 404:** these also refetch the schedule read, so the section shows the current state rather than a stale pending row.
- **Unsaved changes:** an unsaved form reports through `onStateChange`, so closing the product warns (#1303 D5).

## Tests

- **Vitest** (`schedule-class.test.tsx`, `renderWithProviders`):
  - in force, none in force with its reason, X/NDPS note, the 50 cap;
  - a failed read: 401 "session ended", and 503 with "Try again";
  - declare: both selects required with no default; blank source refused; the sent body; the same id on a retry after a 503; toast and invalidation;
  - review: offered only for the newest pending; `four_eyes`, already reviewed (refetches) and 403 shown inline;
  - no forms without the grants or for a reader.
- **Workspace wiring:** a test that the two grants reach the section.
- **e2e:** a step in the pharmacy counter journey that declares a class through the UI as one actor and approves it as a second actor, then checks it is in force. The existing API seeding stays for the other products.

## Docs changed in the same PR

- `docs/api/domains/pharmacy.md` (declared schedule class): the catalogue UI declares and reviews.
- **Acceptance plan:**
  - §7.2 loses "API until G-4", and CAT-11 to CAT-17 become screen steps with the exact labels.
  - G-4 and F-1 point at this PR.

## Not in this slice

- **Schedule X and NDPS sales** remain refused (F-11). This screen only records the class.
- **No drug master or bulk import.** The class stays an outlet declaration (`origin: declared_by_outlet`).
- **People's names** for `declared_by` and `reviewed_by`. These show the recorded identity, as categories and price controls do. No section resolves names today.
- **No backend, migration or contract change.** `contractdocs -check` should stay clean, and the plan runs it.
