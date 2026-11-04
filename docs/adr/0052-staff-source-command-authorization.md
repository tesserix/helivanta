# ADR-0052: Owner-controlled Staff source authorization

Date: 2026-11-02. Status: proposed for the unreleased posting implementation.
This records the architecture to prove locally; it approves no production
migration, permission change, regulatory policy or independent-service protocol.

## Context

ADR-0044 separates postings from grants. ADR-0049 requires reviewed employment
and effective cutoff despite stale sessions or projections. The current IAM
`ScopedCommandGuard` and employment-basis writer already hold authorization
and product work in a shared `tenantdb` command. The asynchronous department
handoff proves an observed historical revision, not current operating eligibility.

Repeating Staff in Pharma creates a second master. Reading Workforce tables
from Pharma breaks ownership. Checking an HTTP/event snapshot and then committing
without a source fence permits suspension to win between check and commit.

## Proposed decision

Extend the existing authorization-provider seam with narrowly typed, owner-
implemented current-source guards, composed at the application wiring boundary.
Products receive an authorization interface, never another module, repository,
database transaction or callable business API. Returned proof is limited to
opaque canonical IDs, epochs and the effective deadline; no private HR finding,
registration content or employee copy crosses this seam. Business data handoffs
continue through published owner contracts/events. This is not general module
RPC and must not become an alternative to event integration.

IAM verifies and locks the exact independent assignment and current identity.
Workforce then verifies its reviewed Staff/login/employment/posting source under
its own RLS purposes. Organization verifies the canonical current structural
scope under its own owner guard. Pharma finally verifies outlet membership,
professional acceptance/duty and the actual operational object. Each phase joins
the active guarded command on the same database. The outer guard controls commit.

Lock order is IAM authority, Workforce sources, Organization hospital/tree gate,
then Pharma outlet/duty/business rows. Each owner defines a stable internal key
order. A source writer must either take only its own locks or enter this same
order; it must never hold a later owner while requesting an earlier one. Source
state remains locked until command commit. Cutoff uses explicit database instants
and the earliest effective deadline; no timer or async tuple removal is required
to deny the next affected command.

The interface must bind verified tenant/subject, server-required action, exact
scope and owner-selected Staff/generation/posting. It rejects forged or escaped
proof, different database/tenant, closed callback and changed source. The receiving
product rechecks any pre-lock resource lookup after obtaining its own locks.
The provider cannot accept caller-selected SQL, owner names, remote URLs,
permissions or a subject impersonated by a client body/header.

No module may import/call another module's implementation or query its tables.
The implementation review must keep this authorization composition outside
business module coupling, explicitly document its allowed provider methods and
add architecture tests for that closed set. An interface or shared transaction
alone does not waive the existing isolation rules. If those constraints cannot
be met, stop that implementation and revise this proposal before exposing it.

## Consequences and acceptance

This relies on the current modular monolith's single guarded database. It does
not survive service/database extraction unchanged; a separately reviewed fencing
protocol and equivalent race evidence are required before extraction. OpenFGA
and storage I/O remain outside row locks, followed by current-state validation.
Unavailable authority denies; audit/outbox failure rolls back the whole command.
Protected evidence release keeps its existing before/after-I/O checks.

Tests must demonstrate both source/command orderings, expired deadlines, stale
graph/session proof, role/link/posting/supervisor revocation, callback escape,
cross-scope/database misuse and rollback. Each critical lock/epoch predicate must
be observed failing under mutation. Actual modern and legacy Pharma consumers
must be covered before operational activation; the legacy tenant-only dispense
lineage remains an explicit migration obligation. Governance/recovery remain
separate and source ending never disables unrelated employment/global identity.

See the [full posting specification](../superpowers/specs/2026-11-02-effective-staff-postings.md)
for the canonical owner records, independent decisions, employer migration and
browser/production acceptance that remain required.

## Scoped posting candidate enforcement — 2026-11-03

`CurrentStaffActorGuard` exposes only `WithCurrentStaffActor`, with a scoped
callback and opaque Staff/link/generation references. Production consumers are
Organization's `posting_choices.go` and Workforce's `posting_authority.go`.
`platform.StaffActors` selects the Workforce provider at application wiring;
the two modules receive that interface through their existing dependency fields.
The corresponding harness and exact integration-test call sites are explicit.

`internal/archtest/staff_actor_test.go` enforces that closed consumer/wiring set
and the single authorization-method signature. Temporary bare consumer/provider
method references in an unrelated Organization source file were observed failing
both rules, then removed. Existing cross-module import/table-ownership checks
remain unchanged. This guard does not certify the complete source-writer lock
graph or permit new operating consumers without their own reviewed contract and
runtime acceptance. The ADR remains proposed for the unreleased candidate.


## Current posting source owner composition — ADR0060

The reviewed Workforce-owned `posting_source_guard.go` additionally consumes the
same narrow actor interface for a current-source observation under an explicit
source-use IAM capability. It restores its owner context before its caller and
retains mandatory atomic evidence. This does not admit general product business
calls or replace product-specific permissions/unit/professional policy. Only the
new platform wiring resolver may construct/select the actor dependency for this
owner composition; the Workforce source constructor receives the injected
interface. Actual Pharma operating integration remains a separate contract gate.
See [ADR0060](0060-current-posting-source-use.md) and its real IAM/FGA/DB fault,
expiry and two-ordering source-writer tests. Existing unreviewed consumer checks
and the actor interface's single-method assertion remain enforced.
