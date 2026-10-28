import { describe, expect, it } from "vitest";
import {
  formatZonedDateTime,
  isCustomerTimeZone,
  zonedDateTimeInput,
  zonedDateTimeISO,
} from "./customer-time";

describe("configured customer time", () => {
  it("uses the saved zone for display and round trips independent of the browser", () => {
    const instant = "2026-10-17T04:30:00Z";
    expect(zonedDateTimeInput(instant, "Asia/Kolkata")).toBe("2026-10-17T10:00");
    expect(zonedDateTimeISO("2026-10-17T10:00", "Asia/Kolkata")).toBe("2026-10-17T04:30:00.000Z");
    expect(zonedDateTimeInput(instant, "Asia/Tashkent")).toBe("2026-10-17T09:30");
    expect(zonedDateTimeISO("2026-10-17T09:30", "Asia/Tashkent")).toBe("2026-10-17T04:30:00.000Z");
  });
  it("preserves exact occurrence microseconds and shows the actual UTC offset", () => {
    expect(formatZonedDateTime("2026-10-17T04:30:00.123456Z", "Asia/Kolkata")).toBe(
      "2026-10-17 10:00:00.123456 GMT+05:30",
    );
    expect(formatZonedDateTime("2026-10-17T09:30:00.123456+05:00", "Asia/Tashkent")).toBe(
      "2026-10-17 09:30:00.123456 GMT+05:00",
    );
  });
  it("rejects nonexistent and ambiguous daylight saving wall times", () => {
    expect(zonedDateTimeISO("2026-03-08T02:30", "America/New_York")).toBeNull();
    expect(zonedDateTimeISO("2026-11-01T01:30", "America/New_York")).toBeNull();
    expect(zonedDateTimeISO("2026-04-05T01:45", "Australia/Lord_Howe")).toBeNull();
    expect(zonedDateTimeISO("2011-12-30T12:00", "Pacific/Apia")).toBeNull();
    expect(zonedDateTimeISO("2026-03-08T03:30", "America/New_York")).toBe(
      "2026-03-08T07:30:00.000Z",
    );
  });
  it("refuses bad configuration, rolled-over dates and invalid input", () => {
    expect(isCustomerTimeZone("Wrong/Zone")).toBe(false);
    expect(isCustomerTimeZone("")).toBe(false);
    expect(isCustomerTimeZone("Asia/Tashkent")).toBe(true);
    for (const input of ["2026-02-30T10:00", "2026-01-01T24:30", "0000-01-01T10:00", "bad", ""])
      expect(zonedDateTimeISO(input, "UTC")).toBeNull();
    expect(zonedDateTimeISO("2026-01-01T10:00", "Wrong/Zone")).toBeNull();
    expect(zonedDateTimeInput("bad", "UTC")).toBe("");
    expect(formatZonedDateTime("bad", "Asia/Kolkata")).toBe("");
    expect(formatZonedDateTime("2026-01-01T10:00Z", "Wrong/Zone")).toBe("");
    expect(zonedDateTimeISO("0001-01-01T00:00", "UTC")).toBe("0001-01-01T00:00:00.000Z");
    expect(zonedDateTimeISO("0001-01-01T00:00", "Asia/Kolkata")).toBeNull();
  });
});
