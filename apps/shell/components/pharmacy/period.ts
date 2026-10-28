import { z } from "zod";

/** A report period in outlet dates, at most 366 days, as the server bounds it. */
export const periodSchema = z
  .object({
    from: z.string().regex(/^\d{4}-\d{2}-\d{2}$/, "Enter the first day"),
    to: z.string().regex(/^\d{4}-\d{2}-\d{2}$/, "Enter the last day"),
  })
  .superRefine((v, ctx) => {
    const days = (Date.parse(v.to) - Date.parse(v.from)) / 86_400_000;
    if (days < 0)
      ctx.addIssue({
        code: "custom",
        path: ["to"],
        message: "The last day is on or after the first",
      });
    else if (days > 365)
      ctx.addIssue({ code: "custom", path: ["to"], message: "A period is at most 366 days" });
  });
export type Period = z.infer<typeof periodSchema>;
