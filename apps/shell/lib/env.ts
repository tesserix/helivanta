import { z } from "zod";
import { defineEnv } from "@hms/api";

// NEXT_PUBLIC_ vars are inlined at build time, so they must be read
// statically (no dynamic process.env access).
export const env = defineEnv(
  {
    NEXT_PUBLIC_GIP_API_KEY: z.string().min(1),
    NEXT_PUBLIC_GIP_PROJECT_ID: z.string().min(1),
  },
  {
    NEXT_PUBLIC_GIP_API_KEY:
      process.env.NEXT_PUBLIC_GIP_API_KEY ??
      (process.env.NODE_ENV !== "production" ? "demo-key" : undefined),
    NEXT_PUBLIC_GIP_PROJECT_ID:
      process.env.NEXT_PUBLIC_GIP_PROJECT_ID ??
      (process.env.NODE_ENV !== "production" ? "demo-hms" : undefined),
  },
);
