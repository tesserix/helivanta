# Private native session evidence source review

For #1703 and PR #1749; not Staff MFA recovery release acceptance.

The web job `135204953738` failed the operational inventory check because the
hash of `backend/pkg/authn/authn.go` changed. Compared with password-recovery
head `eab76985c59df8c22dcab3a7890bcb5aa97f5bf7`, its only change is the
JSON-excluded `Principal.NativeSessionID` field and adjacent alignment.
`verifyCredential` still requires verified credentials, a valid human subject,
current global revocation evidence and an unexpired idle deadline. Unavailable
revocation evidence still fails closed.

The companion Zitadel verifier reads `sid` only after normal token verification.
It supplies no tenant or role authority and is not serialized in the principal.
The new signed-provider test verifies private retention, no tenant derivation,
JSON exclusion and legacy absence. Missing session evidence remains unusable for
the future Staff recovery admission flow.

Re-read the three affected registered chains: renewal still requires the session
cookie, current credential evidence and current membership before preserving
authentication/idle times; activity still requires the authenticated chain and
preserves authentication time while extending the idle deadline; sign-out still
uses the verified subject and commits global revocation through its existing
transaction/outbox path. Their implementation files are unchanged by this draft.

Refreshed only the three `verifyCredential` source pins in the canonical review
input and regenerated the operational inventory. Authority classifications,
registered routes and outstanding runtime acceptance obligations are unchanged.
This is source re-review, not a waiver or completion of employment-cutoff or
Staff authenticator-recovery acceptance.

Local `go test -race ./pkg/authn ./pkg/session -count=1` passed. The inventory
checker and its tests are rerun with the refreshed source records. Full Staff
recovery authorization, revocation/admission, UI and deployed browser acceptance
remain open in #1703.
