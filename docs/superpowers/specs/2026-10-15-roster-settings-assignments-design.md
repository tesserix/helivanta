# Roster settings and staff weekly-template assignments

Issue: #1036 (parent epic #979). ADR: [0019](../../adr/0019-roster-settings-and-staff-assignments.md).

## Problem

Dated rosters (the next #1036 slice) need three facts that do not exist yet: the hospital's reviewed IANA timezone, the weekday a roster week starts, and which staff work a fixed weekly pattern. Legacy Roster Setting exposes automatic population (all / fixed-shift employees) and an effective weekday; legacy Shift Allocation's weekly mode assigns an employee a weekly template. Without these, allocation would guess wall-clock instants and population scope.

## Scope of this slice

- Versioned hospital roster settings: `timezone` (IANA, validated by Go and by a DB trigger against `pg_timezone_names`), `week_start` (Monday=0…Sunday=6), `auto_population` (`disabled`, `fixed_shift`, `all_assigned`), reviewed `effective_on`.
- Versioned staff weekly-template assignments: pinned weekly-template revision, `fixed` flag, `active` (false ends the assignment), reviewed `effective_on`.
- APIs, generated Go/TS contract, `/workforce/roster-policy` shell page, events, audit, verifier script, docs.

Not in this slice (remain on #1036): dated rosters, allocation modes, DST instant resolution, overlap exclusion, population execution, freeze/unfreeze/corrections, leave (#1037) and credential (#1108) conflicts.

## Semantics

- `auto_population` is explicit policy, not a copied legacy value: `fixed_shift` populates only assignments marked `fixed`; `all_assigned` populates every active assignment; `disabled` populates nothing. Owners must review it; no default is created.
- `effective_on` is monotonic per head (a new revision cannot predate the current one). As-of resolution for a date is therefore the newest revision whose `effective_on` ≤ date. Backdating would silently rewrite the policy a past roster was built on, so it fails closed.
- An assignment pins one weekly-template revision. New pins must be current and active and must not take effect before the template revision. An unchanged pin stays valid after the template is revised or retired, so retries and unrelated edits do not force a template change.
- New active assignments require an active staff member in the same hospital. Ending an assignment (`active=false`) is allowed for inactive staff.

## Data and security

Four tables (`workforce_roster_settings`, `…_revisions`, `workforce_staff_roster_assignments`, `…_revisions`), forced RLS with the existing hospital policy plus a write flag `app.workforce_roster_policy`, append-only revisions, no delete/truncate on heads, tenant/hospital-scoped FKs (staff, weekly-template revision via generated `kind`). Writes need `workforce.roster.manage` plus a locked manager grant; reads need an explicit hospital grant. Actor/reviewer come from authentication. Each change commits state, revision, audit and an identifier-only outbox event together.

## Verification

Real-PostgreSQL module tests (red first): primary, concurrent retry, invalid input, role/tenant/hospital denial, stale version, non-monotonic dates, invalid and stale references, DB-level timezone and immutability guards, outbox counts. Full Go/web gates, synthetic verifier, local browser and production evidence before closing.
