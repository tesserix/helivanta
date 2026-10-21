import js from "@eslint/js";
import nextPlugin from "@next/eslint-plugin-next";
import jsxA11y from "eslint-plugin-jsx-a11y";
import reactHooks from "eslint-plugin-react-hooks";
import tseslint from "typescript-eslint";

// Raw Tailwind palette colours and hex arbitrary values bypass the design
// tokens (frontend.md §7, #1255 spec D2), so the one palette could drift again
// one class at a time.
const RAW_COLOUR = String.raw`/\b(bg|text|border|ring|fill|stroke|outline|from|to|via|divide|decoration|shadow|placeholder|caret|accent)-(slate|gray|zinc|neutral|stone|red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose)-\d{2,3}\b|\b(bg|text|border|ring|fill|stroke)-(white|black)\b|-\[#[0-9a-fA-F]{3,8}\]/`;
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
          {
            selector:
              "JSXAttribute[name.name='dangerouslySetInnerHTML']:not([value.expression.callee.name='sanitizeHtml'])",
            message: "Use sanitizeHtml from @helivanta/ui instead of raw dangerouslySetInnerHTML.",
          },
          // One array: no-restricted-syntax options do not merge across
          // config objects, so a later object would drop the entry above.
          ...(design ? DESIGN_SYNTAX : []),
        ],
      },
      settings: { next: { rootDir } },
    },
  );
}
