# Staff posting and Pharmacy workspace release for user acceptance

Status: production promotion succeeded at **2026-11-05T03:40:26Z**. Release
**62f3f7fd180f5ce9800d70807217b1cb2131d54a** is running. This does not claim
completed Staff Management or positive-role browser acceptance.

## Executed deployment

The user explicitly approved all 13 migrations and completion of the rollout.
Kargo promotion `prod.01m6mwm1fjscfvx6162wtj6445.1afd48c` completed all three
standard promotion steps using freight
`1afd48c1194c46b97db1a89afcfe7b7c994ba06f`. All five pre-merge checks passed
in CI run 41956972697. Image build/scan/sign/publish and deploy-branch advancement
passed in run 41961382408. Repository visibility remains public.

Artifact Registry manifest digests verified for the released API and shell:

- API: `sha256:012c821c4b97907760b57ce17c29d59551cba2832738d197ad02fdcdbfe86356`
- Shell: `sha256:e3666affd91aed0fd8593b30ffb6c689695952206da9c3dc86a85d7d2497f82f`

The API migration hook succeeded. Production now has exactly **240** receipts:
all 227 previous IDs plus exactly the 13 approved IDs. All 13 new tables exist
with RLS enabled and forced; no new foreign-key/check constraints are unvalidated.
The `workforce_posting_supervisor_choice_order` index is valid and ready, with
the registered table/column ordering. Twelve registered SQL files match canonical
source SHA-256 values; 18 installed zero-argument function bodies and security
modes match their final definitions from those files.

API, Global API, shell and emergency workers each have **2/2 ready replicas** on
the exact released tag. The three document/evidence worker deployments also
reference the release, but remain unready with the pre-existing antivirus
freshness issue described below. Argo applications are Synced; Global API and
shell are Healthy, while API application health remains Progressing because of
those worker deployments. Promotion success is not a claim that every worker is
healthy.

External checks: `/workforce/pharmacy-relationships` returns HTTP307 to login;
unauthenticated `/api/v1/workforce/staff-actor-scopes` returns HTTP401. These
prove bounded routing/authentication behavior, not positive-role acceptance.
User browser testing starts at
`https://helivanta.app/workforce/pharmacy-relationships`. No test accounts or
operator role grants were added. Detailed execution/schema evidence is retained
privately under the user's release-self-acceptance-20261105 evidence directory;
no credentials or environment dumps are included.

The remaining sections retain the preflight and promotion procedure for audit.


## Source and verification

