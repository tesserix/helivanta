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

The following paths are relative to `/v1/workforce/{suppliers|pharmacies}/{employer}/staff/{staff}/account-links`. Both families require a current private **manager** row and either `workforce.independent_login.manage` or `workforce.independent_login.review`. Manage defaults to supplier owner/admin and pharmacy owner; review also includes supplier compliance. TenantAdmin remains implicit. Every call still requires current private manager access to that exact employer. These administrative rights confer no physical operating or professional authority. A private manager row alone cannot approve links. Refreshed identity, current tenant membership and expected tenant/subject headers protect account/workspace boundaries.

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
