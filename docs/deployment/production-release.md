# Production release

Production changes only when someone approves a release. After approval every
step is automatic: images, application migrations, connector and UI roll out
together from one commit.

| Step            | Trigger                                                            | Result                                                                                                                                                                  |
| --------------- | ------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Merge to `main` | PR merge                                                           | `Images` builds, scans, signs and pushes `helivanta-api` and `helivanta-shell` tagged with the full commit id, then fast-forwards `deploy`. Production does not change. |
| Approve         | `gh workflow run release.yml --repo tesserix/helivanta --ref main` | [`scripts/release-prod.sh`](../../scripts/release-prod.sh) fast-forwards `release/prod` to the `deploy` head, or to `-f commit=<id>`.                                   |
| Promote         | Kargo, within 5 minutes                                            | Warehouse `services` in `kargo-helivanta` creates Freight for the new `release/prod` commit, and the `prod` Stage auto-promotes it.                                     |
| Roll out        | Kargo `argocd-update` steps, in order                              | `helivanta-api` runs its migrate hook, then `helivanta-global-api`, then `helivanta-shell`, all on that commit's image tag.                                             |

The release script refuses, and leaves `release/prod` unchanged, when the
commit is not on `deploy`, is behind `release/prod`, or either image lacks a
keyless signature from `images.yml` on `main`. A feature-branch image is never
releasable.

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

Prefer rolling forward: merge a revert to `main`, then approve a release.

In an emergency, promote older Freight:
`kargo promote --project kargo-helivanta --freight <name> --stage prod`. The
Stage stays there until the next release, because Kargo never re-promotes Freight
that already has a Promotion for the Stage. For the same reason a failed
auto-promotion is not retried. Fix forward with a new release, or promote that
Freight manually once the cause is fixed.

Before rolling back across a migration, confirm the older code tolerates the
newer schema. Migrations are append-only and do not roll back.
