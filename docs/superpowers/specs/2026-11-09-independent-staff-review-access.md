# Independent Staff directory and reviewer access

Live QA has a separate independent-account/duty reviewer in the synthetic stock
workspace. It has review permissions but no product catalogue/directory role.
The existing Staff source screens reject it before reaching Workforce discovery.
This UI proxy contradicts the current typed-employer contract: discovery is
current-member-scoped, basic directory reads require explicit employer access,
and account-link review additionally requires both current manager context and
review permission. A product catalogue role is not Staff authority.

Keep all owner APIs and authorization unchanged. Let the pharmacy/supplier Staff
screens verify the session then request Workforce's bounded discovery, including
for a scoped reader without global product permissions. No access row returns
no accessible employers; refused/unavailable discovery stays an explicit error.
The hospital projection panel remains gated by pharmacy catalogue permission,
so a private independent Staff reviewer does not request hospital contexts.

Add `/workforce/pharmacy-staff` and `/workforce/supplier-staff` as UI aliases of
the existing components and expose them in Staff navigation. This is navigation,
not permission: explicit current employer scope is still required to reveal any
Staff and manager plus action permission still protects review. Existing product
URLs remain compatible. Current Staff versions continue to come from Workforce;
independent approval, stale-basis denial, audit and immutable recovery are unchanged.
Do not add a second review API, bypass private HR context, fabricate Staff status,
or grant catalogue/HR permissions to make a test pass.

No API, permission, event, migration, copied master or datastore changes. Reuse
the existing bounded directory page sizes, scope headers, indexed owner reads and
latency/availability targets. At 5 peak administrative requests/s, this removes
unneeded hospital reads from reviewer views; it adds no new infrastructure cost.
Rollback removes the aliases and restores the UI gate; owner records and all
review history remain intact. Actual current manager scope, participant/legal
review and permitted stock are separate live acceptance prerequisites.

Acceptance: scoped pharmacy and supplier reviewers can use existing controls
without catalogue roles; role-only/no-scope accounts see no Staff or editor;
refused discovery remains unavailable; absent catalogue permission causes no
hospital or product directory calls; navigation points to the shared owner-backed
components. Test the real SDK boundary and preserved workspace headers, then run
full shell/shared UI tests, typecheck, lint, formatting and a production build.
No local apps or containers. ORG-AC-01/02/05/07 and the published Workforce and
independent-account contracts apply; no ORG-D decision changes.
