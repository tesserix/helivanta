import globals from "globals";

import { hmsEslint } from "@helivanta/config/eslint";

// scripts/ holds Node CLI tooling (dev-stack bootstrap, seeding, the e2e
// runner's own helpers) — not app code. It reuses the shared flat config
// for the baseline (no-explicit-any, dangerouslySetInnerHTML guard, etc.)
// but layers in what plain Node ESM scripts need that TypeScript app code
// gets for free:
//   - Node globals (process, console, fetch, URL, Buffer, ...): apps/*
//     and packages/* are TypeScript, where typescript-eslint's recommended
//     config disables no-undef because tsc already catches an undefined
//     name. scripts/ is plain .mjs with no tsconfig (see package.json),
//     so no-undef is live and needs to know what a Node runtime provides.
//   - no-console: these scripts' entire job is to report progress and
//     results to a human on stdout; that is not a leaked debug statement,
//     it is the product.
//
// There is deliberately no @next/next/no-html-link-for-pages override
// here. That rule is already off for the whole repo in
// packages/config/eslint.config.mjs — plain <a> cross-zone links are repo
// policy — so a local copy would be a no-op that reads as if scripts/
// were special. It is not.
export default [
  ...hmsEslint(import.meta.dirname),
  {
    languageOptions: {
      globals: globals.node,
    },
    rules: {
      "no-console": "off",
    },
  },
];
