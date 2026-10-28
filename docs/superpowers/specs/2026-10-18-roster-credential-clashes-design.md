# Roster credential clashes

Issue: #1108 slice C (parent #1035, epic #979; closes the #1108 part of #1036). Builds on [credential validity reviews](2026-10-20-credential-validity-design.md) (slice A) and [dated rosters](../../adr/0020-dated-rosters.md).

## Problem

A roster manager can allocate someone to a shift on a date when their reviewed licence has lapsed or is not yet valid, and freeze that roster without anyone noticing. Roster managers cannot see credential validity at all: validity rows are visible only in enrollment scope, which also exposes licence numbers and issuers that a roster manager has no need for.

MedNet (read-only review) does not check credentials when rostering.

## Decision

- **Only reviewed validity counts.** A clash is an allocation whose roster date falls outside `[valid_from, valid_until]` of one of the person's reviewed credentials: `expired` after it, `not_yet_valid` before it. Dates typed in enrollment and pending reviews are not trusted and never raise or clear a clash. Staff with no reviewed credential raise none; mapping roles to required credentials is out of scope.
- **Rostering sees a column-limited projection.** `workforce_roster_credential_standing` holds staff, credential, type, `valid_from`, `valid_until` and validity version, never number or issuer. A trigger on `workforce_credential_validity` maintains it in the same transaction as every approval, so it cannot drift; reviewed validity is never deleted and a reviewed credential cannot be removed from enrollment, so no row is orphaned. The migration backfills it from existing rows. Reading it requires an explicit hospital manager grant (`workforce_staff_allowed(..., true)`) or enrollment scope; writes need enrollment scope and must equal the validity row, so only an approval changes it.
- **Clashes are computed on read, never stored as truth.** A credential approved or lapsing after a roster is frozen shows up the next time the roster is read. Nothing is skipped, removed or revoked automatically: population and manual changes still book the person. Whether they work is a human decision (no automated employment decisions, #1108).
- **Freezing requires an explicit, attributable acknowledgement.** `POST …/freeze` takes `acknowledged_clashes`: allocation ids, up to 2,000. Under the roster lock the server computes the current clashes. If any is missing from the list, it returns 409 `credential_clashes` with the outstanding count and freezes nothing. Ids that no longer clash are ignored. Each acknowledged clash is recorded in append-only `workforce_roster_credential_acknowledgements`: revision, allocation, credential, validity version and standing. The revision stores `credential_clashes` (the count). More than 2,000 clashes return 409 `too_many_clashes`, and the manager fixes the roster first.
- **Fail closed.** Allocations carry their roster date, so the check needs no time zone. Any database error fails the freeze; a roster is never frozen unchecked.

## API

Under `/workforce/hospitals/{hospital}/hr/rosters/{id}`:

| Route                     | Permission                                                  | Behaviour                                                                                                                                                              |
| ------------------------- | ----------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET /credential-clashes` | `workforce.roster.manage` and hospital manager grant        | Current allocations that clash, by roster date then allocation; paginated; staff, credential type, validity dates and standing, no number or issuer; audited; no-store |
| `POST /freeze`            | `workforce.roster.manage` and locked hospital manager grant | Existing body plus `acknowledged_clashes`; 409 `credential_clashes` (`outstanding`) or `too_many_clashes`                                                              |
| `GET /revisions`          | unchanged                                                   | Revisions gain `credential_clashes`                                                                                                                                    |

Cross-tenant and cross-hospital ids answer 404.

## Events

The existing `roster_changed.v1` freeze event is unchanged and stays identifier-only. Acknowledgements are audit rows, not events.

## Platform

- **NATS:** no new subject or consumer. Rostering reads the same module's projection synchronously, so there is no lag window in which a lapsed credential passes.
- **Temporal:** not used.
- **Valkey:** not used. Credential standing is confidential HR data.
- **SOC 2 / HIPAA:** least-privilege column projection behind RLS, attributable acknowledgement per clash, append-only acknowledgement rows, audited reads, no-store responses.

## Testing

- Projection: first approval and re-approval keep it in step; enrollment cannot remove a reviewed credential; a direct write that differs from validity, and any delete, are refused; a hospital reader cannot read it; number and issuer are absent.
- Boundaries: the expiry day and the valid-from day are valid; the day after expiry and the day before valid-from clash; 29 February.
- Freeze: discard and unfreeze refuse acknowledgements; refused with the exact outstanding count; succeeds with every clash acknowledged; stale ids ignored; replay of the same change id is idempotent; the 2,000 limit; acknowledgement rows and revision count are written once.
- Clash list: pagination, 404 cross-hospital, 403 without roster manage, unreviewed dates ignored.
- UI: clash panel for managers, the freeze dialog listing and acknowledging clashes, the 409 recovery message.

## Not in this slice

- Role-to-required-credential rules (a nurse needs a nursing registration).
- Blocking changes and population on clashes.
- Clash notifications to staff.
