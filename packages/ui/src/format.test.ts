import { describe, expect, it } from "vitest";
import { formatDateTime, formatTime } from "./format";

describe("formatters", () => {
  it("formats times and datetimes deterministically", () => {
    const iso = "2026-08-04T04:05:06Z";
    expect(formatTime(iso)).toMatch(/\d/);
    expect(formatDateTime(iso)).toMatch(/\d{4}|\d{2}/);
    expect(formatTime(iso)).toBe(formatTime(iso));
  });

  it("returns an empty string for invalid dates", () => {
    expect(formatTime("")).toBe("");
    expect(formatTime("garbage")).toBe("");
  });
});
