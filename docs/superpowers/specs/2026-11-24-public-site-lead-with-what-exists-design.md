# Public site: lead with what exists, move the roadmap, close with an ask — design

Issue: #1837 (child of #542; follows #1255 and #1834, uses #1835 when it lands)
Date: 2026-11-24

## Why

The home page was audited on 2026-11-24 against the hosted build at 1568 px
wide, with the source in `apps/shell/app/page.tsx` and
`apps/shell/components/landing/` beside it. It is an honest page and a long
one, and the honesty sits where it costs the most:

- Chapter 5's board opens with "Nothing on this board is available yet".
- The trust section is three dashed cards labelled "Certification
  placeholder" and "Not yet certified".
- The platform grid is mostly `Planned` badges, with a note that availability
  varies.
- The page ends on a reviews placeholder, then a final sign-up button after
  it.
- There is one call to action in the hero and none again for the whole
  length of five chapters and three sections.
- Every board is an animated diagram, several of planned workflows; there is
  no screenshot of the product.
- The nav's mark is a Lucide heart-pulse icon in a grey square.
- Template devices remain once colour is fixed: left-border "Today:"
  callouts and dashed placeholder cards.

Everything true on the page stays true after this change. What moves is
where each truth is said. A hospital administrator reading the home page
should learn what they can use this quarter; a reader who wants the plan
should find it one click away, with the same status badges.

## Goals

1. Above the fold and through chapters 1 to 4, nothing says "Planned".
2. Real product screenshots stand in for diagrams wherever the feature
   exists: the hospital overview, the counter sale, lab orders, the ward
   board.
3. A `/roadmap` page carries chapter 5, the platform grid's planned rows and
   the trust section, with the existing status badges and the existing
   "nothing here is a claim of certification" wording.
4. The page closes with the primary action and a second route to a
   conversation, and the reviews placeholder goes.
5. The nav shows the real mark and wordmark once #1835 lands, and the
   platform placeholder icon goes with it.

## Non-goals

- New marketing claims, testimonials, logos of customers, or numbers. If it
  is not on the page today and not verifiable, it does not appear.
- A CMS, analytics, A/B tooling, SEO work beyond keeping titles and
  descriptions intact.
