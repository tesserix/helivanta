import { z } from "zod";

// Fail-fast env access: call at module load with the exact process.env
// keys the app needs. Throws one readable error naming every bad key.
export function defineEnv<S extends z.ZodRawShape>(
  shape: S,
  values: Record<string, string | undefined>,
): z.infer<z.ZodObject<S>> {
  const parsed = z.object(shape).safeParse(values);
  if (!parsed.success) {
    const lines = parsed.error.issues.map((i) => `  ${i.path.join(".")}: ${i.message}`);
    throw new Error(`Invalid environment:\n${lines.join("\n")}`);
  }
  return parsed.data;
}
