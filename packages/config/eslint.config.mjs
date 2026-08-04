import js from "@eslint/js";
import nextPlugin from "@next/eslint-plugin-next";
import jsxA11y from "eslint-plugin-jsx-a11y";
import reactHooks from "eslint-plugin-react-hooks";
import tseslint from "typescript-eslint";

// Shared HMS flat config. Apps call hmsEslint(import.meta.dirname).
export function hmsEslint(rootDir) {
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
        // HMS UX vocabulary: browser dialogs are banned (spec D4).
        "no-alert": "error",
        "no-console": ["error", { allow: ["warn", "error"] }],
        "@typescript-eslint/no-explicit-any": "error",
        // Plain <a> cross-zone links are repo policy (standards doc section 6).
        "@next/next/no-html-link-for-pages": "off",
        "no-restricted-syntax": [
          "error",
          {
            selector:
              "JSXAttribute[name.name='dangerouslySetInnerHTML']:not([value.expression.callee.name='sanitizeHtml'])",
            message: "Use sanitizeHtml from @hms/ui instead of raw dangerouslySetInnerHTML.",
          },
        ],
      },
      settings: { next: { rootDir } },
    },
  );
}
