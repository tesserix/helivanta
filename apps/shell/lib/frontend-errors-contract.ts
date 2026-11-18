import { z } from "./csp-zod";

export const CLIENT_ERROR_PATH = "/api/client-errors";
export const CLIENT_ERROR_EVENT = "frontend_error";
export const ERROR_KINDS = [
  "Error",
  "TypeError",
  "RangeError",
  "ReferenceError",
  "SyntaxError",
  "ChunkLoadError",
  "unknown",
] as const;
export const ERROR_AREAS = [
  "login",
  "signup",
  "dashboard",
  "workforce",
  "pharmacy",
  "medicore",
  "lab",
  "settings",
  "ambulance",
  "mediconnect",
  "other",
] as const;

export const frontendErrorReportContract = z
  .object({
    version: z.literal(1),
    reference: z
      .string()
      .regex(/^HV-[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/),
    source: z.enum(["root", "workspace", "global", "runtime", "unhandled_rejection"]),
    kind: z.enum(ERROR_KINDS),
    area: z.enum(ERROR_AREAS),
  })
  .strict();

export type FrontendErrorReport = z.infer<typeof frontendErrorReportContract>;
export type FrontendErrorSource = FrontendErrorReport["source"];
