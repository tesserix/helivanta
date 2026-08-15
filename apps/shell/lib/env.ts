import { z } from "zod";
import { defineEnv } from "@hms/api";

// NEXT_PUBLIC_ vars are inlined at build time, so they must be read
// statically (no dynamic process.env access).
//
// No demo/fallback values here, unlike the old GIP config (#838): a
// Firebase emulator accepted any api key/project id, so "demo-key" was a
// harmless placeholder. Zitadel does not — the client id is assigned at
// provisioning time by scripts/zitadel-bootstrap.mjs and a wrong or
// missing value fails real OIDC discovery/token exchange, not a silent
// no-op. Refusing to build without both is the honest failure.
export const env = defineEnv(
  {
    NEXT_PUBLIC_ZITADEL_ISSUER_URL: z.string().url(),
    NEXT_PUBLIC_ZITADEL_CLIENT_ID: z.string().min(1),
  },
  {
    NEXT_PUBLIC_ZITADEL_ISSUER_URL: process.env.NEXT_PUBLIC_ZITADEL_ISSUER_URL,
    NEXT_PUBLIC_ZITADEL_CLIENT_ID: process.env.NEXT_PUBLIC_ZITADEL_CLIENT_ID,
  },
);
