import { afterEach, expect, it, vi } from "vitest";
import { dateTimeInputISO, dateTimeInputValue } from "./time-input";
afterEach(() => vi.unstubAllEnvs());
it("round trips local times across the date boundary using Melbourne daylight saving", () => {
  vi.stubEnv("TZ", "Australia/Melbourne");
  expect(dateTimeInputValue("2026-10-16T20:30:00Z")).toBe("2026-10-17T07:30");
  expect(dateTimeInputISO("2026-10-17T07:30")).toBe("2026-10-16T20:30:00.000Z");
});
it("keeps missing and invalid date input empty so form validation can explain it", () => {
  expect(dateTimeInputISO("")).toBe("");
  expect(dateTimeInputISO("not-a-date")).toBe("");
  expect(dateTimeInputValue("")).toBe("");
  expect(dateTimeInputValue("not-a-date")).toBe("");
});
