# Release by verified digest — 7 November 2026

PR [#1623](https://github.com/tesserix/helivanta/pull/1623) merged as
`49358838d5b1aba7ea25d118c3c16866b94933cf`. It is the second release through the
[approved release flow](../production-release.md), and the first one that created
new Freight from a `release/prod` commit.

The previous production revision was `e6cd589e`. The two commits since change
CI workflows, the release script, its tests, `.gitleaks.toml` and documentation.
No application code or migration changed.

The release script now points each image's `release-prod` tag at the digest it
verified. Kargo reads that one tag by digest, and every workload is pinned to
`<commit>@<digest>`.

## Verified code and artifacts

Every required check on the merged head `d26eaca0` passed.

The `go` job of [main CI run 42364958455](https://github.com/tesserix/helivanta/actions/runs/42364958455)
failed: `internal/modules/workforce` timed out after 20 minutes (1200.8s). That is
the limit tracked in [#1620](https://github.com/tesserix/helivanta/issues/1620),
not a failure in this change. Main CI stays red until #1620 lands.

[Main image publication](https://github.com/tesserix/helivanta/actions/runs/42364958171)
passed both image security gates and Cosign keyless signing, then advanced `deploy`
to the release revision.

| Image             | Published digest                                                          |
| ----------------- | ------------------------------------------------------------------------- |
| `helivanta-api`   | `sha256:be2513f4bef90a4e29860b94fe2d8a95a1044275f2be1d8117d029cd550ebe92` |
| `helivanta-shell` | `sha256:e7d8f0c0b1de209af5ce8d1f9918ff928519282b10993d6b6aacfe67ef1f2b20` |

## Release approval and Kargo execution

[Release run 42365445643](https://github.com/tesserix/helivanta/actions/runs/42365445643)
waited on the `production` environment until `Sam123ben` approved it. Its release
script verified both signatures, moved both `release-prod` tags to the digests
above and fast-forwarded `release/prod` to the release revision.

Kargo created Freight `7db1ae65d4827d16f21b1b3cdda8b730bf529831`
(`giggly-sabertooth`) with the commit and both digests. It auto-promoted in
Promotion `prod.01m6tt3asdccjqvkndjka0r1rf.7db1ae6`, which succeeded at
10:52:06Z.

## Chart Freight

[tesserix-k8s #1622](https://github.com/tesserix/tesserix-k8s/pull/1622) and
[kargo-manifests #67](https://github.com/tesserix/kargo-manifests/pull/67) then
added the Helivanta charts to Freight, as the
[runbook](../production-release.md#what-kargo-subscribes-to) now describes.

Kargo created Freight `f1fd143fc46ee36f913a4f183afa924a2e92e3f1`
(`ironic-buffoon`): the same commit and digests plus tesserix-k8s commit
`08f5f729`. It auto-promoted in Promotion
`prod.01m6twmgndwz71jq9cfgj177md.f1fd143`, which succeeded at 11:35:13Z.
`helivanta-api`, `helivanta-global-api` and `helivanta-shell` now have
`targetRevision` `08f5f729`, and each is Synced and Healthy. `helivanta-app-of-apps`
is Synced and kept the pin.

The rendered charts were unchanged, so no pod restarted. The migrate hook ran
again from the release image and logged `migrations applied`.

## Post-release evidence and limits

- Stage `prod` reports Ready, Healthy and Verified on `f1fd143f`.
- API, Global API, shell and all four API worker deployments have two ready
  replicas on `49358838@<digest>`. Every running container's image digest matches
  the published digests above.
- The public login page returns 200. Anonymous requests to the protected IAM
  proposal APIs return 401.

The chart Freight did not change a rendered manifest, so it has not yet shown
a chart change rolling out or rolling back. Nothing in the cluster checks image
signatures at admission. These checks did not exercise authenticated workflows
or mutate any Staff or patient record.
