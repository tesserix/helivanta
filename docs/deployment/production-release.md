# Production release

Production changes only when someone approves a release. After approval every
step is automatic: images, application migrations, connector and UI roll out
together from one commit.

| Step                 | Trigger                                                            | Result                                                                                                                                                                                                                            |
| -------------------- | ------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Feature branch or PR | Push                                                               | `Images` builds and Trivy-scans both images locally. Nothing is pushed or signed.                                                                                                                                                 |
| Merge to `main`      | PR merge                                                           | `Images` builds, scans, signs and pushes `helivanta-api` and `helivanta-shell` tagged with the full commit id, then fast-forwards `deploy`. Production does not change; deploy that tag to a test environment manually to try it. |
| Request              | `gh workflow run release.yml --repo tesserix/helivanta --ref main` | The run waits on the `production` environment. Pass `-f commit=<id>` to release an older `deploy` commit.                                                                                                                         |
| Approve              | A maintainer approves the run's `production` deployment            | [`scripts/release-prod.sh`](../../scripts/release-prod.sh) verifies both images and fast-forwards `release/prod`.                                                                                                                 |
| Promote              | Kargo, within 5 minutes                                            | Warehouse `services` in `kargo-helivanta` creates Freight for the new `release/prod` commit, and the `prod` Stage auto-promotes it.                                                                                               |
| Roll out             | Kargo `argocd-update` steps, in order                              | `helivanta-api` runs its migrate hook, then `helivanta-global-api`, then `helivanta-shell`, all on that commit's image tag.                                                                                                       |

The release script refuses, and leaves `release/prod` unchanged, when the
commit is not on `deploy`, is behind `release/prod`, or either image lacks a
keyless signature from `images.yml` on `main`. An image built or signed anywhere
else is never releasable.

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
kubectl get deploy -n helivanta             # every workload ready on the new tag
```

## Roll back

Prefer rolling forward: merge a revert to `main`, then request and approve a release.

In an emergency, promote older Freight:
`kargo promote --project kargo-helivanta --freight <name> --stage prod`. The
Stage stays there until the next release, because Kargo never re-promotes Freight
that already has a Promotion for the Stage. For the same reason a failed
auto-promotion is not retried. Fix forward with a new release, or promote that
Freight manually once the cause is fixed.

Before rolling back across a migration, confirm the older code tolerates the
newer schema. Migrations are append-only and do not roll back.
