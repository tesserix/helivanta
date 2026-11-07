# Production release

Production changes only when someone approves a release. After approval every
step is automatic: images, application migrations, connector and UI roll out
together from one commit.

| Step                 | Trigger                                                            | Result                                                                                                                                                                                                                            |
| -------------------- | ------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Feature branch or PR | Push                                                               | `Images` builds and Trivy-scans both images locally. Nothing is pushed or signed.                                                                                                                                                 |
| Merge to `main`      | PR merge                                                           | `Images` builds, scans, signs and pushes `helivanta-api` and `helivanta-shell` tagged with the full commit id, then fast-forwards `deploy`. Production does not change; deploy that tag to a test environment manually to try it. |
| Request              | `gh workflow run release.yml --repo tesserix/helivanta --ref main` | The run waits on the `production` environment. Pass `-f commit=<id>` to release an older `deploy` commit.                                                                                                                         |
| Approve              | A maintainer approves the run's `production` deployment            | [`scripts/release-prod.sh`](../../scripts/release-prod.sh) verifies both images, points each image's `release-prod` tag at the verified digest, then fast-forwards `release/prod`.                                                |
| Promote              | Kargo, within 5 minutes                                            | Warehouse `services` in `kargo-helivanta` creates Freight holding the `release/prod` commit, both image digests and the newest Helivanta chart commit, and the `prod` Stage auto-promotes it.                                     |
| Roll out             | Kargo `argocd-update` steps, in order                              | `helivanta-api` runs its migrate hook, then `helivanta-global-api`, then `helivanta-shell`, each pinned to its Freight digest and chart commit.                                                                                   |

The release script refuses, and leaves `release/prod` unchanged, when:

- the commit is not on `deploy`, or is behind `release/prod`;
- either image is unpublished, or its `org.opencontainers.image.revision`
  annotation is not the commit;
- either image digest lacks a keyless signature from `images.yml` on `main`.

An image built or signed anywhere else is never releasable. The script resolves
each digest once and checks the signature, then moves the tag, on that same digest.

## What Kargo subscribes to

Warehouse `services` has four subscriptions:

- the `release/prod` branch of `tesserix/helivanta`;
- `main` of `tesserix/tesserix-k8s`, limited to `charts/apps/helivanta-api`,
  `charts/apps/helivanta-global-api`, `charts/apps/helivanta-shell` and their
  Application files under `argocd/prod/apps/helivanta/`;
- `ghcr.io/tesserix/helivanta/helivanta-api:release-prod`;
- `ghcr.io/tesserix/helivanta/helivanta-shell:release-prod`.

Both images use the `Digest` strategy, which reads that one tag. It never lists
the repository's tags, which run past Kargo's 100-tag discovery limit. Pods still
pull through the in-region GAR mirror, by the Freight's digest.

The Warehouse creates Freight only when both images' revision annotation equals
the `release/prod` commit. Moving a tag without the branch therefore creates
nothing, and neither does a release that fails between the two. Every workload
and the migrate hook run from `helivanta-api`; the UI runs from `helivanta-shell`.

The Stage sets each of the three Applications' `targetRevision` to the Freight's
tesserix-k8s commit, and `helivanta-app-of-apps` ignores that field so it does not
reset it. A rollback therefore restores the chart as well as the images. Freight
created before the chart subscription pins commit `08f5f729`, the charts
production ran at cutover.

A merged tesserix-k8s change to those paths creates Freight with the current
release commit and digests. It auto-promotes without a Release run; the
tesserix-k8s pull request review is its only approval. Two things still follow
tesserix-k8s `main` directly and are not part of Freight:

- the Application spec itself, including `valuesObject` feature flags, which
  `helivanta-app-of-apps` applies from `main`;
- the other Helivanta Applications (`postgres`, `nats`, `openfga`,
  `network-policies`, `db-schema-bootstrap` and the rest).

Rolling back Freight does not restore either of them; revert them in tesserix-k8s.

## Who can move `release/prod`

Several org apps hold `contents: write`, so the branch itself is locked rather
than trusting the script:

- Ruleset `release-prod` on `refs/heads/release/prod` blocks creation, update,
  deletion and non-fast-forward pushes. Its only bypass actor is deploy keys.
- The repository's only write deploy key is `release-prod`. Its private key exists
  only as the `RELEASE_PROD_DEPLOY_KEY` secret of the `production` environment.
- `production` admits deployments from `main` only and requires approval by
  `Sam123ben` or `mahesh-sangawar`. A workflow on another branch, or a bot
  dispatch nobody approves, never receives the key.

Maintainers and apps therefore cannot push `release/prod` directly; the push is
rejected. Rotating the key means replacing both the deploy key and the secret.
Adding any other write deploy key would also bypass the ruleset.

Database schema bootstrap SQL is not part of a release. It lives in
`tesserix-k8s/charts/apps/db-schema-bootstrap/schemas/helivanta/` and the
`helivanta-db-schema-bootstrap` CronJob applies it every 30 minutes once merged
there. Application migrations (`NNNN_<module>`) ship inside the release image.

## Verify a release

Promotion success and Argo `Synced` are not acceptance. Check all of these:

```sh
kubectl get promotions -n kargo-helivanta --sort-by=.metadata.creationTimestamp | tail -1
kubectl get stage prod -n kargo-helivanta   # Ready, Healthy and Verified
kubectl get jobs -n helivanta helivanta-api-migrate   # Complete; log says "migrations applied"
kubectl get freight -n kargo-helivanta -o wide   # newest has the commit and both digests
kubectl get deploy -n helivanta             # every workload ready on the Freight digest
```

## Roll back

Prefer rolling forward: merge a revert to `main`, then request and approve a release.

In an emergency, promote older Freight:
`kargo promote --project kargo-helivanta --freight <name> --stage prod`. The
Stage stays there until the next release, because Kargo never re-promotes Freight
that already has a Promotion for the Stage. For the same reason a failed
auto-promotion is not retried. Fix forward with a new release, or promote that
Freight manually once the cause is fixed.

Freight created before the image subscriptions existed has no digests; promoting
it deploys by the commit tag instead.

Undoing the chart pinning needs both tesserix-k8s#1622 and kargo-manifests#67
reverted. Reverting only the Kargo change leaves the three Applications on the
last commit they were pinned to.

Before rolling back across a migration, confirm the older code tolerates the
newer schema. Migrations are append-only and do not roll back.
