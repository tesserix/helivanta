# Account recovery implementation plan

Issue [#1703](https://github.com/tesserix/helivanta/issues/1703).
Design: [tenant-owned account recovery](../specs/2026-11-19-account-recovery.md).
Status: in progress. Completing a step does not close the issue.

1. Verify existing login, setup, identity-owner and Staff-link behavior against
   current source. Preserve the existing anti-enumeration and owner-recovery
   decisions. Record genuine gaps separately from account-creation reconciliation.
2. Add a failing assertion for actionable, identical locked-account guidance on
   every credential refusal. Implement through the existing API error response
   and prove Shell renders it once. Run affected unit suites and local builds.
3. Inspect the identity owner's current canonical command/delivery/audit storage,
   native password-reset API and actual deployment. Define and test a separate
   returning-account recipient contract, immutable command, quotas, expiry,
   proof consumption, uncertain delivery and credential invalidation. Generate
   Go/Zod consumer artifacts from its canonical source; no copied wire models.
4. Connect the scoped forgot-password entry, recipient password form and completion
   to the owner through Global API. Test foreign/unknown/disabled targets and
   existing separate-account chooser behavior. Add hosted E2E that resets a
   password and signs in with it, retaining ordinary MFA.
5. Trace current Workforce account-link authorization and IAM credential/login
   admission gates. Define the administrator MFA-reset command and its retained
   source proof, exact eligible link, reason/audit, native operation and durable
   enrollment requirement. Test self/owner/privileged denial, revocation before
   native change and all completion/callback paths before exposing the action.
6. Deliver the Workforce action for hospital and independent-employer Staff, with
   immutable retries and visible unresolved outcomes. Verify new enrollment and
   account access without changing organization policy or business grants.
7. Keep contracts and runbooks aligned; regenerate the affected canonical outputs
   and runtime contract docs. Run affected local suites, lint, typechecks and
   builds. Database/browser runtime checks run in hosted CI; no local apps or
   containers. Remove only validation-generated build artifacts.
8. Commit/push reviewable increments, pass required PR checks, merge normally and
   release through Kargo. Verify actual running images and deployed UI/Global API
   journeys with authorized QA accounts. Capture per-criterion evidence before
   closing #1703; continue #1705, #1704 and #1707, then milestone 12.

Any new live migration or destructive credential operation requires a concrete
reviewable change and its specific authorization before execution. Preparation,
code, tests and ordinary read-only diagnosis continue independently.
