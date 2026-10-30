# Plan: Load more never appends to a stale first page (#833)

Spec: `docs/superpowers/specs/2026-10-08-load-more-never-follows-a-stale-page-design.md`

- [x] Failing hook test first. It reproduced `[b, c, d]`, the newest row
  missing.
- [x] `paged.ts`: `appendNextPage`, which joins with
  `cancelRefetch: false` and asks again after a joined refetch, at most 3
  attempts.
- [x] Mutations: `cancelRefetch: true`; a single attempt.
- [x] Gate: `pnpm turbo lint type-check test build format:check`.
