# Release capability admission

Foundation implementation for #1728; production activation requires the deployment gates below.
The platform owns release availability. IAM and each product retain membership,
permission, professional and object-scope authority. A released flag grants none
of those authorities (ORG-AC and ORG-AU controls remain unchanged).

Canonical source: `contracts/release-capabilities.json`. Run
`node scripts/generate-release-capabilities.mjs` and its `--check` mode to
regenerate/verify Go and Zod outputs and registered route/page coverage. CI also
runs `node --test scripts/release-capabilities.test.mjs`.

| Global API read                              | Requirements                                                                         | Response                                                                                    |
| -------------------------------------------- | ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------- |
| `GET /api/v1/auth/release-capabilities`      | Anonymous, shared IP rate limit, no query/body                                       | Policy version, issued/expiry instants, onboarding boolean only                             |
| `GET /api/v1/workspace/release-capabilities` | Verified session/current membership; exact expected tenant and subject preconditions | Same timestamps plus verified tenant/subject and every known capability's effective boolean |

Both reads return `Cache-Control: no-store` (private for the account read).
Unknown keys, bad policy versions, invalid lifetimes, mismatched accounts and
missing dependencies are refused by the browser. Lifetimes cannot exceed 60
seconds. The account-bound request validates the account again after the read;
the query key includes tenant and subject. Switching accounts closes business
content immediately. No provider SDK/management credential reaches the browser.

New API commands intersect local GrowthBook evaluation, the compiled eligible
set and dependency closure with existing authorization. Explicit off is 404;
unknown/unloaded/expired provider state is 503. Unknown route classification
refuses startup. Evaluation performs no network request in the admission path.
Provider refresh is bounded, redirects are refused, and provider errors never
extend an old snapshot's expiry. Retained baseline authentication, verification,
password setup, session recovery and tenant security keep their existing checks.

Navigation, dashboard cards, connected-workspace links and bookmarked workspace
pages use the same canonical page mapping. Unavailable children are not mounted,
so their effects and business API queries do not run. R1 pharmacy navigation is
Staff source plus Outlets & access; Receiving belongs to supplier collaboration.
Scheduling, payroll, stock, sales, receiving and other later groups stay dark.
New signup is gated independently of already-issued verification/password work.

Production requires `GROWTHBOOK_API_URL` and a mounted namespace-scoped reader
file at `GROWTHBOOK_SDK_KEY_FILE`. The reader must select a dedicated Helivanta
project/environment with only `helivanta.release.*` keys. Store it in OpenBao
and deliver it using the namespace's ESO identity. Missing configuration refuses
startup. The explicit local synthetic-identity fixture is allowed only for the
existing dev issuer/environment; it cannot substitute for production wiring.

Production-only owner callbacks are explicitly listed in `internal_routes` as
baseline and may be absent when their integration is disabled. They retain their
existing workload identity, audience and owner authorization checks and are not
published as browser APIs. Unknown internal callbacks still fail startup; every
normal registered API remains required. This keeps current-authority checks and
accepted onboarding continuations available independently of release flags.

## Remaining activation gates

Nested Staff posting drafts, setup department mappings and assignment families
use canonical panel mappings. Assignment creation and approval evaluate the
owner-resolved template family on the server; unknown families fail closed.
Idempotent command replay, rejection, withdrawal and authorized history remain
available under existing IAM checks. Native and legacy Staff choices hide
unreleased families and do not fetch their catalogues. Physical operating
access belongs to the later supplier collaboration capability.

The provider is a dedicated Helivanta installation, organization and sole project
using its standard production environment. OpenBao/ESO reader availability,
actual SDK payload, live on/off/outage acceptance, database authorization
acceptance and version-bound release approval remain deployment gates. Existing
hospital, account and MFA authority are unchanged.
See the [delivery plan](../superpowers/plans/2026-11-16-first-release-capabilities.md).

## Staff release controls

All keys below use the `helivanta.release.` prefix in the dedicated Helivanta
GrowthBook project, production environment. The canonical capability definitions,
dependencies, API classifications, pages and nested panels remain in
`contracts/release-capabilities.json`; this table describes the current release.

| Flag suffix              | Production | Staff scope                                                                        |
| ------------------------ | ---------- | ---------------------------------------------------------------------------------- |
| `team_accounts`          | On         | Tenant accounts, invitations and permission-assignment workflows                   |
| `staff_core`             | On         | Staff records, person/employment reviews, reviewed login links and credentials     |
| `department_teams`       | On         | Department structure, team membership and posting preparation/review               |
| `pharmacy_staff`         | On         | Pharmacy employer staff, account links and required credential/employment evidence |
| `workforce_scheduling`   | Off        | Attendance, shifts, rosters, leave, overtime and duty exchanges                    |
| `workforce_payroll`      | Off        | Benefits and payroll setup                                                         |
| `workforce_advanced`     | Off        | Advanced workforce tools, including credential reminders and reminder policy       |
| `supplier_collaboration` | Off        | Supplier staff, partner teams and supplier operating-access workflows              |

`onboarding` and `pharmacy_setup` also remain on as agreed release prerequisites;
this Staff configuration does not expand the pharmacy stock or financial release.
Core Staff depends on team accounts. Department teams depend on core Staff.
Pharmacy Staff depends on both core Staff and pharmacy setup. Disabling a
prerequisite closes its dependent UI and APIs after the bounded snapshot lifetime;
it does not grant/revoke IAM assignments or delete Staff history.

Credential reminders use `workforce_advanced` for both the Staff page and all eight
hospital/pharmacy policy, list and acknowledgement API declarations. Core
credential reads remain available under their existing Staff capability and
authorization checks. The release middleware blocks the reminder handlers even
if a provider rule mistakenly enables the advanced flag: its compiled eligibility
is still false. Future activation requires a reviewed eligibility change, the
applicable employer/department authorization, and UI/API acceptance evidence.

The desired production values are provisioned through the owning infrastructure
repository's `charts/thirdparty/growthbook/files/helivanta-release.json`. The
provisioner refuses conflicting existing rules rather than silently overwriting
an operator's configuration. Never use a flag as proof of tenant membership,
employment, professional authority or access to another tenant's staff. Provider
credentials stay on the server; browsers receive only account-bound capability
snapshots with a maximum 60-second lifetime.
