# Design package validation

Date: 2026-11-06. Scope: [design](README.md), [research/evidence](research.md), [native draw.io](staff-and-rbac.drawio) and seven SVG companion previews. This first section records the original merged artifact checks, not implementation tests or regulatory sign-off. The later department-team candidate is described in [its delivery record](department-team-management.md).

## Commands actually run

From the repository root:

```sh
graft ask "staff RBAC hospital departments independent pharmacy tenant membership delegated administration" --source
git remote get-url origin
gh issue list --repo tesserix/helivanta --state all --search 'RBAC staff department' --limit 15 --json number,title,state,url
gh api graphql -f query='{ repository(owner:"tesserix",name:"helivanta") { i22:issue(number:22){number title state} i287:issue(number:287){number title state} i616:issue(number:616){number title state} i429:issue(number:429){number title state} i418:issue(number:418){number title state} i432:issue(number:432){number title state} } }'
node_modules/.bin/prettier --write docs/design/staff-and-rbac/README.md docs/design/staff-and-rbac/research.md
python3 /tmp/build_helivanta_staff_diagrams.py
node /tmp/render_helivanta_staff_diagrams.cjs
python3 /tmp/validate_helivanta_staff_design.py
node_modules/.bin/prettier --write docs/design/staff-and-rbac/validation.md
node_modules/.bin/prettier --check docs/design/staff-and-rbac/*.md
git diff --check -- docs/design/staff-and-rbac
```

The temporary generation/validation scripts are authoring helpers, not application code or a new repository build dependency. The delivered XML/SVGs are standalone artifacts. Source/contract inspection used targeted `sed`, `cat`, `rg` and read-only GitHub queries. External primary sources were searched/opened through the connected web tool and are linked in the research record.

## Results

- Markdown formatting and whitespace checks passed.
- All relative Markdown file links resolve within the repository.
- The native `.drawio` XML parses and contains seven pages with unique cell IDs within each page. Every connector has existing source/target cells. Cards stay inside their 1600 × 1000 page and do not overlap other cards.
- All seven SVGs parse and render in headless Google Chrome through Playwright. Browser text bounds show no text extending beyond the page. All seven rendered page images were visually inspected for hierarchy, labels, connector routing and legibility.
- Diagram previews and native shapes were generated from the same layout data. Visual inspection covered browser-rendered SVG previews; native diagrams.net import/export was not exercised. XML validity and shared layout do not claim a native-editor round-trip test.
- An initial local-link check caught the not-yet-created validation ledger; creating this file resolved it. An initial ImageMagick SVG rasterization failed because the local font configuration was unavailable; Chrome provided the successful rendering path.
- No application code, existing documents, permissions, routes, canonical contracts or migrations were modified by this package. These checks originally preceded the user’s later commit/merge request; that request authorizes a documentation-only branch/PR. No issue update or production action is part of this package.

Application test suites, builds, linters and contract regeneration were not run: this task produces design artifacts only. Future implementation needs the contract, isolation, lifecycle, concurrency and failure tests described in the design; these artifact checks provide no runtime security guarantee.

## Merge preparation

The user requested committing and merging this documentation. A clean worktree at `/tmp/helivanta-staff-rbac-docs-20261106` was created from `origin/main` (`ab9f8a65`), leaving the older dirty checkout untouched. Before commit, ADR links were updated to current numbering, current-main implementation differences were recorded in the research reconciliation, and department inheritance was aligned with accepted ADR-0055. Local links, formatting, XML and whitespace were checked again in that worktree. Only this design directory is included in the change.

## Partner collaboration extension

The user added independent suppliers working with multiple hospitals and sharing roles with named hospital users or hospital-managed groups. Added `partner-collaboration.md`, primary-source research (OpenFGA groups, GS1 healthcare supply-chain framework and WHO storage/distribution guidance), and two editable diagram pages. Supplier internal authority, hospital membership and bilateral disclosure grants remain distinct. Proposed group sharing and full procurement integration are explicitly not claimed implemented.

Commands actually run for the extension:

```sh
python3 /tmp/build_helivanta_staff_diagrams_v2.py
node /tmp/render_helivanta_staff_diagrams_v2.cjs
python3 /tmp/validate_helivanta_staff_design_v2.py
/Users/samyakrout/Desktop/samyak-work/projects/new-repos/helivanta/node_modules/.bin/prettier --write docs/design/staff-and-rbac/*.md
/Users/samyakrout/Desktop/samyak-work/projects/new-repos/helivanta/node_modules/.bin/prettier --check docs/design/staff-and-rbac/*.md
git diff --check
```

Result: 62 relative links resolve; seven native pages contain 54 nonoverlapping cards and 46 valid connectors; all seven SVGs parse/render without page text overflow. The two added previews were visually inspected, including label/arrow spacing. No application/canonical contract/permission change is included.

## Department-team implementation follow-up

