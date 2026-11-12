# Email-first account discovery delivery

Implements the accepted separate-account decision in tenant-account-chooser.md.
The protected assets are affiliations, credentials, factors and tenant records;
unauthenticated enumeration, forged hints and shared-terminal reuse cross the
browser → Global API → identity-owner boundary. Equal emails never grant access.

The existing onboarding identity owner stores a separate, browser-bound mailbox
challenge, sends an eight-digit code through its existing Resend client and
allows five submissions in ten minutes. Valid email submissions always follow
the same delivery path without looking up accounts. Delivery reservations cap
recipient at three/hour and ten/day, source at ten/hour and thirty/day, and
service at 1000/hour. Signup budgets and proofs are unchanged. The existing hourly sweeper removes
challenges older than 24 hours in batches of 5000 (about 25 hours active-store
retention at the 1000/hour cap when housekeeping is healthy); code hashes bind the random browser secret.
No raw code, browser secret or account choices are persisted in browser storage.

After one successful proof, the browser may read a bounded list for the remaining
ten-minute lifetime. Proof replay is refused; the private cookie permits list
recovery after a lost response. Native search caps at twenty matching humans;
overflow refuses discovery rather than returning a partial, misleading list.
Only active, verified native humans owned by an active Helivanta-enrolled org
appear. The identity owner confirms native product/org state and audits reads.
The final OIDC/session exchange independently rechecks IAM membership.

One choice automatically starts org-scoped OIDC; several show business names.
Selected IDs are routing hints only. Scoped links remain supported without an
ID entry field. Expired native attempts restart discovery instead of dropping
scope into generic global-account login. Switch account uses existing sign-out
and cache/broadcast cleanup before returning to email-first discovery; separate
subjects always authenticate destination credentials/MFA. No cross-org roles or
passwords are copied. Clinical and operational permission checks are unchanged.

Use existing platform sizing targets (1000/3000 peak RPS at months 12/36,
99.95% availability, p99 reads 500ms and command acceptance 800ms); mailbox
submission includes provider latency and has a bounded ten-second budget.
The identity owner reuses its Postgres, Resend and native client; no new service.
Provider/database/native failures deny discovery with retry guidance. Email
acceptance is not inbox-delivery evidence. A timeout may consume a delivery
reservation; requesting a fresh challenge consumes a fresh reservation.

Order: additive owning schema, identity owner contracts/service, then Global API
consumer and web UI. Normal CI/release/Kargo gates apply. Rollback retains the
additive schema and rejects expired proofs. No identities or factors migrate.
Tests cover no pre-proof disclosure, foreign/malformed/expired/replayed cookies,
OTP attempts, delivery budgets, dependency outages, zero/one/multiple choices,
revoked/native foreign accounts and switch failure/cache isolation. Live login
still requires the user's private MFA completion.

## Evidence and remaining release gates

Schema: tesserix/tesserix-k8s#1669. Identity owner:
tesserix/onboarding-service#33. These are candidate changes until merged and
promoted. Normal CI has exercised the owning schema with real PostgreSQL;
local integration cases requiring containers cannot run under the user's
no-local-apps constraint. E2e browser consumer tests explicitly stub only the
discovery API; they are not proof of actual Resend/native availability.

Legacy scoped tenant links accept canonical UUID text supported by the existing
Go/native contracts, including pre-RFC-variant fixture IDs; they still carry no
authority. Signup continues to generate and validate its own tenant IDs.
The old same-subject picker test is replaced with source-session revocation and
cancellation tests because that picker cannot implement separate native accounts.
Current membership endpoints remain available to their existing consumers.

Successful sign-in and sign-out clear the host-private discovery cookie. The
switch action uses the existing unsaved-work guard and explicitly warns that
source sign-out affects that account on all devices. No destination is labelled
active before its own authenticated session exchange. A failed/uncertain logout
keeps the source label and reports uncertainty without claiming it is still live.
Audit and encrypted backup retention follow existing platform policies; active
challenge sweeping is not a claim that all historical backups are erased.

Live acceptance must still prove actual mailbox delivery, single hospital and
independent pharmacy sign-in, same-email separate accounts, account switching,
MFA, current IAM scope and denied foreign-tenant APIs. The Yes Hospital owner's
private authenticator enrolment remains user-operated. Do not delete that tenant.

Discovery names preserve signup's limit of 200 Unicode code points, rather than
200 UTF-8 bytes. Identity-owner, public-proxy and generated-client tests cover
Devanagari, Odia and non-BMP boundaries, including rejection at 201 characters.
