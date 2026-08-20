import globals from "globals";

import { hmsEslint } from "@helivanta/config/eslint";

// scripts/ holds Node CLI tooling (dev-stack bootstrap, seeding, the e2e
// runner's own helpers) — not app code. It reuses the shared flat config
// for the baseline (no-explicit-any, dangerouslySetInnerHTML guard, etc.)
// but layers in what plain Node ESM scripts need that TypeScript app code
// gets for free, and turns off two rules that are specific to
// browser-facing UI code and actively wrong here:
//   - Node globals (process, console, fetch, URL, Buffer, ...): apps/*
//     and packages/* are TypeScript, where typescript-eslint's recommended
//     config disables no-undef because tsc already catches an undefined
//     name. scripts/ is plain .mjs with no tsconfig (see package.json),
//     so no-undef is live and needs to know what a Node runtime provides.
//   - no-console: these scripts' entire job is to report progress and
//     results to a human on stdout; that is not a leaked debug statement,
//     it is the product.
//   - @next/next/*: there is no Next.js app under scripts/, so the plugin
//     has nothing to check and its rootDir assumption does not hold.
export default [
  ...hmsEslint(import.meta.dirname),
  {
    languageOptions: {
      globals: globals.node,
    },
    rules: {
      "no-console": "off",
      "@next/next/no-html-link-for-pages": "off",
    },
  },
];
