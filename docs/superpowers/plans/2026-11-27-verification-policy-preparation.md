# Verification policy preparation execution

See the matching specification, [API contract](../../api/pharmacy-verification-policies.md)
and the complete #169 acceptance design. Preparation does not close the parent issue.

- [x] Observe input and route tests fail before implementation.
- [x] Generate canonical request/response Go and Zod contracts and SQL constraints.
- [x] Implement immutable outlet-scoped storage, exact replay and audited history.
- [x] Add hosted database tests for persistence, concurrency, scope, pagination,
      forced RLS, canonical text/duration bounds, immutability and audit failure.
- [x] Add account-bound SDK and unreleased administrative preparation UI with
      explicit settings, stable-command recovery and confirmed-conflict recovery.
- [x] Regenerate route contracts, release admission and Staff source inventory.
- [x] Pass focused local Go race tests, API build, Go vet and package lint.
- [x] Pass SDK/UI tests, API typecheck, focused ESLint and shell production build.
- [ ] Run the real PostgreSQL tests and all required checks on the exact PR head.
- [ ] Obtain named migration approval before merging this migration-bearing change.
- [ ] Deploy through Kargo and perform actual acceptance before exposing the feature.
- [ ] Continue verified-person authority, qualified independent clinical approval,
      separate publication and the remaining full verification workflow.

## Local evidence

Executed without local apps, databases, Docker or Colima:

```sh
# backend/
go test -race ./internal/modules/pharmacy -run '^Test(VerificationPolicyDraftRejects|VerificationPolicyPreparation|VerificationPolicyDraftIntent|VerificationDecisionIntent)' -count=1
go build -o /tmp/helivanta-169-policy-api ./cmd/api
go vet ./internal/modules/pharmacy
golangci-lint run ./internal/modules/pharmacy/...
go run ./cmd/contractdocs
go run ./cmd/contractdocs -check

# repository root, Node 24
pnpm --filter @helivanta/api exec vitest run src/pharmacy-verification-policies.test.ts src/pharmacy-verification-contract.test.ts
pnpm --filter @helivanta/api exec tsc --noEmit
pnpm --filter @helivanta/api exec eslint src/pharmacy-verification-policies.ts src/pharmacy-verification-policies.test.ts
pnpm --filter @helivanta/shell exec vitest run components/pharmacy/verification-policy-drafts.test.tsx
pnpm --filter @helivanta/shell exec eslint components/pharmacy/verification-policy-drafts.tsx components/pharmacy/verification-policy-drafts.test.tsx components/pharmacy/pharmacy-workspace.tsx
node --test scripts/pharmacy-verification-contract.test.mjs
node --test scripts/release-capabilities.test.mjs
node scripts/generate-pharmacy-verification.mjs --check
node scripts/staff-operational-access-inventory.mjs --check
API_URL=http://127.0.0.1:8081 NEXT_PUBLIC_ZITADEL_CLIENT_ID=ci-build-client-id NEXT_PUBLIC_ZITADEL_ISSUER_URL=http://auth.tesserix.localhost:20080 pnpm --filter @helivanta/shell build
```

The shell build initially refused missing production build configuration, then
passed with the repository's synthetic CI build settings. No local server was
started. A stale-account regression first returned 500 instead of 409; reusing
`stockCommandScope` made it pass before storage. UI tests first failed for absent
implementation, then for lost focus and missing conflict recovery, and passed
after the respective changes. A generator test caught coupled interval limits;
Go now derives each interval bound independently from its canonical field.

These results do not establish PostgreSQL migration correctness, live clinical
authority, worker readiness, deployed UI behavior or completion of #169.
