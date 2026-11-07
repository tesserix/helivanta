# Supplier grant preparation release — 7 November 2026

PR [#1615](https://github.com/tesserix/helivanta/pull/1615) merged as
`a9ed32e41c457f9bfb3906098c27c8335d2b2040`. This releases private supplier-owned
proposal preparation for a named hospital account or hospital team, with separate
PharmaConnect outlet and SupplyConnect premise references. Every proposal remains
`pending_verification` and `operating_access_enabled:false`.

Effective external grants, bilateral acceptance, owner-authorized disclosure,
distributor stock, order/dispatch/destination receipt and financial composition
remain unfinished. This rollout issues no trading or clinical authority and does
not certify a participant, licence or country policy.

## Verified code and artifacts

The merged Git tree `c0662eb4770159ae3f8960ff96fba54c9cece593` exactly matches the
candidate tested by [complete PR CI](https://github.com/tesserix/helivanta/actions/runs/42319397494).
All required Go, web, scripts, secret and end-to-end checks passed. Local checks
also passed 932 API tests, 1,725 shell tests, owner/race tests, types, lint, builds
and the real-owner browser recovery scenario. Earlier failed checks and their
resolution remain in the [validation record](../../design/staff-and-rbac/validation.md).

[Main image publication](https://github.com/tesserix/helivanta/actions/runs/42323133417)
passed both image security gates and Cosign keyless signing, then advanced the
`deploy` branch to the exact release revision. No severity threshold or scanner
exclusion was changed. The network dependency and shared Node runtime fixes are
included; the latter merged in [base-docker-images #34](https://github.com/tesserix/base-docker-images/pull/34).

| Image             | Published digest                                                          |
| ----------------- | ------------------------------------------------------------------------- |
| `helivanta-api`   | `sha256:4b83ee149ab600634ec5d548629c09a8183e4ceed747e2a8ca6a74c80e7be767` |
| `helivanta-shell` | `sha256:041c8bcadc92c423d9992af4d9797d08ddec58c4e71f6a06730ac32163f33880` |

## Approved migration and Kargo execution

The preflight read the actual migration ledger: 240 applied IDs, no unknown IDs
and no changed registered SQL hashes for already-applied IDs compared with the
previous deployed revision `62f3f7fd`. The canonical candidate has 253 IDs.
Postgres and the latest reported backup were healthy. The previous migration
Job configuration and detailed execution packet were retained privately; no
credentials or kubeconfigs are included in this record.

After explicit user approval for the named 13-migration batch and rollout, the
existing `kargo-helivanta/prod` stage promoted freight
`1505b3a718417caaf39424d92c3b883689c1c254` (`eating-wolverine`). Promotion
`prod.01m6ssxy9nnmtfg7fej0g8b8xg.1505b3a` succeeded. Production auto-promotion
remains disabled; the stage/template and promotion policy were not changed.

The existing `helivanta-api-migrate` PreSync hook ran `/app/migrate` from the exact
release image and succeeded. No ad-hoc mutation SQL was used. The
[machine-readable evidence](2026-11-07-supplier-grant-preparation.json) contains
every applied migration ID, canonical SQL hash and declared new table. Application
rollback retains the new schema and evidence; a rollback needs its own approval.

## Post-release evidence and limits

- The ledger matches all 253 registered IDs, with no missing or unknown IDs.
- All nine declared new tables exist with RLS enabled and forced. Ledger IDs and
  registered hashes alone do not prove every installed function body.
- API, Global API, shell and all four API worker deployments have two updated,
  ready replicas. All 14 application/worker container image references match the
  published image manifests. Argo applications report Synced/Healthy.
- API and Global API `/healthz` and `/readyz` return 200 through authorized
  Kubernetes port-forward checks. An anonymous Global API proposal request
  returns 401. These temporary port-forwards were closed after verification.
- The public login page returns 200. Anonymous requests to the protected Staff
  page redirect (307); the public proposal API denies access (401). The shell's
  actual readiness/liveness path is `/login`; `/healthz` is protected by its
  normal middleware and is not a public shell health probe.

The production checks did not perform authenticated owner preparation, OIDC
sign-in, real patient/Staff disclosure, medicine movement, receipt or settlement.
Synthetic real-owner browser checks establish preparation/recovery behavior,
not complete independent supplier collaboration. Canonical onboarding-service
[#12](https://github.com/tesserix/onboarding-service/issues/12), its dedicated
machine/audience, actual participating organizations/premises and accountable
Maharashtra/Odisha policy/licence reviewers remain live-activation prerequisites.
