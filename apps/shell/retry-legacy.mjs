// Workforce files that still draw their own read retry or refresh (#1595).
// They are exempt from helivanta/no-hand-rolled-retry until their slice
// converts them. retry-legacy.test.mjs fails when a listed file no longer
// breaks the rule, so this list only shrinks and is deleted when empty.
export const RETRY_LEGACY = [
  "app/(workspace)/workforce/access/page.tsx",
  "app/(workspace)/workforce/pharmacy-binding-command.tsx",
  "app/(workspace)/workforce/pharmacy-binding-workspace.tsx",
  "app/(workspace)/workforce/posting-approval-history.tsx",
  "app/(workspace)/workforce/posting-cutoff-workspace.tsx",
  "app/(workspace)/workforce/posting-drafts-panel.tsx",
  "app/(workspace)/workforce/posting-proposal-decision.tsx",
  "app/(workspace)/workforce/posting-review-workspace.tsx",
  "app/(workspace)/workforce/posting-submission-workspace.tsx",
  "app/(workspace)/workforce/postings/page.tsx",
];
