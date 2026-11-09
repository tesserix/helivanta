# Pharmacy Staff access preparation

## Problem and verified source gap

An inactive Staff record, an unlinked account, or missing reviewed duty correctly
fails closed. Hospital directory editing, reviewed employment activation, login
link review and department postings already exist. Independent Staff editors also
have account-link review and duty decisions. Their preparation UI, however, only
supports supplier receiving; pharmacy preparation is absent and other declared
templates cannot be selected. API availability alone does not complete onboarding.

## Decision

Keep Workforce as the Staff/account-link owner, Shared IAM as duty owner and
PharmaConnect/SupplyConnect as immutable organization/location and stock owners.
Expose all currently declared templates for the exact employer kind in one duty
preparation form. Discover current owned warehouses/depots or independent/chain
pharmacy outlets through existing Global API routes, with bounded pagination and
expected tenant/subject headers. Pharmacy structural versions are 1, matching
`CurrentOperatingLocation`; regulatory eligibility remains independently checked
by the product. A source record is not legal operating approval.

Preparation requires active Staff, a reviewed active account on the current Staff
version, an exact source location, template, reason and bounded validity. A distinct
reviewer approves through the existing immutable command and graph acknowledgement
flow. UI preparation never creates credentials, accepts legal evidence or grants
clinical/financial authority. Missing permission is 403, invisible objects 404,
and unavailable current authorization 503, as enforced by the existing owners.

Add actionable Staff preparation guidance at the current record editor/account
link surfaces. Clearly distinguish active directory status from reviewed employment,
account identity, independent duty, hospital posting and pharmacy relationship.
No client readiness flag can substitute for current server authorization.

## Performance and recovery

This is low-volume administrative work: planning envelope 5 peak requests/second,
2–20 KB pages, 90:10 reads/writes, under 100,000 Staff/duty rows at 12 months and
300,000 at 36 months. These are assumptions, not production measurements. Reuse
existing 99.95% availability and read p99 500 ms/command 800 ms targets, existing
bounded page sizes and indexed owner reads. No new datastore, background job,
endpoint, migration or cloud cost is introduced. Unavailable source/link reads
hide preparation and report failure. An ambiguous write freezes the exact intent;
GET recovery confirms that proposal before another write. Explicit stale rejection
allows refresh. Account/workspace changes must not reveal or reuse prior drafts.

## Acceptance

- Pharmacy and supplier managers can select only their employer's declared roles.
- Current reviewed account and exact owned source/version are submitted to IAM.
- Inactive Staff, unreviewed/stale links, wrong owner/country, missing source,
  invalid dates and excess role validity cannot produce a proposal.
- Source read refusal removes preparation; pagination remains usable on empty pages.
- Lost replies recover the exact immutable proposal without a second PUT.
- Pending approval is never displayed as active authority; self-review remains denied.
- Existing hospital activation/link/posting/relationship flows remain the owners.
- Unit/component tests, typecheck, lint and production build pass before merge.
  No local applications or containers are started. Live positive stock movement
  still requires reviewed participants, licences/policies and permitted test stock.

ORG-AC-01/02/04/05/07 and IN-PH scope/current-authority controls apply. No open
ORG-D policy is settled. Direct client-to-product traffic, a test-only authorization
bypass, automatic activation/review and fabricated legal/clinical evidence are
rejected because they would violate the existing ownership and approval model.
