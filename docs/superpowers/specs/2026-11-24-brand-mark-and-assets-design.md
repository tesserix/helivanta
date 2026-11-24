# Brand mark and asset set — design

Issue: #1835 (child of #542; lands after #1834)
Date: 2026-11-24

## Why

Helivanta has no mark. `apps/shell/app/icon.svg` is a heartbeat drawn over a
spiral in a green that matches no token, the in-app `BrandLogo` fallback is a
Lucide heart-pulse icon in a grey square, the public nav uses the same icon,
and there is no favicon set, no app icon, no social image and no wordmark.
Tenant-branded workspaces (#24) will carry a hospital's logo, which makes it
more important, not less, that the platform has a signature of its own.

On 2026-11-24 the user chose a mark from a set of concepts explored on a
design canvas and in Google Flow: a lowercase h whose shoulder is a single
pulse line. It was chosen over letter-only, chain, heart and loop marks
because it reads as a letter at 16 px, carries one element that can take a
product colour, and says hospital and heartbeat without being either cliché
on its own. The vector was drawn by hand on a 64-unit grid; the generated
images came from Flow and were used as reference only.

The assets already exist, generated under `brand/` by `brand/generate.mjs`.
This document records the decisions behind them and what landing them means.

## Goals

1. One source for every mark: a change to geometry or hue is one edit and one
   run, and CI fails if a generated file is stale.
2. The app, the public pages, the browser tab, the home-screen icon and the
   link preview all show the same mark in the same colours.
3. A family rule that lets PharmaConnect, sold alone to independent
   pharmacies, carry its own colour without a second brand.
4. The mark survives 16 px on a non-retina display.

## Non-goals

- Tenant logos, tenant palettes, tenant favicons: #24.
- Email templates and printed bill headers: they pick the lockup up when
  those features are built; this change ships the files, not the usages.
- A brand guideline document beyond `brand/README.md` and this spec.
- Changing the product names ("PharmaConnect" and so on). The lockups use
  plain words ("helivanta pharmacy") because that is what a counter screen
  and a tab title need; the product names stay in code and the catalogue.

## Anti-goals

- Any raster used as a source. Every output derives from the SVG geometry in
  `generate.mjs` or from `outlines.json`, which itself derives from the
  repo's Anek Latin file.
- A second typeface or a font file embedded in an SVG. The wordmark is
  outlines; nothing is licensed or fetched.
- A mark whose accent collides with a clinical status colour. The pulse is
  ultramarine or a product hue; never good green, warn amber or critical red.
- A favicon that depends on the title to be recognisable.

## Decisions

### D1 Geometry

A 64-unit grid. Stem `x 8–20, y 6–58`. Leg `x 40–52, y 34–58`. Pulse: a
4-unit line `M20 32 H27 L31 19 L36 46 L39 32 H52`, butt caps, mitre joins. No
radius anywhere. The leg is half height so the silhouette is an h, not an H;
the pulse's flat run across the leg's top is intentional and is what reads as
a crossbar at small sizes.

A second drawing exists for 16 px only (`mark-16.svg`): the same shape on a
16-unit grid with bar edges on whole pixels and the pulse on half-pixel
centres. The 64-grid shape scaled to 16 put the stem ends on half pixels and
blurred both bars. Checked by rendering both at 16 px and enlarging with
nearest-neighbour scaling.

### D2 Colours

Ink `#0b1324` for the bars, accent `#2b4ee6` for the pulse. On ink the bars
are white and the pulse is `#7aa0ff`, because `#2b4ee6` on `#0b1324` is
2.2:1 and disappears. One-colour versions exist in ink and in white for print
and stamps. These are the #1834 tokens; `brand/README.md` says so, and the
#1834 PR must update the README's hex list if a value moves during review.

### D3 Lockup

Mark left, wordmark right. Wordmark cap height 44 units against the mark's
64, so the h ascender in the word sits a little below the mark's stem top
and the two read as mark plus word, not as two letters. Gap equals the stem
width, 12 units. Nothing else in the lockup: no dot, no tagline, no "HMS".
The wordmark is Anek Latin at weight 700 and tracking −0.03 em, shaped with
HarfBuzz so kerning is the font's, then outlined.

### D4 Where the mark appears alone and where with the word

Alone where the surroundings name the product: favicon, app icon, the rail,
the account avatar fallback, loading screens, printed stamps. With the word
where they do not: the sign-in header, the public nav, email headers, bill
and receipt headers, the social image, decks. This is a rule for the
components that follow, recorded here so that the first email template does
not invent a third lockup.

### D5 The family

Products are the same geometry with the pulse in the product hue, and the
hue set is the chart token set from #1834: pharmacy `--hms-violet`
(`#86198f`), lab `--hms-sky` (`#0e7490`), supply `--hms-amber` (`#92400e`),
emergency `--hms-rose` (`#c13b5e`). The platform and the hospital core use
the accent. Product lockups add the product word after "helivanta" in the
product hue at weight 600; product icons and favicons carry no letters, since
at 16 px the hue is a hint and the tab title does the naming. MediCore is the
hospital core and uses the platform mark, not a variant.

### D6 Files that land in the app

The Next metadata file conventions do the wiring, so no `<link>` tags are
written by hand:

- `apps/shell/app/icon.svg` replaced with `brand/next-app/icon.svg`.
- `apps/shell/app/favicon.ico` (16, 32, 48 as PNG-in-ICO), `apple-icon.png`
  (180, full-bleed ink so iOS's own mask applies), `opengraph-image.png`
  (1200×630) and `manifest.webmanifest` added from `brand/next-app/`.
- `icon-192.png`, `icon-512.png` and `maskable-icon-512.png` served from
  `apps/shell/public/` for the manifest.

`BrandLogo` in `packages/ui/src/branding.tsx` renders the mark inline (an
SVG component, not an `<img>`) when a tenant has no logo, in `currentColor`
for the bars so it follows the rail's ink or white, with the pulse on the
accent token. The home page nav uses the same component with the wordmark
beside it. The `HeartPulse` import goes.

### D7 Staleness is a CI failure

`brand/generate.mjs` is run in the web CI job and `git diff --exit-code
brand/ apps/shell/app/icon.svg apps/shell/app/favicon.ico
apps/shell/app/apple-icon.png apps/shell/app/opengraph-image.png
apps/shell/public/icon-*.png apps/shell/public/maskable-icon-512.png` must
be clean. `outlines.json` is committed and not regenerated in CI, because
that step needs a Python toolchain the job does not have; a change to the
wordmark text or weight regenerates it locally as the README says. PNG output
must be byte-stable for this to work: the generator sets a fixed sharp
density and compression and no timestamps, and the check is proven by
running it twice.

### D8 CSP and sizes

All files are same-origin, so `apps/shell/lib/csp.ts` does not change.
`opengraph-image.png` is around 31 KB and the whole set under 200 KB; nothing
here needs an image CDN.

## What is left to the builder

- Whether the inline mark component lives in `packages/ui/src/branding.tsx`
  or its own file beside it; the test uses `renderWithProviders` either way.
- The exact CI step placement in `.github/workflows/ci.yml` (it runs after
  `pnpm install`, before the build, in the web job).
- The sharp resolution in CI: `require.resolve("sharp")` from the shell
  workspace, as the README shows, or a dev dependency on the root. The README
  path is preferred because it adds no dependency.

## Verification

- `pnpm turbo lint type-check test build format:check` green in Actions.
- The staleness check (D7) green, and proven to fail by editing one hue in
  `generate.mjs` without regenerating.
- `packages/ui` Vitest: `branding.test.tsx` covers the inline mark fallback,
  the `currentColor` bars and the accent pulse, and that a tenant logo still
  wins when present.
- `e2e/tests/csp.spec.ts` and `smoke.spec.ts` green.
- By eye on the hosted build: the browser tab at 16 px on a non-retina
  display, the home-screen icon on one iOS and one Android device, and the
  link preview through a social card validator; screenshots on the PR.

## Follow-ups this document creates

- Email header lockup when the first transactional email template is built.
- Bill and receipt header when #1693 (print and PDF) lands.
- Tenant favicon override as part of #24, if a tenant asks for it.
