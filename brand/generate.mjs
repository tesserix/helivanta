// Generates every Helivanta brand asset from one mark definition and the
// outlined wordmark (outlines.json, produced by outline.py from the repo's
// Anek Latin file). Run: node generate.mjs <sharp-module-path> <outDir>
//
// Everything is derived, nothing is hand-exported, so a change to the mark or
// a hue is one edit followed by one run.

import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { createRequire } from "node:module";

const [sharpPath, outDir] = process.argv.slice(2);
const sharp = createRequire(import.meta.url)(sharpPath);
const outlines = JSON.parse(readFileSync(new URL("./outlines.json", import.meta.url)));

// Tokens from the slice-1 spec (docs/superpowers/specs/2026-11-24-app-look-tokens-design.md).
const INK = "#0b1324";
const ACCENT = "#2b4ee6";
const ACCENT_ON_INK = "#7aa0ff";
const CANVAS = "#f6f8fb";
const MUTED = "#4e5a6e";
const WHITE = "#ffffff";
const PRODUCTS = {
  pharmacy: "#86198f", // --hms-violet
  lab: "#0e7490", // --hms-sky
  supply: "#92400e", // --hms-amber
  emergency: "#c13b5e", // --hms-rose
};

// The mark on a 64-unit grid: stem, half-height leg, 4-unit pulse at the shoulder.
const markBody = (c1, c2) =>
  `<rect x="8" y="6" width="12" height="52" fill="${c1}"/>` +
  `<rect x="40" y="34" width="12" height="24" fill="${c1}"/>` +
  `<path d="M20 32H27L31 19L36 46L39 32H52" fill="none" stroke="${c2}" stroke-width="4"/>`;

const svg = (w, h, body, title = "Helivanta") =>
  `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${w} ${h}" width="${w}" height="${h}">` +
  `<title>${title}</title>${body}</svg>`;

const markSvg = (c1, c2) => svg(64, 64, markBody(c1, c2));

// 16 px favicon: the same shape redrawn on a 16-unit grid with every edge on a
// whole pixel and the pulse on half-pixel centres, so bars are solid and the
// line is one crisp pixel instead of a blur. Checked against the scaled 64-grid
// render, whose stem ends landed on half pixels.
const mark16Svg = (c1, c2) =>
  svg(
    16,
    16,
    `<rect x="2" y="1" width="3" height="14" fill="${c1}"/>` +
      `<rect x="10" y="8" width="3" height="7" fill="${c1}"/>` +
      `<path d="M5 7.5H6.5L7.5 4.5L8.5 10.5L9.5 7.5H13" fill="none" stroke="${c2}" stroke-width="1"/>`,
  );

// App icon: ink tile, white mark, pulse in the on-ink accent. Rounded for the
// "any" icon; the maskable one is full bleed with the mark inside the 80% safe zone.
const tile = (c2, { rx = 16, scale = 0.75 } = {}) =>
  svg(
    64,
    64,
    `<rect width="64" height="64" rx="${rx}" fill="${INK}"/>` +
      `<g transform="translate(${32 - 32 * scale} ${32 - 32 * scale}) scale(${scale})">${markBody(WHITE, c2)}</g>`,
  );

// Text as outlines. Scales a font-unit path so the cap height equals `cap` px
// and places the baseline at `baseline`.
function text(key, cap, x, baseline, fill) {
  const o = outlines[key];
  const s = cap / o.capHeight;
  return {
    el: `<path transform="translate(${x} ${baseline}) scale(${s})" d="${o.d}" fill="${fill}"/>`,
    width: o.advance * s,
  };
}

// Lockup: mark left, its top at the wordmark's ascender line, gap = stem width.
function lockup(c1, c2, wordFill, extraKey, extraFill) {
  const H = 64; // mark height in px
  const cap = 44; // wordmark cap height; the h ascender then matches the mark's stem
  const stem = 12;
  const baseline = 6 + 52; // mark's stem bottom
  const word = text("wordmark", cap, 64 + stem, baseline, wordFill);
  let body = markBody(c1, c2) + word.el;
  let width = 64 + stem + word.width;
  if (extraKey) {
    const extra = text(extraKey, cap, width + 16, baseline, extraFill);
    body += extra.el;
    width += 16 + extra.width;
  }
  return { body, width: Math.ceil(width), height: H };
}

const lockupSvg = (c1, c2, wordFill, extraKey, extraFill, bg) => {
  const l = lockup(c1, c2, wordFill, extraKey, extraFill);
  const pad = 16;
  const w = l.width + pad * 2;
  const h = l.height + pad * 2;
  const back = bg ? `<rect width="${w}" height="${h}" fill="${bg}"/>` : "";
  return svg(w, h, `${back}<g transform="translate(${pad} ${pad})">${l.body}</g>`);
};

// Social image, 1200x630: canvas, lockup, tagline, domain. Left aligned on an 80px margin.
function openGraph() {
  const l = lockup(INK, ACCENT, INK);
  const k = 2.1; // lockup scale
  const tag = text("tagline", 28, 80, 440, MUTED);
  const dom = text("domain", 18, 80, 560, MUTED);
  return svg(
    1200,
    630,
    `<rect width="1200" height="630" fill="${CANVAS}"/>` +
      `<g transform="translate(80 170) scale(${k})">${l.body}</g>` +
      tag.el +
      dom.el +
      `<rect x="80" y="596" width="48" height="4" fill="${ACCENT}"/>`,
    "Helivanta: one connected hospital, from boardroom to bedside",
  );
}