Release assembly: PR1565, source **1ed7080d**. The 13 dependent PRs (#1567,
#1569, #1570, #1574–#1580, #1583, #1585, #1586) are merged into their respective
parent branches with their implementation commits retained. The application tree
matches **b4f9b466**; an independently concurrent test-clock correction **ca6c9b02**
is also preserved. That correction makes ended-posting fixtures' eligibility
relative to their test clock; it changes no application behavior.

After integrating current main, local full SDK passes **872 /86 files**, full
shell **1,601 /185 files /412.67s**, Pharmacy race suite **337.966s**, Global API
race suite **690.323s**, and architecture race suite **112.893s**. The corrected
posting-review workspace passes **60** focused cases. The command/read workspace
passes 25 focused cases; affected typechecks, lint, formatting, canonical
contract/inventory checks and production webpack build passed. The unchanged
Workforce source suite previously passed 601.269s. GitHub's required main checks
must still pass on the final assembly; local checks do not bypass that rule.

Two root-branch tests failed on an older CI revision because fixed eligibility
expired before their time-relative posting interval. The current clock correction
is retained, and those cases pass in the 60-case run. No assertion was weakened.
Redundant queued/running workflows of already merged dependency ancestors were
cancelled; the current release and unrelated feature work were left running.
No dependency, container image or software was downloaded locally.

## Production preflight

Read-only discovery found API, Global API and shell running **9f827a66** in
namespace `helivanta`, project `tesseracthub-480811`, context
`gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke`. The application
host is `https://helivanta.app`. Kargo project `kargo-helivanta` has manual
production promotion; use the existing `prod` stage/template and image freight.
Do not patch deployments or edit the unrelated, dirty GitOps worktree.

CNPG `helivanta-postgres` is healthy with two ready instances and primary
`helivanta-postgres-2`. The last successful backup observed is
**2026-11-05T00:00:27Z**, with WAL recovery available. The read-only production
ledger has **227** IDs. Current canonical registry has13 pending migrations;
the expected ledger total after their successful application is **240**.

- `0027_iam_staff_posting_cutoff_templates`
- `0028_iam_staff_posting_source_templates`
- `0029_iam_pharmacy_department_binding_templates`
- `0033_pharmacy`
- `0034_pharmacy_staff_department_bindings`
- `0079_workforce_posting_supervision_intents`
- `0080_workforce_posting_root_approvals`
- `0081_workforce_posting_chain_approvals`
- `0082_workforce_posting_supervisor_choices`
- `0083_workforce_posting_supervisor_choice_index`
- `0084_workforce_posting_cutoffs`
- `0085_workforce_posting_lifecycle_reviews`
- `0086_workforce_posting_source_use`

The [migration manifest](2026-11-05-staff-self-acceptance-migrations.json) records
canonical registered SQL SHA-256 values, affected tables and concurrent-index
metadata. SQL/Go canonical inputs are committed in their owning modules. The
production ledger stores IDs/timestamps, not SQL checksums; ledger membership
alone does not prove installed function bodies. Migration 0083 includes the
registered concurrent index and must use the existing migration runner. Apply
through the existing schema-bootstrap job from the exact release image, with its
global lock, bounded timeouts, receipt handling and RLS/provenance checks.
Do not run ad-hoc mutation SQL or delete historical rows.

These migrations add independently reviewed posting/supervision/lifecycle/source
evidence, immutable role templates, Pharmacy category structures already merged
in main, and department relationship records. Existing legacy postings do not
become operating qualification merely because their status is approved. No
role assignment or new professional/operating activation is part of this rollout.

## Existing readiness limitation

The document, Pharmacy evidence and supply evidence workers were running but
unready before this release. Their `/readyz` reported `scanner unavailable`.
Read-only ClamAV VERSION returned definitions28173 dated **2026-11-03T06:27:52Z**;
readiness requires age at most 36 hours and release requires age at most 48 hours.
The running updater at2026-11-05T01:55:57Z reported its daily database up-to-date.
Do not alter timestamps, publish synthetic definitions, bypass scanning or weaken
freshness thresholds. Obtain a genuinely current trusted upstream definition set
through the existing updater before declaring those workers healthy. Readiness remains degraded. Actual processing/release continues to use its
independent hard freshness gate; worker readiness alone is not proof that every
file request fails.

## Promotion and user testing

After required checks pass, merge the release into main, verify repository stays
public, and wait for signed/scanned API and shell images plus the deploy-branch
advance and matching Kargo freight. Prepare and server-validate the existing
promotion request for that exact image tag. Production rollout and all13
migrations require explicit approval under the user's working agreements; ask
only after the concrete image/promotion is ready. Retain preflight and execution
evidence privately; never commit credentials or kubeconfigs.

After approved promotion, verify the 240 ledger IDs, new protected table RLS and
constraints/index validity, deployment image revisions, Global API/API/shell
health, OpenFGA-backed owner admission and actual external routing. Record each
worker's real readiness; do not report the entire rollout healthy if the scanner
limitation remains. Do not automatically roll back, drop additive schema or erase
review history. Any application rollback needs its own approval and preserves
new durable evidence.

User testing starts at `/workforce/pharmacy-relationships`, with separate browse,
prepare and review modes; posting preparation, posting reviews and posting
cutoffs remain under Staff navigation. Use current reviewed hospital-scoped Staff
roles and login links, explicit outlet access, and an independent reviewer.
A coarse owner/HR administrator role alone is not product operating authority.
Preparation chooses canonical department revisions; approval requires independent
responsibility attestation. Rejection/withdrawal/ending and exact uncertain-command
recovery are separately testable. No test credentials, membership or live business
data was changed during release preparation. Browser acceptance is handed to the
user per their explicit request.

All five reusable Staff foundation requirements remain active. Professional
acceptance, operating current-posting consumers, standalone employers, legacy
lineage and complete lifecycle acceptance remain tracked. The tracker stays
10 partial,10 awaiting audit,1 research,0 fully accepted; this release is not a
claim that all Staff Management work is complete.
