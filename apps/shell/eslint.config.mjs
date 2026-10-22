import { hmsEslint, hmsPublicPages, hmsWorkspacePages } from "@helivanta/config/eslint";

export default [
  ...hmsEslint(import.meta.dirname, { design: true }),
  // The public marketing pages keep their headline type (#1255 spec D3).
  hmsPublicPages(["app/page.tsx", "app/signup/**", "app/ambulance/**", "app/login/**"]),
  // Workspace pages open with PageHeading under the shell's h1 (#1274). The
  // login branding heads a page outside the chrome, so it keeps its h1.
  hmsWorkspacePages(["app/(workspace)/**", "components/**"], ["components/login-branding.tsx"]),
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
