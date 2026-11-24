# Grant access to a person by name: employer access candidates — design

- **Issue:** #1719 (follow-up from #1718)
- **Date:** 2026-11-16
- **Status:** proposed

## Problem

Two screens grant access to a person: the pharmacy outlet access panel
(`apps/shell/components/pharmacy/outlet-access.tsx`, #1718) and the supply
organisation access screen (`apps/shell/components/supply/organisation-access.tsx`).
Both choose the person from `/iam/members`, which returns only an account
subject (`user-…`) with no name. Managers pick by an opaque ID.

`/iam/members` also needs `iam.member.manage`, which only tenant administrators
hold. A pharmacy owner who is not a tenant administrator gets a 403, so the
outlet panel can offer them nothing but their own account (#1718 deferred
minor 2).

Names belong to Workforce: `workforce_staff.display_name`, and the account a
staff record signs in with is `workforce_login_links.subject`. The only reader
that joins them for another screen is the staff-governance list
`GET /v1/workforce/operating-governance-employers/{kind}/{employer}/staff-choices`
(#1669). It requires `iam.governance.operating.manage`, the independent duty
administration authority, which an outlet manager need not hold.

## Decision (user, 2026-11-16)

A manager grants access only to **people employed at that pharmacy or
supplier**: active Workforce staff of that employer whose account link is
active. There is no tenant-wide fallback on these screens.

- **Why:** it bounds who can receive catalogue, receiving or supplier access,
  and it discloses only the employer's own staff names to its own access
  managers rather than the tenant's membership.
- **What it costs:**
  - A person must be on the employer's staff, with an active account link,
    before they can be granted access from these screens.
  - The same applies to the manager's own account: self-grant from the panel
    needs your account to be on staff there.
  - A tenant administrator can still grant any subject through the unchanged
    `PUT` routes, but not from these screens.

## Goal

A manager who may grant access at an employer sees that employer's staff by
name and grants one of them access, without needing tenant administration.

## Design

### Ownership

Workforce owns the data and the disclosure. Pharmacy and Supply own the
authority (who may manage access at their employer) and the routes their
screens call. Modules never import each other, so Workforce provides a narrow
interface in `pkg/authz`, injected through `platform.Deps`. This mirrors
`PharmacyEmployerGuard` and `SupplierEmployerGuard`, which run in the other
direction.

```go
// EmployerAccessCandidates lists an employer's staff who may be granted access by name.
type EmployerAccessCandidates interface {
    AccessCandidates(ctx context.Context, req AccessCandidateRequest) ([]AccessCandidate, error)
}
type AccessCandidateRequest struct {
    TenantID, Actor string      // verified principal; Actor is audited
    Kind            string      // "pharmacy" | "supplier"
    EmployerID      uuid.UUID   // pharmacy organisation ID or supply party ID
    Purpose         string      // "pharmacy.outlet_access" | "supply.party_access"
    Page            pagination.Params
}
type AccessCandidate struct {
    Subject     string    // the account to grant
    DisplayName string    // the staff record's name
    CreatedAt   time.Time // page key with StaffID
    StaffID     uuid.UUID // page key only; not serialised
}
```

### What it returns

The candidates are staff rows `s` joined to login links `l` on (tenant, employer, staff), where:

- the employer is `workforce_hospitals` (tenant, `EmployerID`, `establishment_kind = Kind`);
- `s.employment_status = 'active'`;
- `l.status = 'active'`, and the link belongs to the current staff version, as
  `operating_staff_choices.go` requires (`l.independent_staff_version = s.version`,
  `l.employment_generation_id IS NULL`).

They are ordered by `(s.created_at, s.id)` descending and keyset-paged. Each
staff record appears at most once, and only `subject` and `display_name` leave
Workforce.

The response carries nothing else: no employee code, job category, contact
details, documents or employment history.

An employer that Workforce has never registered has no staff, so it gets an
empty page. That is a true answer, not a failure. An employer registered as
the other kind (a supplier ID asked for as a pharmacy) is
`ErrRecordNotFound`, which becomes 404.

### Audit

Each disclosed row is a sensitive staff read. In the same transaction as the
read, Workforce writes one `workforce_audit` row per candidate returned:

- action `workforce.access_candidate.read`;
- actor `Actor`, resource the staff ID;
- `changes = {purpose, staff_version, link_id, link_version}`.

If the audit write fails, the read fails. No unaudited page is ever returned.

### Routes

| Route | Permission | Authority check in the owning module |
|---|---|---|
| `GET /v1/pharmacy/pharmacies/{pharmacy}/access-candidates` | `pharmacy.access.manage` | the pharmacy exists in the caller's tenant, as `changeAccess` checks (missing or foreign is 404) |
| `GET /v1/supply/parties/{party}/access-candidates` | `supply.party.access.manage` | `requireAccessAdministrator` for the party, as `listAccessGrants` checks |

- **Shape:** both are `platform.ListRoute` lists of `{subject, display_name}`,
  with the standard `limit` and `cursor` and no other query parameters.
- **Why the pharmacy route is pharmacy-level:** an outlet grant is managed with
  the same tenant permission at every outlet of a pharmacy, and staff are
  employed by the pharmacy, not the outlet.
- **Unavailable dependency:** if the Workforce interface is not wired, or it
  returns any error other than not-found, the answer is
  503 `access_candidates_unavailable`. It is never an empty list, because an
  empty list would read as "this employer has no staff".

### Screens

**Outlet access panel** (`outlet-access.tsx`):

- The Person field's options come from `…/pharmacies/{pharmacy}/access-candidates`,
  shown as the display name, with the subject as the value.
- Already-granted subjects are excluded, as today.
- **Removed:** the signed-in-subject fallback, the `/iam/members` query and the
  "Only a tenant administrator…" message.
- **No candidates:** "No staff with a linked account at this pharmacy yet. Add
  them in Staff first."
- **Failure:** shows `RequestFailure`, and the grant form is disabled rather
  than offering nothing silently.
- **"Who has access":** the list shows a grant's display name when that subject
  is among the loaded candidates, and otherwise the subject. Grants made before
  this change, or to people no longer on staff, keep showing their subject.
  There is no second name lookup.

**Supply organisation access** (`organisation-access.tsx`): the person choice
uses `…/parties/{party}/access-candidates` in the same way, with the same empty
and failure states.

### Contracts and docs

Update the following, then run the contract docs and staff-access inventory
generators:

- `docs/api/domains/pharmacy.md`, the supply domain doc and `docs/api/domains/workforce.md`;
- the staff operational access inventory;
- the acceptance plan's SET-07, SET-08 and SET-10a.

No new permission and no IAM change. A migration is needed only if Workforce's
row-level security policies do not already admit this read for the employer
under its existing transaction context. The plan settles that against the
policies in force.

## Delivery

There are two slices, each a full vertical PR:

1. **Workforce interface + pharmacy route + outlet panel.** Closes the
   non-administrator gap from #1718.
2. **Supply route + organisation access screen.** Closes #1719.

## Tests

- **Workforce, against Postgres:**
  - only active staff with an active link at the current version are returned;
  - a staff member of another employer or tenant is never returned;
  - an inactive staff member, a revoked link or a stale link version is excluded;
  - an employer of the other kind gives 404;
  - an unregistered employer gives an empty page;
  - each returned row has exactly one audit row with the purpose;
  - a failed audit write fails the read (mutation: skip the audit write, and the test fails);
  - keyset paging works.
- **Pharmacy and supply routes:**
  - without the permission, 403;
  - another tenant's pharmacy or party, 404;
  - for supply, a non-administrator at the party gets refused as `listAccessGrants` refuses;
  - an unwired or failing interface gives 503, never 200 with an empty list.
- **Shell, for each screen:**
  - options show names and submit subjects, and already-granted people are excluded;
  - the empty state shows the "add them in Staff" message;
  - a failure shows `RequestFailure` and the form is disabled;
  - the grant list shows a name when known and the subject otherwise;
  - no request goes to `/iam/members`.
- **e2e:** the pharmacy outlets pane spec selects a person by display name.

## Non-goals

- Changing who may call the `PUT`/`DELETE` grant routes, or validating there
  that the subject is employed. The server keeps accepting any subject from a
  manager, as it does today. Restricting the API is a separate decision.
- Hospital access screens.
- Showing names for grants whose subject is not a current candidate.
- Searching candidates by name. The list is paged with "Load more".
