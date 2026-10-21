import js from "@eslint/js";
import nextPlugin from "@next/eslint-plugin-next";
import jsxA11y from "eslint-plugin-jsx-a11y";
import reactHooks from "eslint-plugin-react-hooks";
import tseslint from "typescript-eslint";

// Raw Tailwind palette colours and hex arbitrary values bypass the design
// tokens (frontend.md §7, #1255 spec D2), so the one palette could drift again
// one class at a time. Variant prefixes (md:, hover:) and arbitrary values
// (bg-[rgb(…)], [color:#…]) count, and so does a bare hex attribute (SVG fill).
const RAW_COLOUR = String.raw`/(^|[\s:])(bg|text|border|ring|ring-offset|fill|stroke|outline|from|to|via|divide|decoration|shadow|placeholder|caret|accent)-((slate|gray|zinc|neutral|stone|red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose)-\d{2,3}|white|black)\b|-\[(#|rgba?\(|hsla?\(|oklch\(|oklab\(|lab\(|lch\()|\[[\w-]+:#[0-9a-fA-F]{3,8}\]|^#[0-9a-fA-F]{3,8}$/`;
const COLOUR_MESSAGE =
  "Use a design-token utility (frontend.md §7), not a raw Tailwind colour or hex.";

const DESIGN_SYNTAX = [
  { selector: `Literal[value=${RAW_COLOUR}]`, message: COLOUR_MESSAGE },
  { selector: `TemplateElement[value.raw=${RAW_COLOUR}]`, message: COLOUR_MESSAGE },
  // The native popup is drawn by the OS in OS colours, outside the tokens
  // (#1255 spec D6).
  {
    selector: "JSXOpeningElement[name.name='select']",
    message:
      "Use SelectControl or FormSelectField from @helivanta/ui (@tesserix/web Select), not a native <select>.",
  },
];

// The landing page's marketing type reads as a template inside a working tool
// (#1255 spec D3). Public pages opt out per file in apps/shell's config.
const MARKETING_TYPE = String.raw`/(^|[\s:])(font-serif|font-\[[^\]]*serif[^\]]*\]|italic|bg-linear-\S+|bg-gradient-\S+|tracking-(wider|widest|\[(0?\.0[5-9]|0?\.[1-9]|[1-9])[^\]]*\]))(\s|$)|(^|[\s:])uppercase\s(.*\s)?tracking-|(^|[\s:])tracking-\S+\s(.*\s)?uppercase(\s|$)/`;
const MARKETING_MESSAGE =
  "Marketing type (serif, italic, gradient, wide-tracked or uppercase eyebrow) belongs to the public pages, not the app (#1255 spec D3).";
const MARKETING_SYNTAX = [
  { selector: `Literal[value=${MARKETING_TYPE}]`, message: MARKETING_MESSAGE },
  { selector: `TemplateElement[value.raw=${MARKETING_TYPE}]`, message: MARKETING_MESSAGE },
];

const SANITIZE_SYNTAX = {
  selector:
    "JSXAttribute[name.name='dangerouslySetInnerHTML']:not([value.expression.callee.name='sanitizeHtml'])",
  message: "Use sanitizeHtml from @helivanta/ui instead of raw dangerouslySetInnerHTML.",
};

/**
 * The public marketing pages keep their headline type but every other design
 * rule. Spread after hmsEslint(dir, { design: true }) with the page globs.
 */
export function hmsPublicPages(files) {
  return {
    files,
    rules: { "no-restricted-syntax": ["error", SANITIZE_SYNTAX, ...DESIGN_SYNTAX] },
  };
}

// Shared Helivanta flat config. Apps call hmsEslint(import.meta.dirname);
// web packages pass { design: true } for the design-token rules.
export function hmsEslint(rootDir, { design = false } = {}) {
  return tseslint.config(
    { ignores: [".next/**", "node_modules/**", "dist/**", "coverage/**"] },
    js.configs.recommended,
    ...tseslint.configs.recommended,
    jsxA11y.flatConfigs.recommended,
    {
      plugins: { "@next/next": nextPlugin, "react-hooks": reactHooks },
      rules: {
        ...nextPlugin.configs.recommended.rules,
        ...reactHooks.configs.recommended.rules,
        // Helivanta UX vocabulary: browser dialogs are banned (spec D4).
        "no-alert": "error",
        "no-console": ["error", { allow: ["warn", "error"] }],
        "@typescript-eslint/no-explicit-any": "error",
        // Off because it only understands the pages/ router, which this repo
        // does not use (App Router only): it cannot see an <a> that should be
        // next/link here. That guarantee is enforced by
        // e2e/tests/navigation.spec.ts instead (#933).
        "@next/next/no-html-link-for-pages": "off",
        "no-restricted-syntax": [
          "error",
          SANITIZE_SYNTAX,
          // One array: no-restricted-syntax options do not merge across
          // config objects, so a later object would drop the entry above.
          ...(design ? [...DESIGN_SYNTAX, ...MARKETING_SYNTAX] : []),
        ],
      },
      settings: { next: { rootDir } },
    },
    // Tests assert on colour values themselves (the contrast test) and render
    // fixtures, so the design rules cover shipped code only. Sanitize stays.
    ...(design
      ? [
          {
            files: ["**/*.test.{ts,tsx,mjs}"],
            rules: { "no-restricted-syntax": ["error", SANITIZE_SYNTAX] },
          },
        ]
      : []),
  );
}
