# Posting proposals and reviewer inbox — 2026-11-04

PR [#1544](https://github.com/tesserix/helivanta/pull/1544) is merged and deployed
as `1d6becdfcc04d13c0d50c791348f1e8962c0d3e4`. This incremental release adds the
proposal API/SDK, receipts/history, protected exact-head context, hospital review
discovery and an eight-item inbox with a read-only reviewer workspace.

## Verified production evidence

- Kargo Freight `04c36a4001e7def9d0fee802937fbfab0b925429`; Promotion
  `prod.01m6hs4y6yn75e61dw14cqbyf8.04c36a4` Succeeded using the unchanged Stage
  template. No imperative Deployment image patch or ruleset exception was used.
- Migration job `helivanta-api-migrate-n9xvk` used the merged image and exited 0.
  The ledger grew from 220 to 223, adding exactly persisted migrations
  `0075_workforce_posting_proposals`, `0076_workforce_posting_proposal_browse` and
  `0077_workforce_posting_proposal_inbox_index`, with no receipts removed.
- `workforce_posting_proposal_inbox` is valid/ready with keys tenant, hospital,
  status, created-at descending and ID descending. Installed
  `workforce_posting_inbox_context` matches committed SQL byte for byte, SHA256
  `9ec4dc2d9590bc6e2265f64b282b1f263574da6b88522b4e98890a6020978397`.
- All seven application deployments are updated/ready/available at 2/2. All 14
  application containers match the published signed build digests and have zero
  restarts. API, Global API and shell Argo applications are Synced/Healthy.
- Landing and login GETs return 200. Unauthenticated review-scope discovery and
  hospital proposal listing return 401 through Global API. An earlier Python
  HTTP-client probe received edge 403 responses; those are not counted as app
  acceptance. Curl GETs supplied the application smoke evidence.

The verified rollout boundary was account `unidevidp@gmail.com`, project
`tesseracthub-480811`, context
`gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke`, application namespace
`helivanta` and Kargo stage `kargo-helivanta/prod`. Both application and
infrastructure repositories remain PUBLIC. Capacity and replica counts are
unchanged; no additional production service was provisioned.

## Validation and limits

Required premerge CI passed. The [merged image workflow](https://github.com/tesserix/helivanta/actions/runs/41729829090)
passed API/shell builds, scans, SBOMs, signing and deploy-branch advancement.
Local source validation includes full Workforce race (679.419s), Global API race
(472.140s), architecture race (65.636s), platform race (12.850s), 633 SDK tests and
1,435 shell tests, plus build, types, lint, canonical generation and inventory
checks. The final gateway protection/index-plan journey passed in 100.443s; its
three-row fixture is not scale or SLO evidence.

Positive effective approval deliberately remains 409
`posting_supervision_required`. Proposals confer no departmental operating
access. Rejection/withdrawal controls are a separate implementation in
[#1547](https://github.com/tesserix/helivanta/pull/1547); they are not part of this
deployed UI. Authenticated browser acceptance still requires the owner sign-in;
the isolated local QA browser remains on its password form. No sign-in, positive
approval or lifecycle acceptance is inferred from deployment health.

Supervision/root policy, effective approval, transfers, contractor expiry,
suspension, exit/rehire and departmental/Pharma consumers remain open. Overall
Staff acceptance remains 10 partial, 10 awaiting audit, 1 research and 0 fully
accepted. Validated slices now ship independently of those remaining areas.

See the [machine record](2026-11-04-posting-proposals.json) and
[Staff tracker](../../qa/staff-management-progress.md). Private raw captures are
under `~/.helivanta/staff-management-20261027/posting-proposals-release-20261104/`.
