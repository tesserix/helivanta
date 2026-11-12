# Separate-account discovery source review

Candidate source review only; no live login or clinical readiness is asserted.

The signout.go diff adds one call after successful existing self revocation and
audit: ClearAccountDiscovery expires a Secure/HttpOnly host cookie containing a
short-lived mailbox capability. It does not change target subject, membership,
revocation watermark, transaction, outbox, audit or error behavior. Existing
self-sign-out classification remains mixed/global credential termination and
must not be repurposed as employment termination. The helper is pinned as well.

The three new unauthenticated account routes are public entry, not business
access. Start requires same-origin JSON and validated email, then invokes only
the private workload broker without account lookup. Verify requires a separate
browser cookie and bounded code; the identity owner consumes attempts and proof.
Choices requires that verified browser capability, bounded native/customer
eligibility and audit at the identity owner. It discloses display data/routing
hints only. Expired/replayed/foreign proof returns a generic refusal; dependency
failures deny. Password, MFA, native tenant ownership and final IAM membership
remain independent boundaries. No role or clinical authority is granted.

Login clears the discovery capability only after its existing token/membership
checks and successful session mint. Account switching uses source sign-out and
fresh destination authentication; it does not re-mint a different tenant for the
same subject through the legacy switch route. The old route is retained for its
existing consumers and is not an implementation of separate-account selection.

Remaining proof: normal CI including database/OpenFGA and browser suites, ordered
schema/identity-owner/consumer rollout, real Resend mailbox delivery, source logout
and destination password/MFA success/failure, shared-terminal/cross-tab cleanup,
revoked eligibility, and foreign-tenant denial. Account discovery is not evidence
that hospital employment enforcement or all staff operating gates are complete.
