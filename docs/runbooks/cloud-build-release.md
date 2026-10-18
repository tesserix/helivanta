# Reviewed Cloud Build release fallback

Keep the repository public. GitHub account billing/spending failures cannot be
repaired by changing repository visibility or granting public PRs a privileged
runner. `cloudbuild.release.yaml` builds the reviewed application commit into GAR
without changing the existing GitHub pipeline.

1. Run tests, type checks, lint, the production build, browser journeys and a
   staged secret scan. Commit and review the exact source first.
2. Resolve the full reviewed commit ID. Use `git archive` for that commit and upload
   its archive to the existing `tesseracthub-480811_cloudbuild` source bucket.
   Never submit a developer working directory containing local credentials.
3. Submit the committed build recipe with `_COMMIT` equal to that source commit.
   The recipe pins GCP mirrors of all application bases and publishes the API and
   web images to `asia-south1-docker.pkg.dev/tesseracthub-480811/global` under that
   exact commit tag. The existing Compute build identity supplies GCP access.
4. Require a successful build with verified Cloud Build provenance. Record the
   build ID, source object generation, application commit and resulting digests.
   Scan both published images before promotion. Do not claim GitHub keyless signing.
5. Promote only those exact reviewed artifacts through the owning GitOps repository.
   Apply prerequisites first, then schema/API/web, then enable processing and test
   using isolated synthetic identities. Retain the clinical/public activation gates.
6. Confirm Argo sync/health, exact live digests, migration completion, worker
   readiness, authenticated routes and negative authorization behavior. Record
   observed results separately from local tests.

The Dockerfiles retain pinned public base defaults for unprivileged public-PR
builds; this recipe explicitly uses the same digests from private GCP mirrors.
Production pulls and this fallback build never require GHCR access.
