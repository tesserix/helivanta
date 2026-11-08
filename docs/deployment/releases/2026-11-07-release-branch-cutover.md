# Release-branch cutover — 7 November 2026

PR [#1619](https://github.com/tesserix/helivanta/pull/1619) merged as
`e6cd589ed9f6e189e094b1a345ddec7cd806243d`. It is the first production release
through the [approved release flow](../production-release.md). Images are now
published only from `main`. Production changes only when an approved
`Release - Production` run advances the locked `release/prod` branch, which Kargo
auto-promotes.

The previous production revision was `06bed958`. The seven changed files are CI
workflows, release scripts, their tests and documentation. No application code
or migration changed, so the release adds no migration and changes no behaviour.

## Verified code and artifacts

All ten required checks on the merged head `0f602026` passed. The `go` check passed
on a rerun after `internal/modules/workforce` exceeded the 20-minute `go test`
limit; that limit is tracked in
[#1620](https://github.com/tesserix/helivanta/issues/1620). The feature-branch
builds of that head pushed no image: both `helivanta-api` and `helivanta-shell`
returned 404 for its commit tag.

[Main image publication](https://github.com/tesserix/helivanta/actions/runs/42349075216)
passed both image security gates and Cosign keyless signing, then advanced `deploy`
to the release revision.

| Image             | Published digest                                                          |
| ----------------- | ------------------------------------------------------------------------- |
| `helivanta-api`   | `sha256:c22c6ad5cdeac2a90ff95c535747d9185296a58517ef2ad3c6d187c673ccea19` |
| `helivanta-shell` | `sha256:af907330065cf70eb413cae959e0dd7910b9d26152ef47987a1cc4f49a9dc373` |

## Release approval and Kargo execution

Before this release, ruleset `release-prod` rejected a direct maintainer push that
tried to create `release/prod`.

[Release run 42349263759](https://github.com/tesserix/helivanta/actions/runs/42349263759)
waited on the `production` environment until `Sam123ben` approved it. Its release
script verified both image signatures against `images.yml` on `main`. It then
created `release/prod` at the release revision, using the environment's deploy key.

[kargo-manifests #65](https://github.com/tesserix/kargo-manifests/pull/65) then
pointed Warehouse `services` at `release/prod` and enabled `prod` auto-promotion.
Kargo already held Freight `c1feaa7c41ef64c014ca3e968ebd57f3a9e75570`
(`prodding-camel`) for this commit from the former `deploy` subscription. It
auto-promoted that Freight in Promotion `prod.01m6tdn8cg1x9vdcsm7sp3pkha.c1feaa7`,
which succeeded. The `helivanta-api-migrate` hook ran from the release image and
logged `migrations applied`.

## Post-release evidence and limits

- Stage `prod` reports Ready, Healthy and Verified.
- API, Global API, shell and all four API worker deployments have two updated, ready
  replicas on the release tag. Every running container's image digest matches the
  published digests above.
- The public login page returns 200. Anonymous requests to the protected IAM
  proposal APIs return 401.

The first release reused existing Freight, so it did not prove that a new
`release/prod` commit creates Freight. Nothing in the cluster checks image signatures
at admission; the signer is checked only when `release/prod` moves. These checks
did not exercise authenticated workflows or mutate any Staff or patient record.
