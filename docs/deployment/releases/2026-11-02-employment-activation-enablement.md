# Reviewed employment source activation enabled — 2026-11-02

Status: deployed and verified. The product owner explicitly requested production
activation be switched on and remaining Staff work continue.

- Application source: `02b1e2b7e5a0fb9fa1fdbc65e449ca322cfda56a`, unchanged.
- GitOps: [tesserix-k8s PR #1619](https://github.com/tesserix/tesserix-k8s/pull/1619),
  merged commit `58226e30296a5f75514d1bc72f94283da8cb1c7d`.
- Change: owner API `WORKFORCE_EMPLOYMENT_ACTIVATION_ENABLED=true` in Argo values.
- Account: `unidevidp@gmail.com`; project: `tesseracthub-480811`.
- Context: `gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke`;
  namespace `helivanta`; owning app `argocd/helivanta-api`.
- No new migration, identity, membership, role, login link or assignment grant.
  The ledger remains 201; production employment generation count remains zero.
- Other installations retain compiled/chart default-off behavior.

The 2026-11-01 source release remains immutable: its flag-off verification records
that earlier rollout. This record captures the later explicitly authorized
configuration enablement; no published tag was retargeted.

## Verification

All required GitOps checks passed before merge. Argo applied the owning repository
change through `helivanta-app-of-apps`; a normal refresh requested reconciliation.
No Deployment or database SQL was imperatively patched.

Both actual running API pods carry activation=true, are ready with zero restarts
and use published digest
`sha256:aea6335fcdb758f6fc9bacae0df7934504fd6b195642ad5a8ef8d4dbe0f2fdf2`.
All six application deployments remain 2/2 updated, ready and available on the
approved source. API Argo is Synced/Healthy at the GitOps commit above. The real
production owner session reloaded Staff normally in the intended synthetic tenant
and still showed its six existing Staff records. No file was uploaded/downloaded
for this enablement smoke.

Both CNPG instances are healthy, Running with zero restarts. The separate Postgres
Argo application still has a pre-existing failed pharmacy secret-seed Job
`helivanta-pharmacy-evidence-secret-seed-7ad7a2f0`, whose Degraded transition was
10:48:57 UTC, before this flag rollout. Database readiness and the API's healthy
rollout were verified independently; that pharmacy task was not changed here.

The full local digital UI-created source, independent approvals, explicit
activation, independently reviewed login/role admission, account-disable and
source-revocation cutoffs passed. Five deterministic signed lock cases and a
negative shared-lock mutation passed their intended assertions; the final full
Global API race suite passed in 113.627s. Detailed commands, evidence and cleanup
are in [the acceptance record](../../qa/2026-11-02-employment-activation-enablement.md).

## Availability and remaining scope

Authorized activators need separately approved activation plus enrollment grants
and their current reviewed Staff/login actor. Source eligibility and consumption
remain tenant/hospital scoped and independently reviewed. Activation creates no
membership, login link, operational role, payroll or clinical privilege.
Production was not granted synthetic activation/reviewer roles to manufacture a
positive business transaction. Positive digital business acceptance above is local;
production verification covers the actual flag, pinned image, readiness, preserved
data and existing owner UI.

Full Staff Management is incomplete. Legacy/governance cohorts and every relevant
suspension, resignation, termination, expiry, rehire, asynchronous/external and
cross-product cutoff require their own acceptance. Inventory parity currently
reports 734 surfaces, 17 classified and 717 unclassified. Neither classification
nor this enabled flag is complete operational enforcement. Keep #417/#1035 and
remaining tracker gates open.

Private mode-600 evidence: `activation-readiness-20261102/production-activation-before.json`,
`production-activation-after.json` and `production-staff-after-enablement.jpg`
under the existing local Staff QA directory. Credentials and private receipts are
not committed.
