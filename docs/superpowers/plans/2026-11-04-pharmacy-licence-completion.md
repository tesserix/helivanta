# Pharmacy licence review and renewal completion (#1391)

Design: [licence evidence and renewal slice](../specs/2026-11-02-pharmacy-licence-evidence.md).
PharmaConnect owns each declaration, its private evidence and immutable decision.
The Global API carries all browser business requests; verified tenant and current
outlet grants remain mandatory. This work implements IN-PH-06/10, IN-FQ-01/06 and
the existing independent-review rule without activating a new regulatory policy.

1. Land the private-file pipeline in #1528 after current-base CI. Preserve its
   isolated worker credential, exact-byte/generation release and audited download.
2. Require released evidence for new decisions in both the owner handler and an
   insert trigger. Pin the exact file/generation, serialize on the declaration,
   retain independent review and preserve historical completed decisions.
3. Publish canonical licence, review and operational renewal-advice fields.
   Provide scoped SDK commands and setup UI with private upload, terminal scan
   polling, authorized download, independent review and dirty-form navigation.
4. Validate missing/pending/rejected evidence, direct-SQL bypass, wrong scope,
   historical decisions, concurrent decisions, changed replays, receipt scope and
   local-calendar renewal boundaries. Do not synthesize available business rows.
5. Run an isolated local stack with a GCS emulator, real Temporal and the actual
   restricted worker. The scanner protocol fixture is explicitly synthetic and
   does not prove production ClamAV signature coverage. Verify startup, failure
   cleanup and that no worker/database credential enters the API environment.
6. Exercise declaration entry, browser upload/scan and a separate reviewer in
   Playwright through the Global API. Rerun sale/UPI/return/refund journeys with the
   licence gate. Run affected race, SDK/UI, architecture, build, lint and generated
   contract/inventory checks; record actual results and unresolved scope.
7. Publish the review/UI PR against the landed pipeline. Deploy the matching API,
   shell and worker artifact after required CI. Production scan acceptance needs
   an explicitly designated synthetic tenant/outlet; readiness alone cannot close
   that acceptance or enable the separate prescription-image enforcement PR.

NPPA policy activation and hospital billing expansion remain outside this change.
