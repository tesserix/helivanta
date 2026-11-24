"""Convert wordmark strings to SVG path outlines from the repo's Anek Latin file.

Usage: python outline.py <anek-latin.woff2> <out.json>

Shapes each string with HarfBuzz (kerning and ligatures from GPOS/GSUB), draws
every glyph with fontTools, and emits one path per string in font units with
the total advance. Letter-spacing is applied per glyph as a fraction of the em.
The JSON is consumed by generate.mjs, which scales paths into the SVGs.
"""

import io
import json
import sys

import uharfbuzz as hb
from fontTools.pens.svgPathPen import SVGPathPen
from fontTools.pens.transformPen import TransformPen
from fontTools.ttLib import TTFont
from fontTools.varLib.instancer import instantiateVariableFont

SRC, OUT = sys.argv[1], sys.argv[2]

# text, weight, letter-spacing (em). The wordmark is tight; the tagline is not.
STRINGS = {
    "wordmark": ("helivanta", 700, -0.03),
    "pharmacy": ("pharmacy", 600, -0.02),
    "lab": ("lab", 600, -0.02),
    "supply": ("supply", 600, -0.02),
    "emergency": ("emergency", 600, -0.02),
    "tagline": ("One connected hospital, from boardroom to bedside.", 500, 0.0),
    "domain": ("helivanta.app", 500, 0.0),
}


def instance(weight):
    font = TTFont(SRC)
    instantiateVariableFont(font, {"wght": weight, "wdth": 100}, inplace=True)
    buf = io.BytesIO()
    font.flavor = None
    font.save(buf)
    return font, buf.getvalue()


def outline(text, weight, tracking):
    font, blob = instance(weight)
    upm = font["head"].unitsPerEm
    face = hb.Face(blob)
    hbfont = hb.Font(face)
    hbfont.scale = (upm, upm)
    buf = hb.Buffer()
    buf.add_str(text)
    buf.guess_segment_properties()
    hb.shape(hbfont, buf, {"kern": True, "liga": True})
    glyph_order = font.getGlyphOrder()
    glyph_set = font.getGlyphSet()
    pen = SVGPathPen(glyph_set, ntos=lambda v: f"{v:.1f}".rstrip("0").rstrip("."))
    x = 0.0
    track = tracking * upm
    for info, pos in zip(buf.glyph_infos, buf.glyph_positions):
        name = glyph_order[info.codepoint]
        # y is flipped here so the path is already in SVG's y-down space.
        tp = TransformPen(pen, (1, 0, 0, -1, x + pos.x_offset, -pos.y_offset))
        glyph_set[name].draw(tp)
        x += pos.x_advance + track
    x -= track  # no trailing space after the last glyph
    hhea = font["hhea"]
    return {
        "d": pen.getCommands(),
        "advance": round(x, 1),
        "upm": upm,
        "ascender": hhea.ascent,
        "descender": hhea.descent,
        "capHeight": font["OS/2"].sCapHeight,
        "xHeight": font["OS/2"].sxHeight,
        "weight": weight,
    }


out = {key: outline(*spec) for key, spec in STRINGS.items()}
with open(OUT, "w") as fh:
    json.dump(out, fh)
print({k: (v["advance"], len(v["d"])) for k, v in out.items()})
