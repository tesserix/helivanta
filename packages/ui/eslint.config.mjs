import { hmsEslint, hmsPublicPages } from "@helivanta/config/eslint";

export default [
  ...hmsEslint(import.meta.dirname, { design: true }),
  // LoginSurface is the public sign-in page, which shares the landing page's
  // headline type (#1255 spec D3).
  hmsPublicPages(["src/branding.tsx"]),
];
