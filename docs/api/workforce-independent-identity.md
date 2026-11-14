# Independent employer Staff and account identity

Unreleased candidate for supplier collaboration. Workforce owns the Staff directory and reviewed account links. SupplyConnect owns an independent supplier's legal organization and premises; PharmaConnect owns a pharmacy and its outlets. These records confer neither hospital employment nor clinical/professional authority.

## Ownership tree

```text
Verified tenant
├── Hospital employer → departments → hospital Staff/postings
│   └── hospital-owned recipient teams → independently accepted partner scope
├── Independent pharmacy employer (PharmaConnect pharmacy ID)
│   ├── private Workforce Staff directory
│   ├── reviewed Staff/account identity
│   └── outlet-scoped operating duties → PharmaConnect-owned stock
└── Independent supplier employer (owned SupplyConnect party ID)
    ├── private Workforce Staff directory
    ├── reviewed Staff/account identity
    └── premise-scoped operating duties → SupplyConnect-owned stock
```

The last operating-duty and partner-consumption edges require the remaining Shared IAM implementation. A directory access level or reviewed account identity is insufficient authority for them.

## Employer and directory

`PUT /v1/workforce/suppliers/{supplier}` registers a supplier employer from the current SupplyConnect-owned organization. The request is `{}`; callers cannot supply its name, ownership or country. `workforce.supplier.manage` is TenantAdmin-only by default and grants no operating permission. The registry row and initial caller's manager access commit atomically. Counterparties and foreign-owned IDs are refused.

The supplier family mirrors the pharmacy directory with a typed `supplier_id` in its wire DTOs:

| Method       | Path relative to `/v1/workforce/suppliers`       | Authority                                                                   |
| ------------ | ------------------------------------------------ | --------------------------------------------------------------------------- |
| GET          | empty                                            | current tenant membership; bounded discovery of caller-accessible employers |
| GET / PUT    | `/{supplier}`                                    | `workforce.supplier.manage`; current owner registration checks on creation  |
| PUT / DELETE | `/{supplier}/access/{subject}`                   | `workforce.supplier.manage`; typed employer                                 |
| GET          | `/{supplier}/staff`                              | private reader/manager; bounded keyset page                                 |
| GET / PUT    | `/{supplier}/staff/{staff}?directory_context=v1` | private reader/manager for GET, current manager for PUT                     |

Staff updates require `expected_version` and `directory_context_version:v1`. Supplier records cannot enter hospital/pharmacy route families. Hospital child workflows, including clinical employment, payroll and professional credential review, remain unavailable under an independent supplier root.

## Reviewed account links

The following paths are relative to `/v1/workforce/{suppliers|pharmacies}/{employer}/staff/{staff}/account-links`. Proposal, withdrawal and revocation keep their separate manage permission and current private manager access. Manage defaults to supplier owner/admin and pharmacy owner; TenantAdmin remains implicit.

Approval/rejection now requires current `GovernanceAccountLinkReview` authority from IAM: an actual current tenant owner membership or an effective independently governed account-link reviewer grant for that exact employer. Coarse review permissions and private manager rows cannot substitute for that source. The managed governance role receives coarse hospital/independent review admission only; the owner enforces the source again under locks. Existing supplier/pharmacy role defaults are coarse admission, so non-owner reviewers require a governance grant before rollout.

A governance reviewer needs no fabricated employment or directory manager row. Review/read purpose exposes the selected Staff member's link records, plus private actor/maker alias relations needed by independence checks. It cannot edit Staff, create links or update another link. Retained reads still require current source or separately authorized private manage access. Missing source returns 403; unavailable source dependencies return 503. Refreshed identity, current tenant membership and expected tenant/subject headers remain required. These rights confer no physical operating, professional or clinical authority.

| Method    | Path                         | Result                                                                             |
| --------- | ---------------------------- | ---------------------------------------------------------------------------------- |
| GET       | empty                        | bounded private page of typed link views                                           |
| PUT       | `/{link}`                    | proposal bound to exact Staff version, target account membership, reason and maker |
| GET       | `/{link}`                    | current retained link state and its original Staff basis                           |
| GET       | `/{link}/proposal-receipt`   | immutable proposal receipt reconstructed from retained source facts                |
| PUT / GET | `/{link}/commands/{command}` | independent review/change and exact actor-bound command recovery                   |

