# Hospital identity onboarding and multitenancy

This runbook describes the operator-backed identity foundation delivered on
2026-10-17. The [tenancy architecture](../architecture/02-tenancy-and-identity.md)
also describes proposed clinical capabilities; identity activation does not prove
that every proposed hospital-sharing or clinical provisioning step is implemented.

## Choose the customer boundary first

A **tenant** is an independently administered customer or group, identified by a
stable Helivanta UUID. Multitenancy means several customers use the same product
while their memberships, configuration and data access remain explicitly scoped.
The UUID is an identifier, not an access credential.

Use **one Zitadel organization per tenant identity administration boundary**, and
the **shared, platform-owned Helivanta project and product clients**. A project
represents the product, not a physical hospital. Do not create a project, client
secret or product deployment per hospital.

| Situation                                                  | Onboarding and access model                                                                                                                        |
| ---------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| Independent hospital                                       | New tenant and identity organization; hospitals/branches are resources within that tenant.                                                         |
| Group managing several hospitals                           | One tenant/org when identity governance is shared; separately scoped hospital and branch assignments. Common ownership grants no clinical access.  |
| Autonomous subsidiary with its own identity administration | Separate tenant/org; any collaboration requires explicit grants.                                                                                   |
| Hospital-owned pharmacy or laboratory                      | Explicit product/resource scopes within the owning tenant; affiliation alone grants no access.                                                     |
| Independent pharmacy or pharmacy chain                     | Own tenant/org. PharmaConnect uses tenant → pharmacy → outlet; never create a fake hospital.                                                       |
| Independent laboratory                                     | Own tenant/org and explicit laboratory permissions; no hospital ownership requirement.                                                             |
| Clinician working for unrelated customers                  | One home `(issuer, subject)` credential with separately approved memberships in each tenant. No duplicate account or implicit email-domain access. |

Zitadel authenticates people and manages org policy/federation. Helivanta owns
product memberships and authorization, combining applicable OpenFGA relationships
with tenant-scoped database access. Organization ownership and project grants do
not confer clinical permissions. A group-wide `tenant_admin` role is inappropriate
for a branch-only worker. Hospital/resource checks must be verified for each
enabled product; the broader sharing model remains a target where not delivered.

## 1. Prepare an approved request

1. Decide new tenant versus adoption. Never use new onboarding to migrate an
   existing tenant; it would allocate another identity boundary.
2. Record the approved customer name, administrator, products, region, legal
   entities, retention obligations and hospital/pharmacy/lab structure privately.
   Do not include patient data in onboarding requests or retained YAML.
3. Select an existing active, verified human Zitadel subject as initial admin.
   Verify the person through the approved administrative process; an email alone
   is insufficient. New onboarding deliberately delegates identity org ownership
   and initial clinical administration to this person.
4. Confirm the platform/product registration already maps Helivanta to the shared
   project. Product registration and customer onboarding are different operations.
   Product machine credentials stay in OpenBao and never enter a browser form.
5. For adoption, privately capture existing tenant/org IDs, `tenant_id` metadata,
   memberships, client callbacks and policy settings. The requested person must
   already have active `tenant_admin` membership. Preserve current login policy
   until MFA/domain changes have a separately reviewed rollout and recovery plan.

## 2. Submit once through the authorized control plane

The currently delivered admin page is
`https://onboard.tesserix.app/orgs/hospital`. Its replacement in
`https://console.tesserix.app` must pass the cutover checks below before this
address is retired. Do not assume the console's existing funnel reporting page
already provisions identity.

1. Sign in as an authorized platform administrator. Choose Helivanta and new
   customer or existing-tenant adoption.
2. Enter the approved display name and verified initial-admin subject. Supply
   existing IDs only for adoption; this operation is platform-admin-only.
3. Retain one request/idempotency key for this intent. If the request times out,
   resume/query that request or replay its identical payload with the same key.
   A changed payload requires a separately reviewed request, not a blind retry.
4. Save the returned tenant UUID and resume/status reference in the private
   customer record. Do not publish customer bindings in GitHub or example YAML.

Approved product backends can submit `POST /v1/hospital-onboardings` with a
short-lived product-scoped OAuth token and `Idempotency-Key`. The API derives the
new UUID from the verified caller/product/key. Product callers cannot adopt
existing UUIDs or read another product's requests (404); a changed payload for an
existing key returns 409. Generic org/grant/enrolment routes reject Helivanta so
they cannot bypass retained onboarding evidence.

