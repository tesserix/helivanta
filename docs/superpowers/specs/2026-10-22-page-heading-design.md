# One page heading

Resolves #1274. Applies #1255 spec D3 ("PageHeading, no eyebrow") to every workspace page, and enforces it.

## Problem

Only the dashboard uses `PageHeading`. The other workspace pages each copy the landing-page header:
- an accent `text-xs font-semibold` eyebrow;
- a `text-3xl` heading, sometimes a slogan;
- a lede.

Section panels reuse the eyebrow above their `h3`. EmergencyConnect pages and `(workspace)/error.tsx` render a second `<h1>` under the shell's. The #1255 lint catches only uppercase or wide-tracked eyebrows, so none of this fails CI.

## Decisions

- **D1 PageHeading opens every page.** The shell header's `<h1>` already shows the page's name (`zones.ts` page label), so a title that only repeated it would print the name twice. `PageHeading.title` is therefore optional:
  - a page passes no title, just one plain sentence saying what the page is for, plus its `actions` (the hospital, pharmacy or outlet picker);
  - a title is used only when it adds something the shell does not show: a record (a case number) or a view ("Hospital overview").

  No slogans: a description says what the page does.
- **D2 No eyebrows.** The eyebrow is deleted, not restyled; the heading already says where you are. A section panel keeps its `h3` alone.
- **D3 One `<h1>`.** The shell header (`HmsShell`) owns it. Workspace content starts at `h2`. `(workspace)/error.tsx` uses `PageHeading`.
- **D4 Enforced by lint.** `hmsWorkspacePages(files, ignores)` in `@helivanta/config/eslint` adds three rules to every design rule, applied to `app/(workspace)/**` and `components/**` in the shell (except `components/login-branding.tsx`, which heads the login page):
  - no `<h1>`;
  - no heading element at `text-2xl` or larger;
  - no class string holding `text-xs`, `font-semibold` and `text-(--hms-accent)` together.

  Display numbers on non-heading elements, accent pills and accent links stay allowed. Tests are exempt, as they are from the design rules.

## Not covered

- Card surfaces (#1275) and loading/empty/error states (#1276).
- Page copy and layout below the header stay with the zone issues #1256–#1263.
