# Professional-source runtime delivery — #169

Observed 2026-11-26 UTC (2026-11-27 Australia/Melbourne). This is a source-runtime
delivery record, not completion of pharmacist clinical verification.

## Reviewed and delivered artifacts

- [Application PR #1897](https://github.com/tesserix/helivanta/pull/1897), main
  `d2129d535fb27593118eee249865116780fdf834`. Tested PR head `633b2732` and
  merged main have identical tree `a91ab658ed900fa822c144636a567bab1ae5bb26`.
  All PR checks passed; no check or approval bypass was used.
- [Runtime infrastructure PR #1726](https://github.com/tesserix/tesserix-k8s/pull/1726),
  main `c37b065bdc7ccf03a9a89d93c187d7a71f2b3ec1`. Atlantis applied precisely
  three professional-object-path IAM grants: no replacements or deletions.
  Its required `atlantis/apply` check passed before normal merge.
- [Credential preparation PR #1728](https://github.com/tesserix/tesserix-k8s/pull/1728),
  main `0daaa50754fa81071654d3aff4808d350a74ad1e`. This selects create-once
  credential preparation only; LOGIN, credential projection and worker activation
  remain disabled. The job `helivanta-practitioner-evidence-secret-seed-e6513eed`
  completed successfully through GitOps; its safe log reports initialization
  with existing versions preserved. No password was read from OpenBao or printed.
- [Signed images](https://github.com/tesserix/helivanta/actions/runs/45912041837)
  and [production release](https://github.com/tesserix/helivanta/actions/runs/45912884139)
  succeeded for the exact main commit. Normal environment review was used.
- API/worker image digest: `sha256:1e75080745625990177cd745cd5a802ac86232e80ca0d5890f8cd1898e56c262`.
- Shell image digest: `sha256:5fa19ca67dbd3b17db6fb4d6e057dcf13da7ce015b8cba4b576e66fc3c245a53`.
- Kargo freight: `9279adec652a4886c5c373c21cfa55d4e82515c6`.
  Promotion: `prod.01m8d478j06gf830462f1904px.9279ade`.

## Actual observations

The authenticated operator identity was checked before rollout. Project: `tesseracthub-480811`;
context `gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke`; application
namespace `helivanta`; Kargo stage `kargo-helivanta/prod`.

The migration pod exited zero and logged `migrations applied`. A read-only query
against `schema_migrations` returned every identifier `0001_practitioner` through
`0016_practitioner`. Metadata queries confirmed nineteen practitioner tables
have both RLS and FORCE RLS enabled. The dedicated
`helivanta_practitioner_evidence_worker` role has LOGIN, inheritance, superuser,
BYPASSRLS, CREATEDB and CREATEROLE all false and zero memberships. No credential
or patient/staff record was selected or printed.

API, Global API and shell each reached two updated/ready/available replicas.
HTTP checks returned `200` for `/login` and `401` for an anonymous
`/api/v1/iam/me/permissions` request. Browser attachment was attempted but Codex
reported that its browser network policy requires an application update.
No manual browser acceptance is claimed.

## Health and acceptance limits

Kargo selected the exact new source/images; the stage remains `Progressing`
because the existing document, pharmacy-evidence and supplier-evidence workers
are unready. ClamAV serves engine 1.5.4, daily signatures 28194 dated
2026-11-25 06:26:59 UTC. FreshClam runs hourly and reports this database current;
the upstream TXT record also reports version 28194. The signature timestamp
exceeds the 36-hour readiness threshold. Do not increase that threshold, falsify
freshness or bypass the 48-hour maximum to make deployment appear healthy.

All 68 professional HTTP routes remain compiled `unreleased`, regardless of
GrowthBook or tenant pilot switches. Professional evidence processing is not yet
activated. Registration attestations retain `clinical_authority: not_evaluated`.
Clinical authority decision ADR-0077, independent person/account linkage and
local privileges, decisions/history, second pharmacist, prescriber response loop
and the atomic active-sale stock/financial gate remain required in #169.

## Validation scope

Source PR checks include real hosted database/RLS/role tests, Go race/coverage,
web and E2E checks. Focused local builds of API, migrator, Global API, contractdocs
and the new worker passed and their generated binaries were removed. Local
contract, capability, architecture, gateway and release checks passed.
Credential preparation has 54 passing focused tests plus Ruff and strict mypy;
its wrong-owner mutation is detected and restored before passing validation.
No local app, database, Docker or Colima was started. Passing these checks does
not establish clinical or regulatory acceptance or close the parent issue.