## 3. Observe provisioning and check completion

1. The API creates an immutable private `ZitadelOrganization` claim. The operator
   first stores held intent YAML, then reconciles org metadata, the shared project
   grant and policy. New orgs require MFA/domain verification and disable public
   signup/discovery. Adoption preserves identities and its selected login policy.
2. The operator calls the onboarding API with its exact namespace-bound workload
   identity. The API verifies the claim and current Zitadel state, records the
   projection/audit, and calls Helivanta with its separate workload identity.
3. Helivanta verifies or establishes the approved membership and reconciles its
   authorization projection. Only a new pending tenant can bootstrap a first
   member. Adoption never bootstraps one. An append-only audit marker prevents a
   retry from re-granting the revoked last membership.
4. Wait for current-generation Ready, successful activation and the held archive
   receipt. A created org or HTTP acceptance alone is not completion. Dependency
   errors remain pending and retry forward; never delete an org to fix a timeout.
5. Compare adopted memberships, UUIDs, org/client IDs and callbacks with the
   private baseline. A safe replay must reuse identities and archive generations.
   YES HOSPITAL uses this path; its real identifiers belong only in private records.

Intent and receipt are content-addressed revisions under
`helivanta/prod/tenants/<tenantUUID>/revisions/<specSHA>/`. GCS versioning, holds,
six-year retention and no expiry lifecycle protect this configuration history.
The operator has prefix-scoped create/get permissions, not deletion/overwrite or
hold-release rights. Administrative IAM changes can still affect retention;
“never deleted” is not an absolute technical guarantee. Credentials remain in
OpenBao; archive restore and independent database/OpenBao recovery need their own
tested procedures.

## 4. Finish product setup and test access

1. In Helivanta, configure the purchased products, legal entities and actual
   hospitals/facilities or pharmacies/outlets. Use the product's delivered setup
   workflows; identity readiness does not create every clinical resource.
2. Assign workforce roles at the narrowest supported resource scope. Approve each
   additional tenant membership separately, including shared clinicians. Select
   one active tenant/context and have the server validate it on every operation.
3. Verify admin login, MFA/recovery and session expiry. A destination org's strict
   MFA policy does not strengthen a shared user's weaker home-org login; validate
   trusted upstream assurance and the required step-up before approving access.
4. Test an allowed action, another tenant's denial, sibling-hospital denial where
   applicable, branch-only access, a removed membership and a stale browser tab.
   Verify product APIs and exports as well as navigation. An unavailable identity
   or authorization dependency must never silently grant access.
5. Keep hospitals isolated unless a delivered, approved sharing workflow grants
   specific categories/purposes. Sharing pharmacy stock, CRM contacts or a referral
   does not authorize clinical charts. Do not enable proposed sharing features
   solely because the architecture document describes them.

For custom login, use validated org branding or the approved product-hosted
Zitadel Login V2 flow. Keep credentials and login-client secrets server-side in
OpenBao; preserve exact HTTPS callbacks, PKCE S256, state/nonce, host allowlists,
CSRF protection and MFA. A theme or org hint selects presentation, never access.

## Console cutover and retirement gates

Move product registration, customer submission/adoption and status/resume into
the console with server-side authorization and audit attribution. Keep product
machine APIs and private operator activation available during migration. Test
new onboarding, adoption, identical replay, changed-payload conflict, unauthorized
access and dependency failure through the console before switching consumers.

Capture the old app/routing configuration privately, then remove the obsolete
frontend, public hostname and delivery resources through their owning GitOps
repositories. Preserve customer records, database/schema, held GCS revisions,
identities, OpenBao credentials and any still-needed private provisioning backend.
Record deployed revisions and verify the old URL no longer exposes onboarding.

## Operational acceptance

Record who approved the customer/admin, request and tenant references, deployed
revisions, identity/policy checks, negative access tests and held receipt generation
in the private delivery evidence. Never record tokens, credentials or PHI.

These controls support security assurance; they do not establish SOC 2 attestation
or HIPAA compliance. Contract/BAA coverage, risk assessment, workforce procedures,
access reviews, audit retention, recovery drills and incident response also require
organizational evidence. See the
[operator security assurance guide](https://github.com/tesserix/tesserix-operators/blob/main/operators/zitadel/docs/security-assurance.md)
and [API/OpenBao integration guide](https://github.com/tesserix/tesserix-operators/blob/main/operators/zitadel/docs/openbao-and-onboarding-integration.md).
