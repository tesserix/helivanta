# Proposed fulfilment review implementation

The matching specification and #169 design define the boundary. This implements
source/quantity binding required by the approval flow; it grants no clinical
approval and does not complete dispensing enforcement.

- [x] Observe missing input/function and route failures before implementation.
- [x] Generate canonical Go/Zod request and projection models.
- [x] Validate exact source, selected lines, repeats and remaining quantities.
- [x] Canonicalize owner-derived products and quantities into a distinct proposal hash.
- [x] Register a scoped, audited, no-store read serialized against the prescription parent.
- [x] Implement account-bound SDK and explicit-quantity UI with stale-result clearing.
- [x] Add hosted database cases for scope, stale/overfill denial, audit failure and no business effects.
- [x] Complete local focused builds/lints and final contract/release/source-inventory checks.
- [ ] Pass focused hosted PostgreSQL and protected checks at the final commit.
- [ ] Integrate qualified-person/policy authority and decision persistence before clinical activation.
- [ ] Implement and verify mandatory approval enforcement in the real sale/stock/financial transaction.

No local application, database, Docker or Colima is started. New projections stay
compiled unreleased. The existing migration-bearing PR remains draft; no live
migration or deployment is performed by this component's read operation.

## Executed local validation

Node 24; no local application, database or container was started.

```sh
# backend/
go test -race ./internal/modules/pharmacy -run '^TestVerificationFulfilment(Binds|Rejects|Canonical|Declares)' -count=1
golangci-lint run ./internal/modules/pharmacy/...
go build -o /tmp/helivanta-169-fill-api ./cmd/api
go run ./cmd/contractdocs
go run ./cmd/contractdocs -check

# root
pnpm --filter @helivanta/api exec vitest run src/pharmacy-fulfilment-review.test.ts src/pharmacy-verification-contract.test.ts src/pharmacy-verification-policies.test.ts
pnpm --filter @helivanta/api exec eslint src/pharmacy-fulfilment-review.ts src/pharmacy-fulfilment-review.test.ts src/pharmacy-verification-contract.ts
pnpm --filter @helivanta/shell exec vitest run components/pharmacy/prescription-fulfilment-review.test.tsx components/pharmacy/prescription-review-context.test.tsx
pnpm --filter @helivanta/shell exec eslint components/pharmacy/prescription-fulfilment-review.tsx components/pharmacy/prescription-fulfilment-review.test.tsx components/pharmacy/prescription-review-context.tsx
node --test scripts/pharmacy-verification-contract.test.mjs
node scripts/generate-pharmacy-verification.mjs --check
node scripts/generate-release-capabilities.mjs --check
node scripts/staff-operational-access-inventory.mjs --check
actionlint .github/workflows/pharmacist-verification.yml
API_URL=http://127.0.0.1:8081 NEXT_PUBLIC_ZITADEL_CLIENT_ID=ci-build-client-id NEXT_PUBLIC_ZITADEL_ISSUER_URL=http://auth.tesserix.localhost:20080 pnpm --filter @helivanta/shell build
```

The initial missing implementation and absent route assertions failed before
implementation. The SDK/component tests also failed for missing modules first.
The local build caught a missing canonical type export; it was corrected at the
SDK boundary before a successful production build. The hosted test suite also
covers source invalidation after partial/final supply, actual scope checks,
audit-before-disclosure and absence of business effects. Its result must be
recorded separately from local unit/compilation evidence.
