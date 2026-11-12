import { z } from "zod";

// GET /v1/iam/me/profile (#1428): the signed-in person and their roles in the session tenant.
export const accountProfileSchema = z.object({
  subject: z.string(),
  tenant_id: z.string(),
  tenant_name: z
    .string()
    .min(1)
    .refine(
      (value) =>
        Array.from(value).length <= 200 && value === value.trim() && !/\p{Cc}/u.test(value),
    )
    .optional(),
  display_name: z.string(),
  email: z.string(),
  roles: z.array(z.object({ key: z.string(), label: z.string() })),
});
export type AccountProfile = z.infer<typeof accountProfileSchema>;
