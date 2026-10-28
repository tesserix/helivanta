# Home page: the scroll story in consistent chapters — design

Issue: #1413 (follows #1354, which added the animated story)
Date: 2026-10-28

## Why

The home page tells Helivanta's story through six animated sections: care board,
patient journey, audit trail, stock watch, maintenance watch and AI board. The
content and the diagrams work. The page reads as cluttered. Measured at 1440px on
2026-10-28:

- **No gutters.** `pageWidth` runs to `max-w-[120rem]` with `5vw` side padding,
  so content spans nearly edge to edge on wide screens.
- **No section anatomy.**
  - Four sections put the step list in a 21rem column beside the diagram, where
    it runs up to 948px tall. Two sections stack them.
  - Every section opens with the same hairline and heading, so nothing marks
    where one section ends and the next begins.
- **Nothing groups built and not-yet-built work.** Every section reads as
  equally real, even though the maintenance board's own note says "Nothing on
  this board is available yet".
- **No way to orient.** On a 13,139px page there is no indication of where the
  reader is or what comes next.

A static prototype (`prototype-v2.html`, shown to the user on 2026-10-28) kept
every diagram and fixed only the presentation. The user approved that direction.

## Goals

1. **All current content stays.** This includes every story, scenario, register
   row, step, availability label and note, and every section after the story. A
   test proves it (D6).
2. **One container and gutter for the whole home page.**
3. **One chapter anatomy, repeated:**
   - a numbered header with title and lede;
   - the existing diagram, steps and availability beneath it, unchanged.
4. **Two groups.**
   - "Available today": chapters 1–4.
   - "What comes next": chapter 5, maintenance and AI together.
5. **A sticky chapter bar** that marks the current chapter.

## Non-goals

- **Palette.** Slate is #1412. This change reads the existing `--night-*`
  tokens, so it composes with either palette.
