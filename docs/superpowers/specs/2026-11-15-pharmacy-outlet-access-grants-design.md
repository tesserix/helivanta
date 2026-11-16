# Pharmacy outlet access: manage other people's grants — design

- **Issue:** #1718 (finding F-8 of the [counter acceptance plan](../../qa/2026-11-09-pharmacy-counter-acceptance-plan.md))
- **Date:** 2026-11-15
- **Status:** proposed

## Problem

An outlet grant (`pharmacy_outlet_access`: subject, `reader` or `editor`) is
what lets a person use an outlet. The API can already grant, change and
revoke it (`PUT`/`DELETE …/outlets/{outlet}/access/{subject}`, under
`pharmacy.access.manage`), and every change is written to the audit log by
the table's `audit_record_change` trigger.

The outlet screen (`outlets-setup.tsx`, `OutletAccess`) can only grant the
signed-in person editor access to themselves. Nothing lists who has access to
an outlet: `GET …/access` returns only the caller's own level. So setting up
a pharmacist, a reader or a second owner needs someone to call the API, and
nobody can see an outlet's grants without querying the database.

The acceptance plan's SET-08 also says "GET `…/access` lists them", which is
not true today.

## Goal

A person holding `pharmacy.access.manage` sees who has access to an outlet,
and grants, changes and revokes it from the outlet screen.

## Decisions

### D1. A list of an outlet's grants

`GET /v1/pharmacy/pharmacies/{pharmacy}/outlets/{outlet}/access-grants`,
permission `pharmacy.access.manage`, through `platform.ListRoute`. It is a
keyset page (default and maximum page sizes from `pagination`), newest grant
first, ordered by `created_at, id`.

Each row: `id`, `subject`, `level`, `created_at`, `updated_at`. Nothing else
from the table or the audit log is disclosed.

The outlet must belong to the pharmacy in the path, checked inside the same
transaction, as `changeAccess` does. An outlet of another pharmacy or tenant
is 404. Like `PUT`/`DELETE`, the list needs the permission and the pharmacy
scope, not a grant at that outlet: managing grants is what gives the first
one (SET-06, SET-07).

A separate path, not `GET …/access/grants`, so a subject can never be read
as a route segment.

**Fail closed.** A database error is the existing 500 with no raw SQL text
(`catalogueError`). It is never an empty list, which would read as "nobody
has access".

### D2. The outlet access panel

For someone without `pharmacy.access.manage`, the panel is unchanged: their
own level, or "You don't have access to this outlet yet."

With the permission, the panel also shows:

- **Who has access:** each grant's subject and level, with "You" beside the
  signed-in subject. Loading, empty ("Nobody has access to this outlet
  yet.") and failure (`RequestFailure` with retry) are separate states.
- **Change level:** a `SelectControl` per row (Reader, Editor). It sends the
  existing `PUT`. The toast is "Access changed".
- **Revoke:** a button per row opens a `ConfirmDialog`: "Remove {subject}'s
  access to {outlet}?" If the row is the signed-in person, the dialog adds
  "You will lose access to this outlet." It sends the existing `DELETE`. The
  toast is "Access removed".
- **Grant access:** a form (`useZodForm`, `noValidate`) with a person
  (`FormSelectField`), a level (Reader, Editor; default Editor), and the
  existing "Confirm the outlet" checkbox. It sends the existing `PUT`. The
  toast stays "Pharmacy access granted". A person who already has a grant is
  not offered here; their level is changed in the list.

After any change, the grant list and the caller's own access are refetched.
Errors show inline, as the panel does today.

### D3. Who can be chosen

The person choices are, without duplicates:

1. the signed-in subject, so self-grant (SET-07) keeps working for everyone;
2. the tenant's members from `/iam/members`, paged with "Load more", as the
   supply organisation-access screen does.

`/iam/members` needs `iam.member.manage`, which only a tenant administrator
holds. For anyone else it answers 403. The panel then says: "Only a tenant
administrator can add a new person here. You can still change or remove
existing access." Any other failure shows `RequestFailure` with retry. Either
way the signed-in subject stays selectable and the list of existing grants is
unaffected.

People are shown by account subject. No source of staff display names is
readable here; #1719 tracks it.

### D4. Docs corrected in the same change

- `docs/api/domains/pharmacy.md`, registry and grants row: the list route.
- The contract registry, regenerated with `go run ./cmd/contractdocs`, and
  the staff operational access inventory, regenerated and checked.
- Acceptance plan: F-8 is marked done (#1718). SET-07 and SET-08 use the
  panel, and SET-08 checks the list. SET-10 revokes from the panel. F-2's
  status changes from "Merge #1581" to merged, with verification at SET-19 to
  SET-23 after deployment.

## Not covered

- Display names for people (#1719).
- Letting a pharmacy owner who is not a tenant administrator list tenant
  members. That widens who can see the tenant's account list and needs its
  own security decision.
- Showing a grant's change history in the panel. The audit log already holds
  it.
- Time-limited grants, or rules such as "an outlet must keep one editor".

## Tests

Every new assertion is first seen failing.

- **Backend list:** after grants for three subjects, the list returns
  exactly those three with their levels. A revoked subject is absent. A
  changed level shows the new level.
- **Scope:** an outlet of another pharmacy is 404; another tenant's outlet is
  404; a caller without `pharmacy.access.manage` is 403. Mutation: drop the
  pharmacy check.
- **Paging:** with a page size of 2 and three grants, two pages hold all
  three once.
- **Shell (`renderWithProviders`):**
  - the list shows subjects, levels and "You";
  - a grant sends `PUT …/access/{subject}` with the chosen level and refetches
    the list;
  - changing a level sends `PUT` with the new level;
  - revoking asks for confirmation, and only then sends `DELETE`; cancelling
    sends nothing;
  - a 403 from `/iam/members` shows the administrator message, and the
    signed-in subject is still selectable;
  - a person who already has a grant is not offered in the grant form;
  - without the permission, the panel shows only the caller's own level and
    never requests the grant list.

Gates: `make lint-go`, `cd backend && ./scripts/coverage-gate.sh`,
`go test -race` on pharmacy, `go run ./cmd/contractdocs -check`, the staff
access inventory check, `pnpm turbo lint type-check test build format:check`.
