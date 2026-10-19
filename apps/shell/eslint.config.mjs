import { hmsEslint } from "@helivanta/config/eslint";

export default [
  ...hmsEslint(import.meta.dirname),
  // The chrome is rendered once, by the workspace layout (#1091). A page that
  // rendered its own HmsShell would nest a second rail and a second idle
  // tracker, so importing it anywhere else in the app is an error.
  {
    files: ["**/*.{ts,tsx}"],
    ignores: ["app/(workspace)/layout.tsx"],
    rules: {
      "no-restricted-imports": [
        "error",
        {
          paths: [
            {
              name: "@helivanta/ui",
              importNames: ["HmsShell"],
              message:
                "The chrome is rendered once by app/(workspace)/layout.tsx; pages render content only (#1091).",
            },
          ],
        },
      ],
    },
  },
];
