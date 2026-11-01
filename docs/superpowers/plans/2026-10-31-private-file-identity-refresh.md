# Original credential and current identity implementation

Follow the [design](../specs/2026-10-31-private-file-identity-refresh.md) and the
full A01–A12 plan. This change addresses A06 file disclosure; it does not close
the employment lifecycle or activate scoped HR intake.

1. Retain real signed-session IAM/PostgreSQL/OpenFGA/Global API regressions for
   credential revocation, identity inactivity and identity unavailability during
   storage reads. Preserve denial/no-byte/no-filename/no-success-audit assertions
   and prove reactivation does not revive the same credential.
2. Extract existing credential verification without changing entry semantics.
   Bind revalidation to the original token and all original principal claims;
   never borrow a replacement header or overwrite the original actor.
3. Reuse IAM's current identity guard with a fresh bounded context. Preserve the
   direct source watermark read after provider observation, durable inactivity
   revocation and the provider-independent sign-out exception.
4. Invoke credential/identity revalidation before each shared private-file
   permission refresh, outside owner transactions. Keep existing operation,
   hospital, purpose, release and generation checks.
5. Correct the default harness token fixture to retain immutable claims per
   token. The public StaticVerifier and explicit custom verifiers remain
   available. Reproduce the default fixture's 401 through NewHarness before
   fixing it; test repeated requests and unknown-token denial.
6. Run complete authn, harness, journey, IAM, Workforce, bootstrap and Global API
   race suites, architecture checks, vet/build/lint and contract freshness.
   Record actual outcomes and remaining browser/deployed gates in the tracker.
7. Update the draft PR only after its previous CI head is terminal. Do not merge
   or deploy on the strength of this prerequisite alone; all A01–A12 consumer,
   legacy compatibility and lifecycle acceptance requirements remain active.
