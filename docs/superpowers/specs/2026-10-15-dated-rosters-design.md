# Dated rosters, allocation modes and freeze

Issue: #1036 (parent epic #979). ADR: [0020](../../adr/0020-dated-rosters.md). Builds on [roster settings and assignments](2026-10-15-roster-settings-assignments-design.md).

## Problem

Hospitals plan who works which shift, in which area, on which date. Legacy Shift Allocation offers roster-wise, employee-wise and additional-shift entry, automatic population from weekly patterns and a frozen roster. Without dated rosters, the reviewed shifts, areas, templates and settings from earlier slices cannot be scheduled.

## Scope of this slice

- Dated rosters: one to six whole weeks starting on the hospital week start; `draft` → `frozen` → (`unfrozen` back to `draft`) and `draft` → `discarded`. Non-discarded periods never overlap per hospital.
- Allocation changes in three modes: `roster` (any staff and dates), `employee` (one staff member), `additional` (a second shift on a date that already has a primary). Each change is one reviewed, idempotent revision of at most 500 additions and 500 removals.
- Population from active weekly-template assignments under the settings pinned to the roster, optionally limited to selected staff. Population never overwrites an existing staff-date and reports what it skipped.
- DST-safe instants: wall-clock shift times resolve through the pinned IANA timezone; gaps and repeated hours fail closed (`dst_unresolvable`).
- Overlap exclusion per person across rosters, enforced in PostgreSQL.
- As-of reads: allocations at any past version, full revision history.
- APIs, generated Go/TS contract, `/workforce/rosters` shell pages, events, audit, verifier, docs.

Not in this slice (remain on #1036): leave conflicts (need #1037 leave records) and credential conflicts (need #1108 credentials). Population skips nothing for those reasons yet; that is called out on the roster page and in the PR.

## Semantics

- A roster pins the settings revision in force on its first day (composite FK on timezone, week start and population scope), so later settings edits never move existing instants.
- Every write takes `expected_version` and a client `change_id`. The same `change_id` replayed returns the original revision; reused for another action or roster returns 409 `change_reused`.
- Allocations are immutable rows with `added_version` and `removed_version`; history is never rewritten. Discard removes all current allocations in its revision and frees the period.
- `end ≤ start` means the shift ends the next day. Overlap uses half-open `[starts_at, ends_at)` per staff member, across all non-removed allocations in the hospital.
- Freeze and discard need `workforce.roster.manage`; unfreeze needs the separate `workforce.roster.correct` and a reason, so a planner who publishes cannot also quietly correct.
- Population is bounded at 20,000 allocations per call and runs in one transaction; larger hospitals populate selected staff in batches.

## Data and security

Three tables (`workforce_rosters`, `workforce_roster_revisions`, `workforce_roster_allocations`) with forced RLS on the existing hospital policy plus a write flag `app.workforce_rostering`. Revisions reject UPDATE/DELETE/TRUNCATE; allocations allow only a single removal in the current draft version; rosters allow only status/version transitions. Two GiST exclusion constraints (`btree_gist`) enforce period and per-person overlap. Writes need the permission plus a manager grant locked `FOR SHARE`; reads need an explicit hospital grant. Each write commits state, revision, audit and an identifier-only outbox event together.

## Verification

Real-PostgreSQL module tests: create pinning and idempotent concurrency, invalid periods, population scope as-of settings, replay and stale versions, the three modes, overlap within and across rosters, DST gap and overnight instants in `America/New_York`, freeze/unfreeze/discard permissions, as-of snapshots, audit and outbox counts, and DB-level guards below the API. Unit tests cover DST gap, fold and overnight resolution. Full Go/web gates, synthetic verifier, browser and production evidence before closing.