- The signup and sign-in flows (#1767, #1751).
- The ambulance page (`/ambulance`), which is a public safety page, not
  marketing, and is correct as it is.
- Copy rewrites of the chapters that stay. Re-ordering and trimming only.

## Anti-goals

- A "Planned" or "Future" badge rendered on `/`.
- A screenshot that shows real patient, staff or tenant data. Screenshots are
  taken from a seeded QA workspace with labelled QA records, and the tenant
  name shown is a fictional hospital.
- A screenshot that is a raster of the old slate look. Screenshots are taken
  after #1834 and #1836 land, or the change waits.
- Any colour, radius or type declared outside the shared tokens
  (`token-source.test.ts`, frontend.md §7).
- A Lighthouse LCP regression on the hosted build from the images.

## Constraints found in the code

- `page.tsx` composes `StoryAct` and `StoryChapter` components with a
  `ChapterBar` that lists chapters in two groups: "Available today" (1 to 4)
  and "What comes next" (5). The bar is tested in `chapter-bar.test.tsx` and
  the chapters in `story-chapter.test.tsx`.
- The platform grid reads `apps/shell/app/platform-catalog.ts`, which carries
  a status per module (`Foundation available`, `Available`, `Planned`,
  `Future`) and a colour class per group from the chart tokens.
- `landing.test.tsx` renders the page and asserts section order, headings
  and the mock of `next/font/local` for Anek.
- The public page uses `.home-slate`, which reads the shared tokens; there
  is no palette to migrate (#542 web slice).
- Sections have ids `how`, `why`, `platform`, `trust`, `reviews`,
  `maintenance`, and the nav links to four of them.

## Decisions

### D1 Chapters 1 to 4 stay; chapter 5 moves to `/roadmap`

The `ChapterBar` loses its second group. The `MaintenanceWatch` and `AiBoard`
boards, their narrative lists and their availability tables move unchanged
to the roadmap page under the heading "What comes next". The chapter
numbering on the home page runs 1 to 4; the roadmap page has its own
sequence, since it is its own story.

### D2 Screenshots replace diagrams where the feature exists (second half, #1838)

This decision ships separately as #1838, after #1834, #1836 and the
catalogue refresh, so the structural change in D1 and D3 to D8 does not wait.
Chapters 1 to 4 each get one screenshot of a shipped screen: chapter 1 the
hospital overview (`/dashboard`), chapter 2 the OPD visit list
(`/medicore/opd`), chapter 3 the pharmacy sales register (`/pharmacy/sales`,
the one trail of tablets and payments that exists today), chapter 4 the
stock overview (`/pharmacy/stock-overview`). There is no audit-trail screen
and no ward board yet, so neither is shot. The animated boards (`CareBoard`,
`PatientJourney`, `AuditTrail`, `StockWatch`) stay as the interactive
element beneath the screenshot; the screenshot leads. Images are
`next/image`, sized, `priority` only on the first, `loading="lazy"` on the
rest, stored under `apps/shell/public/screens/` at 2× the rendered width and
no wider than 1600 px, as WebP. A one-line caption under each names the
screen and the date it was taken, so a stale screenshot is visible as stale.
Until #1838 lands, each chapter shows its board as today.

### D3 The platform grid shows what exists; the rest is on the roadmap

On `/`, the platform section lists only modules whose status is
`Available` or `Foundation available`, under the same three group headings,
with the status badge kept (foundation is still a qualifier the reader
should see). A line under the section links to the roadmap: "The full
platform and its status, module by module." On `/roadmap` the grid renders
every module with every status, exactly as the home page does today, and the
`StatusGuide` hint moves with it.

### D4 Trust moves to the roadmap

The three placeholder cards and their sentence move to the roadmap page as
they are. The home page's `#trust` anchor and nav link go; the nav gains a
"Roadmap" link in its place. The public pages also gain a short links row
above `TesserixFooter` (Roadmap, Trust, Emergency help) rendered by
`page.tsx` and the roadmap page themselves. `TesserixFooter` is shared with
the workspace chrome (`hms-shell.tsx`) and does not change; a signed-in
clinician never sees a roadmap link.

### D5 The close

The reviews placeholder section is removed. The last section is a plain
close: a two-line heading in the same voice as the hero and the primary
action ("Start your workspace"). The contact flow
(`2026-10-26-contact-chat-design.md`, #1355) is not built yet, and a
"Talk to us" that leads nowhere would be the placeholder this document
removes elsewhere, so the secondary control is added by #1355 when it lands,
not stubbed here. No background band, no gradient; the canvas, the heading,
one control.

### D6 Template devices

Left-border "Today:" callouts become plain paragraphs beginning "Today:" in
the muted colour. Dashed-border placeholders exist only on the roadmap page,
where "placeholder" is the honest state. On the home page nothing is dashed.

### D7 The mark

The nav's `HeartPulse` in a grey square becomes `BrandLogo` with the wordmark
beside it, as #1835 D6 arranges, once that lands. If this change lands
first, the nav keeps the current icon and the swap is part of #1835.

### D9 The content inventory spans both pages

`landing-content.test.tsx` and `landing-content.fixture.json` freeze every
string on `/` (`2026-10-28-home-story-chapters.md` D6: no existing string may
disappear). Moving chapter 5, the trust section and the grid's planned rows
to `/roadmap` keeps every one of those strings in the product; the inventory
is extended to render both pages and assert the union, so the guarantee
becomes "no string disappears from the public site". The reviews
placeholder's strings do disappear, deliberately; the home-story-chapters
spec gets a note under D6 recording that this document supersedes it for the
reviews section and widens the inventory to two pages.

### D10 Catalogue truth is a prerequisite for screenshots, not for this change

The catalogue describes Pharmacy sales and returns as planned while the
routes ship. D3's filter alone would put a screenshot of a sale beside a row
saying sales are planned, so the catalogue refresh is its own issue and
blocks #1838. D3 ships now with the catalogue as it is; the filter is on
status, and corrected statuses flow through without further change.

### D8 Honesty stays visible

The roadmap page is linked from the nav, the footer, the platform section and
the trust mention in the hero's supporting paragraph if there is one. Nothing
is hidden; it is organised. The roadmap page title and description say what
it is.

## What is left to the builder

- Whether `/roadmap` is a route under `apps/shell/app/roadmap/` with its own
  `page.tsx` reusing the landing components, or a second export from the
  landing module. A route is expected; it needs no permission since it is
  public, and it gets the same `.home-slate` wrapper and Anek font. It is
  added to `middleware.ts`'s public allowlist and to `hmsPublicPages` in
  `apps/shell/eslint.config.mjs`.
- The sectors section's left-border callout is treated like the "Today:"
  ones (D6).
- The capture process for screenshots (viewport, scale, which QA tenant),
  documented in a comment beside the images' import.
- Exact section ids on the roadmap page.

## Verification

- `landing.test.tsx`: new section order on `/`; no element with text
  `Planned` or `Future` renders on `/`; the close section renders the two
  controls; the nav has the roadmap link.
- A new `roadmap.test.tsx`: chapter 5, the full grid with all statuses, the
  trust placeholders and the `StatusGuide` render there.
- `platform-catalog` filter has a unit test proving `Planned` rows are
  excluded on the home page and included on the roadmap.
- `pnpm turbo lint type-check test build format:check` green;
  `csp.spec.ts` green (images are same-origin).
- Lighthouse against the hosted build at 1280 and at 390 wide, LCP no worse
  than the current page; the number recorded on the PR.
- A phone-width walk-through screenshot set on the PR.
