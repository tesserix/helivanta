# Shared Staff foundation — production acceptance

## Approved rollout

Foundation PR [#1521](https://github.com/tesserix/helivanta/pull/1521) merged as
`058ea37a2c14ce88b3a7cf20d5a0762671fb5a85`. The user approved its six migrations,
then separately approved the four pending Billing migrations found in preflight:

- Organization 0001–0004.
- Workforce 0069–0070.
- Billing 0012–0015.

Image workflow 41424323219 succeeded. Kargo freight
`1565f0bd17d695fb4a9422a130a1cea008859339` references the exact merge commit.
Promotion `prod.01m6dwe2xg0twhysa8xd97jjg0.1565f0b` succeeded through the
configured stage template. Migration job `helivanta-api-migrate-8clts` exited 0.
Read-only database receipt verification confirmed all ten approved migrations;
credential migrations 0071/0072 were absent and remain a separate rollout.

Cloud discovery confirmed account `unidevidp@gmail.com`, project
`tesseracthub-480811`, context
`gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke`, application namespace
`helivanta`, and Kargo namespace/stage `kargo-helivanta`/`prod`.
API, Global API, shell and document worker were each 2 ready/2 updated at the
approved merge image. Their image tags were checked again during UI acceptance.
API, Global API and shell Argo resources were Synced/Healthy.
The repository remains public. No GitOps working-tree changes were included.

## Normal owner UI

The existing test owner signed in normally at `https://helivanta.app/login`
using the private authorized credential file. The default tenant initially had
no hospital access. Selecting the intended existing YES HOSPITAL tenant restored
its explicit synthetic hospital access; no grant was added.

All business mutations below used the deployed browser UI:

1. Created inactive pharmacist `SYN-PROD-SHARED-PHARMA-20261102` through Staff.
2. Edited its name to `Synthetic Production Pharma Source Updated 20261102`.
3. Opened Pharma → Staff source. The read-only canonical table showed the same
   edited inactive pharmacist. Filtering returned one record; reload preserved it.
4. A supplementary read-only database query confirmed exactly one matching Staff
   row, inactive, version 2. It did not modify business data.
5. In Settings → Organization structure, registered the existing synthetic
   hospital reference. Its source observation became verified.
6. Created department `SYN-PHARMA-20261102`, named
   `Synthetic Production Pharmacy 20261102`, with a required synthetic QA reason.
7. Saved an attributed edit to `Synthetic Production Pharmacy Updated 20261102`.
   History retained revisions 1 and 2 with their respective UTC effective times.
8. Reloaded Organization structure and reopened the hospital. The edited
   department remained active with its canonical code.

No Staff activation, login association, professional approval or operational
grant was created. A verified hospital reference proves source existence only;
the department records structural metadata. Neither authorizes dispensing.

## Existing reviewer boundary

After normal owner sign-out, the existing synthetic reviewer signed in using its
private credential file. The dashboard reported no hospital Staff access. HR
setup explicitly required hospital access and exposed no setup or mapping data.
A direct visit to the known Organization structure route displayed
`Organization structure access required`, with no structural data or edit
controls. No membership or permission was expanded for this test. These are
negative acceptance results; they do not prove a positively authorized
Organization reader or an operational pharmacist journey.
The known `/pharmacy/staff` route also explicitly refused access without
disclosing its canonical Staff table.

## Evidence and remaining scope

Private mode-0600 receipts and screenshots are retained under
`~/.helivanta/staff-management-20261027/foundation-rollout-1a632554/`, including
`promotion-receipt.json`, `migration-receipts.json`,
`production-pharma-shared-source.png` and `production-organization-history.png`.
Credential values, tokens and business database dumps are not committed.

This acceptance does not complete effective Staff postings, Pharma professional
acceptance, PIC/assistant supervision, operational command lifecycle enforcement,
standalone employer scope or the Staff audit workspace. Full Staff remains
10 partial, 10 awaiting audit, 1 research and 0 fully accepted. Earlier local
evidence sections describe their historical checkpoints; this document records
the later foundation rollout, not acceptance of the outstanding workflows.
