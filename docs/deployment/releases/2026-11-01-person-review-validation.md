# Person-review validation rollout — 2026-11-01

Status: **deployed and verified**. Approved application revision
`b6ba32e513f6ffe55284d388a289723920da556c`, merged through
[PR #1503](https://github.com/tesserix/helivanta/pull/1503), replaces `bd1f62d7`.
Required person-finding preparation fields now show clear policy, finding and date
instructions. The [local browser acceptance matrix](../../qa/2026-11-01-person-review-ui-acceptance.md)
records separate Staff Admin, preparer and independent reviewer results.

## Delivery evidence

The PR passed Go, web, scripts, secret scanning and all browser CI checks. Both
image builds/scans/SBOM/signature jobs passed. The merged source tree is identical
to the tested PR tree. The [merged image build](https://github.com/tesserix/helivanta/actions/runs/41263453193)
published the API and shell images and advanced the deploy branch.

The user approved this named rollout. Kargo promotion
`prod.01m6ae13wb8qmegze8ctmb42m3.dbe4e27` selected freight
`dbe4e2779a99623371d4a9847948c3a1e17862dc` for the exact approved revision.
All three promotion steps succeeded. Argo's API, Global API and shell Applications
are **Synced/Healthy** and the Kargo stage is **Healthy**. No repository protection
was changed for the merge; the main ruleset remained active with no bypass actors,
and `tesserix/helivanta` remains public.

All six application workloads independently reported the expected observed
generation and **2/2 updated, ready and available** replicas: API, Global API,
shell, document worker, emergency worker and supply-evidence worker. All **12 ready
pods** resolved to the exact published digests:

- API and workers: `sha256:027101c9056e2d2bd3200cc84160eae42737091ea9f0d6bdc8cf234b7b11ad08`.
- Shell: `sha256:8584f268ad51bcb905586e83d3b9073d75b2458d0e9579e103576aeef5189975`.

The normal migration hook exited 0. A read-only ledger comparison confirmed all
**193 recorded migration IDs are identical** to the prior deployed ledger:
**zero added or removed**. This rollout adds no schema, permission, assignment,
login link or operational activation. Baseline, admission manifest, controller
receipt, exact pod-image evidence, ledger comparison and screenshots are retained
in the existing private QA evidence store; credentials and raw screenshots are
not committed. [Machine-readable evidence](2026-11-01-person-review-validation.json)
contains only minimized technical results.

## Browser verification and limits

Normal production browser sign-in succeeded separately for the existing synthetic
owner and Staff reviewer. After fresh page load, both person-review workspaces
correctly reported no authorized hospitals. Tenant-admin or membership alone did
not create person-review authority. The reviewer also received the explicit
payroll-access denial and **No hospital access yet** on the Staff directory.
No production grant was created to make those checks pass.

The positive preparation, protected download, independent approval/history,
withdrawal, rejection and revocation journeys were accepted **locally** with
independently reviewed scoped actors and real scanner-released synthetic bytes.
Production smoke/denial checks do not replace those positive normal-workspace
acceptance gates or justify broader production authority.

[PR #1504](https://github.com/tesserix/helivanta/pull/1504)'s protected employment
history API/SDK is a **separate candidate**, excluded from this rollout. Employment
review UI/selection/protected bytes, operational generation, explicit legacy and
governance review, and lifecycle cutoff remain open. Whole Staff remains **10
partial, 10 awaiting audit, 1 research and 0 fully accepted areas**; #1035 and #417
remain open. This release does not establish complete Staff Management or active
employment enforcement.