- **Hero copy, CTA choice, and the contact route** (#1355).
- **Module guarantees** (#1359).
- **Removing or trimming content.** That includes the stock and maintenance
  register rows.
- **The trust and review placeholders.** They are kept as they are.
- **The animated components, including their layout:**
  - stories and data;
  - motion and timings;
  - the `StoryPicker`;
  - the figures' accessible names.

  The chapter shell wraps them unchanged.

## Decisions

### D1. Home container

- Add `homeWidth = "mx-auto max-w-[84rem] px-6 sm:px-8"` to `app/page-width.ts`.
  That is 1344px less 2×32px gutters, so 1280px of content. Beside the existing
  21rem step column and 3rem gap, each diagram keeps its current 896px width.
- The home page uses `homeWidth` everywhere.
- `pageWidth` stays as it is for `/workspace`.

### D2. Chapter shell

`components/landing/story-chapter.tsx` exports `StoryChapter`:

- `id`, `number`, `title`, `lede`, `children`, and `tone: "plain" | "band"`.
- It renders a `<section aria-labelledby>` holding:
  - a number badge;
  - an `h2` with the existing heading text;
  - the lede;
  - the children.
- `band` adds the `--night-surface` background with top and bottom hairlines.
  Chapters alternate bands so each one has a visible start and end.
- It keeps each section's existing `id` and accessible name, so the region tests
  and in-page anchors continue to work.

### D3. Steps stay beside the diagram

The story components are unchanged. Each keeps its own grid, with the diagram
and the step list side by side from `lg`/`xl` up, stacked below that.

Stacking every diagram above its steps was built and measured on 2026-10-28:

| Layout                            | Page height at 1440px |
| --------------------------------- | --------------------- |
| Current page                      | 13,194px              |
| Diagram stacked above steps       | 15,281px              |
| Steps beside the diagram (chosen) | 13,866px              |

Stacking was longer because the diagram height and the step-list height add
together instead of sharing a row. The full step text is content (Goal 1), so
it cannot be shortened to make the stacked layout fit. The user chose to keep
the side-by-side layout.

### D4. Acts

- The page shows two act headers: "Available today", above chapters 1–4, and
  "What comes next", above chapter 5.
- The "What comes next" header uses a dashed rule. It states that nothing in the
  act is available yet, which matches the maintenance and AI notes.
- Chapter 5 renders `MaintenanceWatch` and then `AiBoard`. Each keeps its own
  `h3` and region, so both remain directly addressable. No content is hidden
  behind a tab.

### D5. Chapter bar

`components/landing/chapter-bar.tsx` (client) is a `nav` sitting directly under
the header.

- It is sticky at `top-0`, with a translucent `--night-board` background.
- It lists the five chapters, grouped under the two act labels.
- An `IntersectionObserver` sets `aria-current="true"` on the link for the
  chapter in view.
- Without JavaScript the links are plain anchors and still work.
- Targets are at least 44px tall. On narrow screens the bar scrolls sideways.

### D6. Content inventory test (proves Goal 1)

- `app/landing-content.test.tsx` renders `LandingPage` and collects every
  non-empty, whitespace-normalised text node.
- It asserts that every string in `app/landing-content.fixture.json` is still
  present.
- The fixture is generated once from the page **before** this change, from the
  same test environment, and committed alongside the test.
- New strings (act labels, chapter-bar links, numbers) are allowed. Removing any
  existing string fails the test.
- The test is shown to fail by deleting one register row from the stock data.

### D7. Type sizes

Measured at 1440px before this change, about a quarter of the page's text was
under 14px (11px 11%, 12–13px 15%), and reading text was 14px.

- Reading text (steps, availability, notes, later-section prose): 14px → 16px.
- Labels, legends and status pills: 12px → 13px.
- Diagram text: 11px → 12px minimum. Diagram layouts are unchanged; no text
  overflows its box at 1440px (checked in a browser).
- Navigation and buttons stay at 14px.

After: nothing under 12px, and 67% of text at 16px. The page grows from 13,866px
to about 15,100px, because the longer step columns beside each diagram grow.

The Planned and Future pills differed only by a `--home-line` border (1.42:1).
They now use `--home-muted` (7.2:1) to meet WCAG 1.4.11.

### D8. Help text

Help text explains terms. It is not a way to fit small text.

- `Term` wraps a term in a dotted-underline button that opens a `Popover` from
  `@helivanta/ui` (Radix).
- It opens on click, tap or keyboard, closes on Escape and returns focus. It
  never relies on hover, which touch screens do not have (WCAG 1.4.13).
- The button's text is only the term, so the sentence still reads the same.
- `StatusGuide` sits next to every "Availability" heading and in the platform
  intro. It defines the four status labels in the wording of
  `docs/product/public-platform-catalog.md`.
- The terms defined are:
  - National Health Mission;
  - CMO, CMHO;
  - Rogi Kalyan Samiti;
  - CHC, PHC;
  - quarantine.

  The definitions come from `docs/domain/india/public-and-private-sectors.md`
  and the page's own audit-trail copy. Abbreviations are not expanded where the
  docs do not expand them.

- Terms inside the animated components' data (for example "indent") are not
  wrapped in this slice.

The D6 inventory also matches whole-element text, so a sentence split by a
`Term` button still counts. Renaming a wrapped term was shown to fail it.

## Accessibility

- One `h1`. Chapters stay `h2`. Maintenance and AI become `h3` inside chapter 5.
- Sticky bar targets are at least 44px. Focus rings follow the existing
  `focus-visible:outline-(--night-ink)` pattern.
- Reduced motion behaves as before. The chapter bar adds no motion.

## Testing

- The existing `landing.test.tsx` and the component tests stay green. The only
  edits are role-level changes needed for maintenance and AI moving under
  chapter 5.
- D6 inventory test.
- A chapter-bar unit test: links, act grouping, and `aria-current` set from a
  stubbed `IntersectionObserver` entry.
- Visual check at 1440px of the before and after page.

## Not covered by this slice

- Mobile-specific redesign of the diagrams. They keep their current responsive
  behaviour.
- The CTA and copy decisions from the 2026-10-28 audit.