The local candidate now includes a current roster and an owner-local atomic
scheduled transfer, with canonical models, real Global API/owner integration and
lost-response browser recovery. See the [complete command/result ledger](department-team-management.md#validation-record)
and [ADR-0061](../../adr/0061-department-team-and-atomic-transfers.md). This updates
the package to eight native pages and eight SVGs; the original seven-page merge
record above remains historical.

The latest artifact checks found 78 resolving local links, 62 nonoverlapping cards,
52 valid connectors and no SVG page overflow. Chrome rendered all eight SVGs and
the new page was visually inspected. Native diagrams.net import/export remains
unexercised. Runtime acceptance is recorded separately from artifact validity;
neither establishes regulatory or production readiness.

## Independent membership candidate — 6 November 2026

Added the standalone [native diagram](diagrams/09-independent-membership.drawio)
and [SVG preview](diagrams/09-independent-membership.svg). XML checks confirm
13 nonoverlapping surfaces, 11 valid connectors and geometry within the page.
`rsvg-convert` rendered the SVG and its PNG was visually inspected for text,
contrast and connector spacing. The existing eight-page master remains unchanged;
native diagrams.net import/export of the new file remains unexercised.

Local behavior evidence:

- Dedicated QA Postgres/OpenFGA/NATS, actual IAM/Workforce owners and Global API,
  race detector: selected pre-employment/posting fixture chain passed in
  280.921 seconds. It includes the owner candidate projection, current peer
  source binding, self-enrolment refusal, independent Staff review, changed-intent
  and stale-version rejection, exact nomination/command recovery, collection
  reads and following department transfer/cutoff regression cases.
- `pnpm --filter @helivanta/shell exec vitest run 'app/(workspace)/workforce/partner-teams'`:
  nine tests passed, including scoped non-governance browsing, lost nomination
  and approval response recovery with a single PUT, and removal of Staff details
  after current read authority is withdrawn.
- New SDK group/member/contract tests, API/shell typechecks and scoped ESLint
  checks have passed. Full-package checks, final builds and real browser runtime
  acceptance remain required; consult the delivery ledger for current status.

These are local candidate checks, not evidence of complete supplier disclosure,
stock/finance composition, production migration, merge or Kargo promotion.

### Main integration and actual-browser verification — 7 November 2026

After combining upstream department leave-review work, 84 template migration
tests and 81 affected UI tests passed; the API typecheck, generator checks,
production build and targeted IAM cases passed. Actual-owner Global API tests
with the race detector and the new browser scenario passed in 231.519 seconds.
The browser screenshot was visually inspected: the scoped staff user sees the
retained suspended membership and recovery confirmation. Exactly one suspension
PUT was issued despite the intentionally lost post-commit response.

This test caught and fixed a tenant-versus-hospital permission discovery bug:
ordinary staff now use their current hospital capabilities, with command-time
reauthorization. Unit coverage includes an empty tenant-wide permission list with
a valid scoped role. No mocked tenant-wide permission is required for the browser
scenario. The screenshot uses only synthetic fixture data.

## Supplier grant preparation — 7 November 2026

This checkpoint implements private supplier proposal preparation only. It does
not implement effective grants, bilateral acceptance, partner disclosure, stock
fulfilment or financial reconciliation. The delivery ledger retains those open
stages and the canonical onboarding prerequisite.

Commands actually run in the isolated supplier worktree:

- `pnpm --filter @helivanta/api test`: 926 tests in 93 files passed.
- `pnpm --filter @helivanta/shell test`: the unchanged full rerun passed 1,725
  tests in 195 files. The first run had three timeout-related failures in two
  unchanged files; those files also passed independently, with 11 tests.
- API and shell `type-check`, ESLint and the production shell build passed.
- `go test -race ./internal/archtest ./cmd/contractdocs`: passed, including the
  full architecture suite (84.228 seconds).
- Affected-package `go vet` and `go build ./...` passed. The final repository
  `golangci-lint run ./...` passed with zero findings.
- `go test -race ./internal/modules/iam -run TestPartnerGrantDraft -count=1
-timeout=5m`: passed. Actual Postgres tests cover invalid inputs, explicit nulls,
  immutable exact recovery, changed intent, tenant/cursor isolation, missing
  governance authority, audit withholding and creation rollback. Direct database
  UPDATE/DELETE attempts cannot change or remove saved evidence.
- The creation rollback assertion was mutation-tested: deliberately omitting the
  creation audit produced the expected failing assertion (expected 503, actual
  200). Restoring the audit made the draft suite pass. No mutation remains.
- `go test -race ./internal/globalapi -run
TestSupplierGrantDraftThroughActualOwnerAndGraph -count=1 -timeout=5m -v`,
  with the browser URL configured, passed in 10.295 seconds against the built
  shell and actual Global API/IAM/OpenFGA/PharmaConnect/Postgres. The browser
  saved once, lost the successful response and recovered by GET with one PUT;
  the graph outage returned 503 and no operating authority. Only synthetic
  accounts were used. OIDC sign-in and positive profile integration were not
  exercised; the fixture's unavailable profile reader correctly returned 503.
- Canonical grant generation, contractdocs parity, operational inventory parity
  and `git diff --check` passed. Inventory parity does not certify operating
  authorization; 840 existing surfaces remain unclassified.

The earlier full IAM race run failed the unchanged
`TestAssignmentProjectionAcknowledgesOnlyCurrentScope` case with expected 202,
actual 504. Its unchanged isolated rerun passed (34.194 seconds). A fresh full
IAM race run is pending; neither its deadline nor assertions were weakened.
Remote CI, merge, image publication and Kargo promotion are not established by
these local checks.

Read-only production discovery reconfirmed API, Global API and shell at
`62f3f7fd180f5ce9800d70807217b1cb2131d54a`, two ready replicas each, and the
previous Kargo promotion succeeded. No production change occurred.
