import { describe, expect, it } from "vitest";
import { emergencyAccess, emergencyCopy } from "./index";

describe("public emergency access", () => {
  it("offers the national India emergency dialler without login or connectivity", () => {
    const access = emergencyAccess("IN");
    expect(access.countryName).toBe("India");
    expect(access.services).toEqual([
      expect.objectContaining({ number: "112", dialUrl: "tel:112", kind: "national_emergency" }),
    ]);
    expect(access.bookingEnabled).toBe(false);
    expect(access.services[0]?.sourceUrl).toMatch(/^https:\/\/nhsrcindia\.org\//);
    expect(access.reviewedOn).toBe("2026-10-18");
  });

  it.each(["OTHER", "", "AU", "in", "IN-OD", "IN<script>"])(
    "does not present Indian numbers as local in unsupported country %s",
    (country) => {
      const access = emergencyAccess(country);
      expect(access.services).toEqual([]);
      expect(access.regionalInformation).toEqual([]);
      expect(access.countryCode).toBe("OTHER");
      expect(access.bookingEnabled).toBe(false);
    },
  );

  it("explains regional 108 and 102 without asserting nationwide availability", () => {
    const access = emergencyAccess("IN");
    expect(access.regionalInformation).toEqual([
      expect.objectContaining({
        number: "108",
        scope: "Emergency response in participating states",
      }),
      expect.objectContaining({
        number: "102",
        scope: "Patient transport, particularly for pregnant women and children",
      }),
    ]);
    expect(access.regionalInformation.every((service) => !("dialUrl" in service))).toBe(true);
    expect(emergencyCopy.regionalNotice).toContain("varies by state");
  });

  it("never represents opening a dialler as booking or confirmed dispatch", () => {
    expect(emergencyCopy.callNotice).toContain("does not book an ambulance");
    expect(emergencyCopy.callFailure).toContain("dial manually");
    expect(emergencyCopy.privacyNotice).toContain("does not request your location");
  });
});
