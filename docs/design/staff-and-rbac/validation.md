# Design package validation

Date: 2026-11-06. Scope: [design](README.md), [research/evidence](research.md), [native draw.io](staff-and-rbac.drawio) and five SVG companion previews. This records artifact checks, not authorization implementation tests or regulatory sign-off.

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
- The native `.drawio` XML parses and contains five pages with unique cell IDs within each page. Every connector has existing source/target cells. Cards stay inside their 1600 × 1000 page and do not overlap other cards.
- All five SVGs parse and render in headless Google Chrome through Playwright. Browser text bounds show no text extending beyond the page. All five rendered page images were visually inspected for hierarchy, labels, connector routing and legibility.
- Diagram previews and native shapes were generated from the same layout data. Visual inspection covered browser-rendered SVG previews; native diagrams.net import/export was not exercised. XML validity and shared layout do not claim a native-editor round-trip test.
- An initial local-link check caught the not-yet-created validation ledger; creating this file resolved it. An initial ImageMagick SVG rasterization failed because the local font configuration was unavailable; Chrome provided the successful rendering path.
- No application code, existing documents, permissions, routes, canonical contracts or migrations were modified by this package. These checks originally preceded the user’s later commit/merge request; that request authorizes a documentation-only branch/PR. No issue update or production action is part of this package.

Application test suites, builds, linters and contract regeneration were not run: this task produces design artifacts only. Future implementation needs the contract, isolation, lifecycle, concurrency and failure tests described in the design; these artifact checks provide no runtime security guarantee.

## Merge preparation

The user requested committing and merging this documentation. A clean worktree at `/tmp/helivanta-staff-rbac-docs-20261106` was created from `origin/main` (`ab9f8a65`), leaving the older dirty checkout untouched. Before commit, ADR links were updated to current numbering, current-main implementation differences were recorded in the research reconciliation, and department inheritance was aligned with accepted ADR-0055. Local links, formatting, XML and whitespace were checked again in that worktree. Only this design directory is included in the change.
