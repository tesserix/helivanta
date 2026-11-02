# Shared Staff foundation

Status: shared directory implementation verified locally on 2026-11-02;
department ownership, postings and product-specific authority remain in progress.
See the [complete delivery scope](../superpowers/specs/2026-11-02-reusable-staff-foundation.md)
and [implementation checklist](../superpowers/plans/2026-11-02-reusable-staff-foundation.md).

## Reuse the Staff owner and presentation

Workforce is the canonical Staff owner. `StaffDirectory`, exported by
`@helivanta/ui`, renders its existing `Staff` type from `@helivanta/api`.
The normal `/workforce` workspace now consumes this component instead of owning
another embedded directory renderer. The component presents the name, employee
code, job category and employment state, with current-page filtering and
caller-owned cursor navigation.

Callers provide `title`, `description`, `scopeDescription`, `emptyDescription`,
`rows`, `isPending`, `isError` and `onRetry`. Optional `headerActions` and
`rowActions(staff)` compose the caller's authorized workflow; `onFirstPage` and
`onNextPage` follow that owner's pagination. Omit actions entirely for a display-
only consumer. Loading or failure hides supplied retained rows, record counts,
actions and continuation; errors expose retry rather than a fabricated empty
result. Filtering is explicitly limited to the supplied page and never prevents
continuing to another authorized page.

The component owns no fetching, permissions, scope discovery, mutations or
private evidence. Its title and scope description are presentation, not proof of
authority. Existing workspace tenant/subject assertions, hospital capability
checks, protected editor/document/employment/link panels and navigation guards
stay with their existing callers. No new API, table, permission or event is added
by this UI extraction.

## First product consumer: Pharma Connect

`/pharmacy/staff` is the first actual consumer. Its fresh workspace check requires
`pharmacy.catalogue.read` for the screen, while canonical hospital discovery and
Staff reads independently require the user's current Workforce scope. It does
not infer HR access from pharmacy membership or a hospital affiliation. The shared
SDK validates bounded pages, exact normalized hospital IDs, canonical Staff
fields, duplicate IDs, continuation and the original tenant/subject headers.

Pharma offers read-only source browsing and a guarded link to normal Staff
management. It neither creates a duplicate employee nor calls a new pharmacy
Staff master. The local create/edit/no-access journeys are recorded in
[Pharma acceptance](../qa/2026-11-02-pharma-shared-staff-local.md). Product
postings and regulated authority are separate unfinished capabilities.

## Department and product integration boundary

Future department and Pharma Connect consumers must obtain an owner-authorized
Staff projection and reuse its canonical reference. They must not query another
module's tables, duplicate the Staff master, infer authority from a hospital
owner or category label, or pass private HR files and pay into a shared list.
Pharmacy outlet access alone does not grant access to a hospital Staff directory.

ADR-0044 assigns structural departments/sub-units to the platform organization
owner and postings to Workforce. Existing HR department master revisions are
profile references, not a substitute for that hierarchy; reconcile them through
an explicit migration and retain historical revisions. Products own their own
operational extensions and refer to canonical Staff and department versions.

For Pharma Connect the extension includes professional registration evidence,
current verification, outlet designation and pharmacist/assistant authority.
Those decisions remain separate from the common record. `job_category =
pharmacist` and `employment_status = active` do not permit dispensing. Source
ending, credential expiry and disabled identities must be checked by the actual
product command under current authority, including retained browser sessions.

The current Staff contract remains hospital-local. Standalone pharmacy employers
need an explicit owner-scope evolution; never create a synthetic hospital merely
to make a product fit. This design does not claim to implement that evolution,
department postings, in-charge coverage or pharmacist-account story #418.

The [Pharma specialization and audit scope](pharma-staff-specialization.md) records
current source evidence and the requested professional, assistant/PIC, access and
audit responsibilities. These are tracked implementation obligations, not a claim
that the current read-only directory has delivered those workflows.

The [department handoff candidate](../api/workforce-department-links.md) now connects retained HR vocabulary revisions with exact Organization node/revision IDs through owner observations. It preserves existing profile/payroll references and exposes mapping history in HR setup. This is migration lineage; canonical postings, current eligibility and Pharma professional authority still need implementation and acceptance.
