# Tenant-owned onboarding production acceptance

Status: prepared for the approved coordinated rollout, not a passed acceptance
report. See ADR-0070 and the dependency PRs. No original identity, password,
security key, accepted membership or business data is deleted to run this plan.
The shared production/QA environment is the user's named test target.

## Release preconditions

- Operators PR31 and its actual Kargo/Argo rollout are healthy on the approved source.
- Schema PR1665 is approved, applied and verified; capture existing index DDL,
  schema checksum and aggregate duplicate counts before migration in protected
  task evidence. Retain the expanded schema on rollback after duplicates exist.
- Onboarding PR30 is green on real PostgreSQL, merged retaining the pinned source
  ancestry, and deployed healthy. Its private login-target route must be present.
- Helivanta PR1654 is green for Go/database, web, supplier and end-to-end gates;
  main CI/images pass; use the normal manual release and production environment
  review, then verify Kargo/Argo and actual deployment source/digests. No CI bypass.
- Verify repo visibility remains public. Record actual account/project/context,
  namespaces and source before each live mutation; never include credential values.

## Same-email independent owner flow

1. Use the authorised `samyak.rout@gmail.com` for a new hospital intent, preserving
   the original global identity. Capture complete HQ/main-branch/contact setup.
   Its active unbound historic receipt may recover its original fresh owner;
   accepted historic bindings are not automatically replaced.
2. Inspect receipt-bound progress and delivery privately. Read-only native/owner
   checks must prove one active deterministic tenant org and its exact fresh
   owner subject, resourceOwner and native ORG_OWNER. Do not publish raw profiles,
   credentials, native links/codes, cookies or tokens.
3. Confirm product email delivery from onboarding@helivanta.app and SPF/DKIM.
   Explicitly verify the new account through the web. It must not inherit the
   original account's verified bit or device challenge. Never reuse an old
   verification link against the new account.
4. The user enters/submits this new account's password through the normal setup
   page. The browser must offer fresh MFA enrolment under its new org policy;
   the user privately enrols the factor. Never remove or simulate the old key.
5. Open workspace: observe original-code/state/PKCE callback selecting the exact
   new tenant, correct owner permissions, profile and business resource. Save the
   workspace-specific sign-in link; repeat sign-in with email and this account's
   password/MFA. Do not use an unscoped email to choose among separate accounts.
6. Save the hospital workspace sign-in link, then use **Create another workspace**.
   Confirm it opens a blank form without changing the existing hospital or
   provisioning another org before submission. Create an independent pharmacy
   intent using the exact same email, not an alias.
   Repeat verification/password/MFA. Prove different tenant/org/owner IDs and
   independent account state. Native names must not collide even if display
   names are the same; business display names remain as submitted.
7. Wrong workspace credentials and foreign native sessions fail generically.
   Check the second tenant cannot read/update the first tenant's business data.
   A browser-supplied workspace ID cannot bypass current server membership.

## Staff and owner administration

- Use fresh native staff accounts owned by each target org. First enrolment and
  role grants succeed only with active, email-verified accounts from that org.
  A same-email/active account from the other org is generically ineligible and
  creates no membership, enrolment, outbox or success audit side effect.
- Confirm department/outlet scope is narrower than tenant membership. Staff
  clinical/operational authority requires explicit scoped roles, not native
  account ownership. New grants/restorations cannot share a foreign identity.
  Existing historic membership reads/removals/history remain compatible.
- As owner, Settings > Security reads the exact org's native current policy and
  applied/pending/drifted status. Reapply the supported mandatory MFA/no-grace
  minimum with fresh authentication and matching revision; await reconciliation.
  It must not claim success before native proof. Arbitrary policy weakening is
  not offered. Staff/non-owner, revoked owner, foreign org and stale revision
  requests must fail; native/audit outage must report unavailable.

## Delivery and integration negatives

- Native email/password links expire after1hour. Expired proof offers recovery.
  Initial/send/resend share3/hour and10/rolling24h per immutable intent,
  including provider failures. Do not deliberately send10 real emails to a user
  just to prove quota: real PostgreSQL concurrency tests cover reservations.
- After verification, a resend returns a privacy-safe no-op, consumes no extra
  allowance/code and produces no new verification message. Completed password
  setup also no-ops. Check metadata and aggregate reservations privately.
- Verify the API workload can call the deployed private login-target broker,
  while absent/wrong workload fails. Broker404 becomes an equalised credential
  refusal; broker/native/store/audit outage is unavailable. The login PAT's
  existing ListUsers403 permission must remain unchanged.
- Run `pnpm --filter @helivanta/e2e exec playwright test --config
playwright.signup-live.config.ts` against the actual site. These bounded UI
  checks intercept submissions/proofs; passing them is not a real owner login.
  Record the real owner flow and native/tenant proof separately.

Close acceptance only with actual evidence for the deployed source. Record any
human step, unavailable capability or unresolved owner-policy scope explicitly.
