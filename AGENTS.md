# Helivanta shared agent rules

These rules apply to Codex, Claude and other coding agents across this repository.
Helivanta is an enterprise healthcare SaaS application: patient safety, customer
isolation, accountable clinical decisions and reliable integrations are part of
correctness. Read applicable nested instructions as well.

## Required reading before development

Before proposing a design or changing application code:

1. Read [engineering principles](docs/standards/engineering-principles.md), the
   [API contract index](docs/api/README.md) and
   [product ownership and coverage](docs/api/portfolio.md).
2. Read the affected domain specification linked from the contract index and the
   applicable backend/frontend standards. For API, event, schema or integration
   work, also read [HTTP contracts](docs/api/http-contract.md),
   [integration contracts](docs/api/integration-contracts.md),
   [Global API rules](docs/api/global-connector.md),
   [schema ownership](docs/api/schemas.md) and the relevant findings and review
   gates in the [delivery plan](docs/api/review-plan.md).
3. Identify the data owner, consumers, verified tenant and narrower resource
   scope, canonical schema source, current implementation and proposed changes.
   Check the actual handlers, migrations, registered contracts and relevant
   issues; documentation does not replace source verification.
4. For India-specific hospital workflows, read the
   [India HMS knowledge index](docs/domain/india/README.md) and use the relevant
   [domain skill](.agents/skills/hms-india/SKILL.md). Consult only the affected
   domain references and `IN-*` acceptance criteria. Verify consequential legal,
   clinical, tax, drug-schedule and payer rules against current primary sources,
   actual state/service scope and qualified review; do not invent them from model
   memory or mistake a research note for an activated production policy.
5. For roles, permissions, staff access, departments, approvals, tenant-configured
   workflows or public/private sector differences, read the
   [organization knowledge index](docs/domain/organization/README.md) and use the
   [organization access skill](.agents/skills/hms-organization-access/SKILL.md).

Proposed architecture and draft contracts are not implemented or approved APIs.
Do not infer a deployed service, FHIR compliance, clinical authority or a working
end-to-end integration from a document, route, event publication or passing schema
validator. Resolve discrepancies against source and accepted decisions, correct
stale documentation in the same change, and record unresolved domain decisions
before consumers rely on them.

## Enterprise acceptance criteria

- Route UI/app business API traffic through the Global API, with explicit product
  destinations and no direct client-to-product bypass. Product APIs retain data
  ownership and authorization. Apply the [onboarding and end-to-end gates](docs/api/global-connector.md)
  before exposing a new API or service; routing alone does not prove completion.
- Enforce verified tenant and hospital/outlet/premise/base/facility scope on the
  server, including object-level authorization. UI gating, shared ownership,
  affiliations, supplied IDs and client headers are not authority. Identity,
  authorization and data-isolation checks fail closed when unavailable.
- Preserve a single owner for patient identity, encounters, prescriptions,
  diagnostic results, stock and financial records. Use published contracts and
  authorized projections; do not introduce duplicate masters, cross-module table
  access or independently copied wire models.
- Define validation, legal state transitions, concurrency, durable retry/replay
  behavior and partial-failure recovery for commands. Clinical, physical stock
  and financial actions need attributable evidence; corrections and compensation
  preserve history rather than erase completed actions.
- Protect patient/staff/commercial personal data, credentials and private files.
  Minimize event, log and export payloads; reauthorize disclosure and file release.
  Preserve audit consequences for sensitive reads as well as writes.
- Keep data access bounded and use the owner's units, precision, currency,
  calendar/timezone and effective-version rules. Report unavailable or stale data
  explicitly; do not disguise missing integrations with fabricated success,
  production mock data or zero-valued reports.
- Deliver correct, bounded slices. Do not defer isolation, security, validation,
  audit, recovery or relevant tests as later hardening, and do not expand into
  speculative services or unrelated product work to claim enterprise readiness.
- Verify the actual changed behavior, including unauthorized scope, stale state,
  duplicates and dependency failures where relevant. Run applicable tests, builds
  and linters; report commands actually run and any remaining limitations.

## Keep contracts and implementation aligned

Update affected specifications, examples, errors, permissions, schemas, event
contracts and consumer expectations in the same change. Evolve breaking contracts
through the documented review and migration process; do not silently redefine v1.
Edit canonical inputs and regenerate their Go/Zod outputs instead of hand-editing
generated models.

When routes, permissions, published payloads, subscriptions or migrations change,
run from `backend/`:

```sh
go run ./cmd/contractdocs
go run ./cmd/contractdocs -check
```

Also run the affected canonical generators and checks described in the
[schema index](docs/api/schemas.md). Registry parity is not payload conformance or
consumer compatibility. Implementation summaries and PRs must identify the
contracts consulted or changed, validation evidence and unresolved review items;
do not claim enterprise, clinical or regulatory readiness beyond that evidence.

## Repository visibility

`tesserix/helivanta` must remain public. Never make it private, including as
part of a temporary public/CI/private workflow. This is a persistent user
instruction. Verify visibility before release; do not change it back after CI.
