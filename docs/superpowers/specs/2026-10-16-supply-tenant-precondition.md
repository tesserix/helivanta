# Supply expected-tenant precondition

Issue #1170; epic #1133; production finding from #1169 acceptance. Owner Sam123ben.

## Decision

The session remains authoritative for identity and tenant. An optional
`X-Helivanta-Expected-Tenant` is only a stale-workspace precondition: it cannot
select or override a tenant. Parse and compare it with the authenticated tenant
inside Supply's existing shared `principal` guard. Reject malformed, nil or
mismatched values with `409 session_changed`, before private reads, body parsing,
audit writes or GCS access. The existing review panel already offers a workspace
refresh for this error. Equivalent UUID representations and requests without the
optional header remain compatible.

This covers every Supply route, including directory/access, profile revisions,
premises/licences, evidence and licence reviews, through the existing call graph.
A check only in reviewScope was rejected because evidence and other Supply
handlers share the same stale-workspace risk. A new authentication authority,
tenant selector, cross-module dependency or datastore is unnecessary.

Assets are private supplier profiles, evidence and integrity of review decisions.
The threat is an authenticated browser completing a delayed request after a
workspace change. Forced RLS and explicit party grants remain independent
boundaries; this precondition is additional protection, not a substitute.

## Verification and release

First reproduce the production 200-versus-409 failure through public API tests
against disposable PostgreSQL/NATS. Cover every route family, malformed/nil IDs,
matching/absent preconditions, and valid mutation attempts with no row/audit/event
changes. Retain foreign-tenant denial and exact retry tests. Then run full Go
race/coverage, vet/build/lint and required web/build/CI checks. Repeat actual
production negative acceptance after a specifically approved application rollout.

There is no migration, backfill, new dependency, additional service or measurable
storage cost. UUID comparison adds no I/O to existing requests. Rollback uses the
previous app image and preserves `0004_supply` history; it restores the missing
precondition, so forward repair is preferred. This does not close broader #1135
or B2B stories.