// PNG-in-ICO: 6-byte header, one 16-byte directory entry per image, then the PNGs.
function ico(pngs) {
  const header = Buffer.alloc(6);
  header.writeUInt16LE(0, 0);
  header.writeUInt16LE(1, 2);
  header.writeUInt16LE(pngs.length, 4);
  let offset = 6 + 16 * pngs.length;
  const entries = [];
  for (const { size, buf } of pngs) {
    const e = Buffer.alloc(16);
    e.writeUInt8(size === 256 ? 0 : size, 0);
    e.writeUInt8(size === 256 ? 0 : size, 1);
    e.writeUInt8(0, 2);
    e.writeUInt8(0, 3);
    e.writeUInt16LE(1, 4);
    e.writeUInt16LE(32, 6);
    e.writeUInt32LE(buf.length, 8);
    e.writeUInt32LE(offset, 12);
    offset += buf.length;
    entries.push(e);
  }
  return Buffer.concat([header, ...entries, ...pngs.map((p) => p.buf)]);
}

const png = (svgText, size, opts = {}) =>
  sharp(Buffer.from(svgText), { density: 384 })
    .resize(size.w ?? size, size.h ?? size, { fit: "contain", background: opts.bg ?? { r: 0, g: 0, b: 0, alpha: 0 } })
    .png({ compressionLevel: 9 })
    .toBuffer();

const dirs = { svg: join(outDir, "svg"), png: join(outDir, "png"), app: join(outDir, "next-app") };
for (const d of Object.values(dirs)) mkdirSync(d, { recursive: true });
const write = (dir, name, data) => writeFileSync(join(dir, name), data);

// 1. SVG sources
const sources = {
  "mark.svg": markSvg(INK, ACCENT),
  "mark-16.svg": mark16Svg(INK, ACCENT),
  "mark-mono-ink.svg": markSvg(INK, INK),
  "mark-mono-white.svg": markSvg(WHITE, WHITE),
  "mark-on-ink.svg": svg(64, 64, `<rect width="64" height="64" fill="${INK}"/>${markBody(WHITE, ACCENT_ON_INK)}`),
  "app-icon.svg": tile(ACCENT_ON_INK),
  "maskable-icon.svg": tile(ACCENT_ON_INK, { rx: 0, scale: 0.6 }),
  "wordmark.svg": (() => {
    const t = text("wordmark", 44, 0, 52, INK);
    return svg(Math.ceil(t.width), 64, t.el);
  })(),
  "lockup.svg": lockupSvg(INK, ACCENT, INK),
  "lockup-on-ink.svg": lockupSvg(WHITE, ACCENT_ON_INK, WHITE, null, null, INK),
  "lockup-mono.svg": lockupSvg(INK, INK, INK),
  "opengraph.svg": openGraph(),
};
for (const [name, hue] of Object.entries(PRODUCTS)) {
  sources[`mark-${name}.svg`] = markSvg(INK, hue);
  sources[`app-icon-${name}.svg`] = tile(hue);
  sources[`lockup-${name}.svg`] = lockupSvg(INK, hue, INK, name, hue);
}
for (const [name, body] of Object.entries(sources)) write(dirs.svg, name, body);

// 2. Rasters
const favSizes = [16, 32, 48];
const favPngs = [];
for (const s of favSizes) {
  const buf = await png(s === 16 ? sources["mark-16.svg"] : sources["mark.svg"], s);
  favPngs.push({ size: s, buf });
  write(dirs.png, `favicon-${s}.png`, buf);
}
write(dirs.png, "favicon.ico", ico(favPngs));
write(dirs.png, "apple-touch-icon-180.png", await png(sources["maskable-icon.svg"], 180));
write(dirs.png, "icon-192.png", await png(sources["app-icon.svg"], 192));
write(dirs.png, "icon-512.png", await png(sources["app-icon.svg"], 512));
write(dirs.png, "maskable-icon-512.png", await png(sources["maskable-icon.svg"], 512));
write(dirs.png, "opengraph-image.png", await png(sources["opengraph.svg"], { w: 1200, h: 630 }));
write(dirs.png, "lockup@2x.png", await png(sources["lockup.svg"], { w: 1200, h: Math.round((1200 * 96) / (lockup(INK, ACCENT, INK).width + 32)) }));
for (const name of Object.keys(PRODUCTS)) {
  write(dirs.png, `icon-${name}-512.png`, await png(sources[`app-icon-${name}.svg`], 512));
  write(dirs.png, `favicon-${name}-32.png`, await png(sources[`mark-${name}.svg`], 32));
  write(dirs.png, `favicon-${name}-16.png`, await png(mark16Svg(INK, PRODUCTS[name]), 16));
}

// 3. Files named for Next's metadata conventions (apps/shell/app/), ready to copy.
write(dirs.app, "icon.svg", sources["mark.svg"]);
write(dirs.app, "favicon.ico", ico(favPngs));
write(dirs.app, "apple-icon.png", await png(sources["maskable-icon.svg"], 180));
write(dirs.app, "opengraph-image.png", await png(sources["opengraph.svg"], { w: 1200, h: 630 }));
write(
  dirs.app,
  "manifest.webmanifest",
  JSON.stringify(
    {
      name: "Helivanta",
      short_name: "Helivanta",
      start_url: "/",
      // Icons only. Standalone mode is untested against the OIDC redirect
      // flow on iOS, so the manifest does not make the app installable yet.
      display: "browser",
      background_color: CANVAS,
      theme_color: INK,
      icons: [
        { src: "/icon-192.png", sizes: "192x192", type: "image/png" },
        { src: "/icon-512.png", sizes: "512x512", type: "image/png" },
        { src: "/maskable-icon-512.png", sizes: "512x512", type: "image/png", purpose: "maskable" },
      ],
    },
    null,
    2,
  ),
);

console.log("wrote", Object.keys(sources).length, "svg sources and rasters to", outDir);
