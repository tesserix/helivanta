# Hospital partner membership: implementation candidate

India is the initial country. Maharashtra (`IN-MH`) and Odisha (`IN-OD`) are the
initial jurisdictions. These identifiers narrow rollout planning; they never
establish a premise's licence, a pharmacist's designation or authority to sell a
medicine category. Each source and destination needs its own reviewed evidence.

```text
Organization / tenant
├── Hospital: owns its departments and hospital Staff
│   ├── Department: head → supervisors → assigned Staff and transfers
│   ├── Hospital-operated pharmacy → PharmaConnect outlet stock owner
│   └── Hospital-published partner group (procurement / receiving / finance)
│       └── Direct named hospital Staff memberships, independently reviewed
├── Independent pharmacy organization: owns its own Staff
│   └── Branch / outlet → PharmaConnect stock owner
└── Independent distributor organization: owns its own Staff
    └── Licensed supplying premise → SupplyConnect stock owner

Supplier ↔ hospital collaboration
  exact parties + premises + relationship revision + bilateral consent
  → shared IAM named-person or hospital-published-group recipient
  → supplier-approved action / fields / locations / expiry ceiling
  → current home Staff and membership eligibility on each admission
  → source-owner catalogue / order / shipment / invoice projection
```

The last collaboration chain remains a required consuming integration. The
candidate membership endpoints below do not implement that chain or authorize a
stock movement, clinical action, disclosure, invoice or payment.

## Actors and authority

IAM governance administrators prepare and independently approve publication of a
hospital-owned group. Operational membership managers use reviewed, hospital-only
shared IAM templates from the `partner-membership-v1` catalog:

| Template                               | Authority                                                   | Preconditions                                                                       | Assignment approval                          |
| -------------------------------------- | ----------------------------------------------------------- | ----------------------------------------------------------------------------------- | -------------------------------------------- |
| `iam.partner_group_member_reader.v1`   | Read retained membership evidence                           | Current hospital scope, session, reviewed Staff account and employment              | Existing shared IAM assignment maker/checker |
| `iam.partner_group_member_preparer.v1` | Nominate another named Staff; withdraw own pending proposal | Reader prerequisites plus current nominee posting and exact account link            | Existing shared IAM assignment maker/checker |
| `iam.partner_group_member_reviewer.v1` | Approve/reject independently; suspend approved membership   | Reader prerequisites; approval also rechecks nominee source and purpose attestation | Existing shared IAM assignment maker/checker |

Preparation, review and nomination are separated by canonical Staff ID as well
as account subject. Two accounts linked to one Staff cannot bypass that rule.
Hospital publication and assignment administration never manufacture a pharmacist
qualification or supplier employment. A published group grants no external access.

## Candidate owner endpoints

All browser requests pass through Global API. IAM owns these relative product
paths, with the verified tenant and expected subject supplied by the authenticated
session preconditions:

```text
PUT /v1/iam/hospitals/:hospital/partner-groups/:group/member-proposals/:member
GET /v1/iam/hospitals/:hospital/partner-groups/:group/member-proposals/:member
PUT /v1/iam/hospitals/:hospital/partner-groups/:group/member-proposals/:member/commands/:command
GET /v1/iam/hospitals/:hospital/partner-groups/:group/member-proposals/:member/commands/:command
```

`member` and `command` are canonical UUIDv7 identities retained before sending.
Only `kind:subject` is accepted. A nomination selects the exact subject, Staff,
posting ID/revision/version, department ID/revision, publication version, validity
and reason; it attests that the sources were examined. IAM does not resolve a
foreign product directory or query private Workforce tables.

The Workforce-owned guard checks current hospital posting, reviewed Staff/account
link and employment generation. The nomination retains link ID/version, employment
generation and epoch, and IAM membership epoch. Approval rechecks all of them;
relinking or rehire cannot silently reactivate this nomination. Validity may not
exceed either the published group or the observed Staff source.

The current requester is checked before all reads and commands. A compound IAM
command holds the hospital role permissions; Workforce takes its hospital gate
before Staff actor/link locks, and IAM then locks the group before mutation.
Audit, source-use evidence, immutable command receipt, cohort epoch, projection
and outbox event commit together. No reusable authorization token is returned.

## State and recovery

```text
pending_review → approved → suspended
               ↘ rejected
               ↘ withdrawn (preparer only)
```

Approval requires an independent current Staff reviewer and explicit source and
purpose-eligibility attestations. Withdraw/reject/suspend do not require the
nominee to remain employed. Every command uses the expected proposal version.
Approved subject and Staff membership are unique within a group. Stored approved
status is historical evidence; actual external access still needs live eligibility
and a separate current external grant.

Retrying the retained command ID and exact actor/intent returns its immutable
receipt. A changed intent is `409`; GET recovery never creates another command.
Recovery is checked before nominee revalidation so a committed decision remains
recoverable after the nominee exits. A caller whose own current authority is gone
cannot use recovery to bypass authorization. Unavailable authorization or audit
returns `503`, revoked credentials `401`, missing current permission `403`, and
objects outside the verified tenant/hospital scope `404`.

## Migration and remaining acceptance

Candidate IAM migration `0036_iam_partner_members` adds forced tenant/hospital RLS,
immutable commands, guarded membership projections and cohort epochs. It is an
additive forward migration; application rollback retains its evidence. There is
no destructive down migration for reviewed history.

Real-owner integration, SDK and UI acceptance must be recorded in the delivery
ledger before release. The candidate includes roster pagination, a minimal owner
candidate selector and ordinary scoped membership-manager UI. Retained membership
is explicitly historical evidence rather than a current access observation.
Independent employer scopes, live external-grant admission, bilateral grants,
actual supplier fulfilment and the exact Kargo release remain required work. This
candidate document is not evidence of deployment or regulatory readiness.

The ordinary Staff screen obtains its operation hints from a current hospital
capability read. It does not require membership permissions to appear in the
tenant-wide `/iam/me/permissions` response. Missing/unavailable hospital authority
withholds the roster, and every mutation independently repeats its full source
and permission checks.
