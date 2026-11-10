# Automated customer signup

Implements the customer journey in `docs/design/automated-customer-onboarding.md`.
A new India hospital or independent pharmacy registrant becomes its tenant's
native organization owner and Helivanta tenant administrator after native email
verification. This grants governance, not a reviewed clinical Staff identity or
licence. Supported administrative home states are Maharashtra and Odisha.

The onboarding service owns a private durable signup command and receipt. Its
canonical exported Go contract generates the browser contract. Helivanta's
Global API routes `/v1/auth/signup` to its platform owner, which maintains a
Secure HttpOnly host cookie with a random receipt capability and forwards only
bounded commands through an audience/namespace-bound workload identity. The
private service never accepts browser credentials as provisioning authority.
Receipt capabilities are hashed at rest, never logged or put into URLs.

Signup snapshots contain submitted organization/profile data and a stable
initial resource ID, not a second business master. The native organization and
first human are created atomically by the identity operator. The browser cannot
select tenant, subject, native organization or owner role IDs. Cross-tenant
collaboration and subsequent Staff assignments keep their existing owners.

A server-side dry run checks the installed private claim schema before creation.
Deterministic identity and resource IDs plus immutable command fingerprints
make lost responses replayable. Durable pending commands are processed in the
existing API process with bounded leases/backoff; remote work is outside DB
transactions. A leased retry cannot overwrite a completed or conflicting intent.

Helivanta owns the screens, including native email proof and password setup;
there is no hosted-login handoff (#947). Native verification/password codes are
only accepted for the signup's expected native subject. Password setting always
includes the native reset verification code; privileged code-free setting is
never a fallback. Email links carry codes in fragments, removed before sending
requests. Codes/passwords never enter storage, audit detail or logs. Native
password/lockout policies apply. First sign-in uses existing mandatory TOTP
registration and verification. An expired or uncertain proof provides native
recovery; it does not manufacture email verification or login success.

Activation first establishes the existing tenant administration membership and
authorization projection, then invokes the selected workforce/pharmacy owner
through a separate workload-only resource-setup command. Each owner creates its
own resource and governance access idempotently, with attributable evidence.
Independent pharmacy signup creates no fictitious hospital. A receipt becomes
ready only after identity, IAM, initial business setup and held archive receipt.

A signup cookie discloses only its own command. Email duplication must never
replace an earlier profile or create a second person automatically. Existing
identities require verified reuse/recovery rather than email matching to grant
ownership. Public creation/proof/resend operations have bounded abuse budgets;
provider and proof failures remain explicit. No licence, operational Staff,
patient, stock, financial policy or legal readiness is fabricated by signup.

Alternatives rejected: static copy replacement (no working signup), browser
access to management APIs (privilege exposure), shared human/admin credentials
for first login, client-selected existing subject adoption, duplicate business
CRUD, remote work inside database transactions, and public cleartext receipts.

Release requires local build/checks without local applications or containers,
real PostgreSQL CI, native API boundary tests, schema/contract checks and browser
journey tests. GitOps/Kargo owns rollout. The approved data reset separately
needs fresh recoverable captures and actual access to every owned store. A
storage denial is not proof a bucket is empty.
