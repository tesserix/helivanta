# Scope restoration must preserve an explicit workspace navigation

Observed in CI run 40548628104, E2E job 122345984495: the chrome-preservation
test clicked Emergency from Pharmacy, but the address stayed `/pharmacy` with
newly restored pharmacy/outlet parameters. The private trace records the click
and subsequent Pharmacy snapshots; no unsaved-changes dialog was present.
This is a release investigation, not a changed supervision requirement.

Pharmacy owns organization/outlet identity and API authorization. The shell's
Pharmacy workspace owns its selected scope and copied address. Its current
automatic scope effect calls `router.replace` after async directory resolution;
that starts a navigation which can compete with the explicit rail action. Scope
selection remains client context only, never data authority. Global API routing,
product authorization and bounded owner queries remain unchanged.

Use the installed Next.js native History API integration for automatic address
repair. It updates `usePathname`/`useSearchParams` without a route transition.
Before remembering or replacing scope, require both the browser pathname and
query to match the effect's captured owner address. Preserve unrelated filters
and fragments; preserve replace semantics (no added back-stack entry). A late
effect must not overwrite a newer route, query or cached selection. Explicit
user scope changes continue through the existing unsaved-change guard.

This adds no service, API, policy, dependency or infrastructure. It removes an
unnecessary route transition per default/restored scope. Existing paginated
scope queries and route availability targets are unchanged; the acceptance
budget is zero automatic router transitions for address restoration. Browser
history/storage failure never supplies business authority or fabricated data.

Acceptance: failing-first component assertions cover copied scope without route
navigation, delayed resolution after leaving the page, and an address changed
before hooks catch up. Existing scope precedence, unknown-outlet denial, filter
reset and unsaved-edit cases must remain green. Reproduce the rail journey in a
real local browser, then run relevant tests, typecheck/lint and a Next.js build
using the repository's supported Node version. Keep the CI failure/trace as
evidence; do not increase timeouts or remove chrome preservation assertions.
