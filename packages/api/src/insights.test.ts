import { describe, expect, it } from "vitest";
import {
  dashboardFiltersSchema,
  dashboardPeriod,
  insightsSchema,
  insightsSearch,
} from "./insights";

describe("dashboard periods", () => {
  it("uses the selected timezone's date and Monday-start weeks", () => {
    const now = new Date("2026-10-18T23:30:00Z");
    expect(dashboardPeriod(now, "week", "Asia/Kolkata")).toEqual({
      from: "2026-10-19",
      to: "2026-10-19",
      timezone: "Asia/Kolkata",
      interval: "day",
    });
    expect(dashboardPeriod(now, "week", "UTC").from).toBe("2026-10-12");
    expect(dashboardPeriod(now, "today", "UTC").from).toBe("2026-10-18");
    expect(dashboardPeriod(now, "month", "UTC").from).toBe("2026-10-01");
    expect(dashboardPeriod(now, "year", "UTC").interval).toBe("month");
    expect(dashboardPeriod(now, "fiveYears", "UTC")).toMatchObject({
      from: "2022-01-01",
      interval: "year",
    });
  });
});

describe("dashboard filter contract", () => {
  const valid = { from: "2024-01-01", to: "2024-12-31", interval: "day", timezone: "UTC" };
  it("permits leap years and safely encodes IANA zones", () => {
    expect(dashboardFiltersSchema.parse(valid)).toEqual(valid);
    expect(
      insightsSearch({ ...dashboardFiltersSchema.parse(valid), timezone: "Asia/Kolkata" }),
    ).toContain("timezone=Asia%2FKolkata");
  });
  it.each([
    { from: "2026-02-30" },
    { from: "bad" },
    { to: "2023-12-31" },
    { from: "1999-01-01" },
    { to: "2200-01-01" },
    { to: "2025-01-01" },
    { interval: "hour" },
    { timezone: "" },
    { timezone: "Local" },
    { timezone: "Mars/Olympus" },
    { to: "2029-01-01", interval: "year" },
  ])("rejects invalid or unbounded input: %j", (overrides) => {
    expect(dashboardFiltersSchema.safeParse({ ...valid, ...overrides }).success).toBe(false);
  });
  it("allows five calendar years when grouped annually", () => {
    expect(
      dashboardFiltersSchema.safeParse({ ...valid, to: "2028-12-31", interval: "year" }).success,
    ).toBe(true);
  });
  it("rejects malformed aggregate data rather than rendering a false zero", () => {
    expect(insightsSchema.safeParse({ totals: {}, series: [] }).success).toBe(false);
  });
});
