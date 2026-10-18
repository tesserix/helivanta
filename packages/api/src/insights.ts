import { z } from "zod";

const date = z
  .string()
  .regex(/^\d{4}-\d{2}-\d{2}$/, "Use YYYY-MM-DD.")
  .refine((value) => {
    const parsed = new Date(`${value}T00:00:00Z`);
    return Number.isFinite(parsed.getTime()) && parsed.toISOString().slice(0, 10) === value;
  }, "Enter a valid calendar date.");

export const dashboardFiltersSchema = z
  .object({
    from: date,
    to: date,
    interval: z.enum(["day", "week", "month", "year"]),
    timezone: z
      .string()
      .min(1)
      .max(100)
      .refine((value) => {
        if (value === "Local") return false;
        try {
          new Intl.DateTimeFormat("en", { timeZone: value });
          return true;
        } catch {
          return false;
        }
      }, "Enter an IANA timezone, such as Asia/Kolkata."),
  })
  .superRefine((values, ctx) => {
    const from = new Date(`${values.from}T00:00:00Z`);
    const to = new Date(`${values.to}T00:00:00Z`);
    if (!Number.isFinite(from.getTime()) || !Number.isFinite(to.getTime())) return;
    if (from.getUTCFullYear() < 2000 || to.getUTCFullYear() > 2199 || to < from) {
      ctx.addIssue({
        code: "custom",
        path: ["to"],
        message: "Use an inclusive range between 2000 and 2199, ending on or after From.",
      });
      return;
    }
    const last = new Date(from);
    last.setUTCFullYear(last.getUTCFullYear() + 5);
    // PostgreSQL/Go calendar years clamp February 29 through AddDate normalization.
    last.setUTCDate(last.getUTCDate() - 1);
    const days = (to.getTime() - from.getTime()) / 86_400_000 + 1;
    if (to > last || (values.interval === "day" && days > 366)) {
      ctx.addIssue({
        code: "custom",
        path: ["to"],
        message: "Use at most five years; daily grouping supports up to 366 days.",
      });
    }
  });

export type DashboardFilters = z.infer<typeof dashboardFiltersSchema>;
const counts = z.record(z.number().int().nonnegative().max(Number.MAX_SAFE_INTEGER));
export const insightsSchema = z.object({
  from: date,
  to: date,
  interval: z.enum(["day", "week", "month", "year"]),
  timezone: z.string(),
  as_at: z.string().datetime({ offset: true }),
  totals: counts,
  series: z.array(z.object({ date, counts })).max(366),
});
export type Insights = z.infer<typeof insightsSchema>;

export const staffInsightsSchema = z.object({
  hospital_id: z.string().uuid(),
  as_at: z.string().datetime({ offset: true }),
  counts: z
    .array(
      z.object({
        category: z.enum([
          "doctor",
          "nurse",
          "pharmacist",
          "lab_technician",
          "receptionist",
          "administration",
          "other",
        ]),
        status: z.enum(["active", "inactive"]),
        count: z.number().int().nonnegative(),
      }),
    )
    .max(14),
});

export function dashboardPeriod(
  now: Date,
  period: "today" | "week" | "month" | "year" | "fiveYears",
  timezone: string,
): DashboardFilters {
  const parts = new Intl.DateTimeFormat("en", {
    timeZone: timezone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).formatToParts(now);
  const part = (type: string) => parts.find((p) => p.type === type)?.value ?? "";
  const end = `${part("year")}-${part("month")}-${part("day")}`;
  const from = new Date(`${end}T00:00:00Z`);
  if (period === "week") from.setUTCDate(from.getUTCDate() - ((from.getUTCDay() + 6) % 7));
  if (period === "month") from.setUTCDate(1);
  if (period === "year") from.setUTCMonth(0, 1);
  if (period === "fiveYears") {
    from.setUTCFullYear(from.getUTCFullYear() - 4);
    from.setUTCMonth(0, 1);
  }
  return {
    from: from.toISOString().slice(0, 10),
    to: end,
    timezone,
    interval: period === "fiveYears" ? "year" : period === "year" ? "month" : "day",
  };
}

export function insightsSearch(filters: DashboardFilters): string {
  return new URLSearchParams(filters).toString();
}