Link and command IDs are UUIDv7. Proposal input is `{expected_staff_version, subject, reason}`. Command input is `{expected_version, decision, reason}`; decisions are `approve`, `reject`, `withdraw` and `revoke`. Review requires the review permission; withdrawal/revocation requires manage. The maker, target account and known historically approved aliases of the same Staff cannot supply independent review. Only the maker withdraws a pending proposal.

The lifecycle is `pending → active|rejected|withdrawn|revoked`, followed by `active → revoked`. Staff inactivation revokes pending/active links atomically. A new approval must still match the original Staff version. A directory edit invalidates that identity basis for operating consumers even when the historical link still says active. Reactivating Staff or relinking never restores an old assignment basis.

Creation answers 201; identical actor/intent retry answers 200. Changed intent, stale versions and independence violations answer 409. Out-of-scope resources answer 404, missing action permission 403, and unavailable current identity/authorization 503. Reads remain audited and require current authority. Immutable proposal/command recovery survives later target membership removal, link revocation or Staff edits; recovery grants no new authority. Audit/outbox failure rolls back the associated link/history/command changes.

Independent link changes publish `helivanta.in.workforce.independent_login_link_changed.v1`, carrying employer kind/ID, Staff/link IDs, status and version only. Hospital links preserve their existing event contract. An independent employer ID is never published as a hospital ID.

## Internal current identity boundary

`CurrentIndependentStaffGuard` returns only Staff ID/version and reviewed link ID/version, within an existing guarded tenant command. It checks the immutable employer kind, current active Staff, exact Staff basis and current active reviewed account link. It locks the source through the consumer's commit and restores private database purpose settings before invoking the consumer. The boundary requires an attributable caller and records sensitive source use in Workforce audit.

This reference is source evidence. The consumer must separately enforce current Shared IAM duty, exact source location, credential/membership epochs, legal operating scope, applicable licence/qualification and reviewed activity/category policy. It cannot satisfy `CurrentStaffActorGuard` or a hospital clinical employment generation.

## Canonical inputs and release boundary

[workforce-establishments.json](../../contracts/workforce-establishments.json) derives supplier directory models from the existing canonical pharmacy directory fields. [workforce-independent-identity.json](../../contracts/workforce-independent-identity.json) declares typed independent identity envelopes and derives common link fields/statuses from [workforce-login-links.json](../../contracts/workforce-login-links.json). Run `node scripts/generate-workforce-establishments.mjs` and `node scripts/generate-workforce-independent-identity.mjs`; each supports `--check`.

Candidate Workforce migrations are `0093_workforce_supplier_staff` and `0094_workforce_independent_identity`. They are forward-only expansions, with hospital generation and lifecycle protections retained. Production release needs the exact reviewed migration/image set and Kargo promotion approval. This document does not activate a real organization, licence, recipient grant or trade policy.

## Governed review provenance candidate

Unreleased migration `0103_workforce_login_review_authority` applies selected-resource
RLS purposes to hospital and independent links. Each new governed approval/rejection
retains an immutable `workforce_login_link_authorities` row linked to the exact
history version: employer kind/ID, target Staff version, actor, original session
times, owner/grant source, source version, membership epoch and owner count.
Owner source IDs are actual IAM owner-membership IDs; no Staff actor is invented.
Workforce retains references to IAM authority without querying or owning IAM tables.
History, source evidence, audit, independent command receipts and outbox changes
commit or roll back together. Known historical actor/target/maker aliases remain
excluded from independent review, including hospital links.

Current review commands refuse a missing provenance table or inactive recording
trigger with 503, so an old schema cannot report a governed approval without its
retained source evidence. Historical migration fixtures are prepared explicitly
under their old owner rules; they do not manufacture modern governance proof.

Public wire models and event payloads remain unchanged. Canonical generators do
not add private provenance to v1 responses. Focused Workforce/gateway fixtures use
an explicit IAM protocol double; bootstrap acceptance uses the actual IAM grant
and owner guards, FGA membership, PharmaConnect/SupplyConnect owner APIs and
Workforce employer registration. New consumer database acceptance remains pending;
this candidate does not prove first employment activation, operating-duty review,
stock access or live recipient login.
