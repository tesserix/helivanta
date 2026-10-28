import { hmsEslint, hmsPublicPages, hmsWorkspacePages } from "@helivanta/config/eslint";
import { SURFACE_LEGACY } from "./surface-legacy.mjs";

export default [
  ...hmsEslint(import.meta.dirname, { design: true }),
  // The public marketing pages and their story boards keep their headline type (#1255 spec D3).
  hmsPublicPages([
    "app/page.tsx",
    "app/workspace/page.tsx",
    "app/signup/**",
    "app/ambulance/**",
    "app/login/**",
    "components/landing/**",
  ]),
  // Workspace pages open with PageHeading under the shell's h1 (#1274) and
  // paint cards only through Panel (#1275). The login branding and the home
  // page boards sit outside the chrome, so they keep their own headings and surfaces.
  ...hmsWorkspacePages(
    ["app/(workspace)/**", "components/**"],
    ["components/login-branding.tsx", "components/landing/**"],
    SURFACE_LEGACY,
  ),
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
