# Helivanta brand source

Issue #1835. Everything under `svg/`, `png/` and `next-app/` is generated;
edit `generate.mjs` (mark geometry, hues, lockup rule) or regenerate
`outlines.json` and run the generator. Never hand-edit an output.

## Regenerate

```sh
# 1. Outline the wordmark and tagline from the repo's Anek Latin file.
#    Needs fonttools, brotli and uharfbuzz in a throwaway Python env.
python3 -m venv /tmp/fontenv && /tmp/fontenv/bin/pip install fonttools brotli uharfbuzz
/tmp/fontenv/bin/python brand/outline.py apps/shell/app/fonts/anek-latin.woff2 brand/outlines.json

# 2. Rasterise with the sharp already in the pnpm store (Next depends on it).
node brand/generate.mjs "$(pnpm --filter @helivanta/shell exec node -p "require.resolve('sharp')")" brand
```

## What is here

- `svg/` the mark (platform, one-colour ink, one-colour white, on ink, 16 px
  pixel-snapped), app and maskable icons, the wordmark as outlines, lockups
  (platform, on ink, mono, four products), product marks and icons, the
  social image.
- `png/` favicon 16/32/48 and `favicon.ico`, Apple touch 180, icons 192/512,
  maskable 512, product icons 512, product favicons 16/32, OpenGraph
  1200×630, lockup at 1200 wide.
- `next-app/` the files Next's metadata conventions pick up by name, to copy
  into `apps/shell/app/` when #1835 lands: `icon.svg`, `favicon.ico`,
  `apple-icon.png`, `opengraph-image.png`, `manifest.webmanifest`.

## Rules

- Mark alone where the context names the product (favicon, app icon, rail,
  avatar, stamps). Mark and wordmark together where it does not (sign-in,
  public nav, email, bills, social). One lockup: mark left, gap equal to the
  stem width, nothing else.
- Products share the shape; the pulse takes the product hue, which is the
  chart token for that area. Product lockups add the product word; product
  icons carry no letters.
- Tokens: ink `#0b1324`, accent `#2b4ee6` (`#7aa0ff` on ink), canvas
  `#f6f8fb`; pharmacy `#6b1a6e`, lab `#0e7490`, supply `#92400e`,
  emergency `#c13b5e`. These mirror `packages/ui/styles.css` after #1834.
- The wordmark face is Anek Latin (OFL, `apps/shell/app/fonts/OFL.txt`).
