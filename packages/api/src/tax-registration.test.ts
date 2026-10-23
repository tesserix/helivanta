import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { taxScheme } from "./country-profiles";
import { normalizeTaxRegistration, validateTaxRegistration } from "./tax-registration";

// The same vectors as backend/pkg/tax, so the form and the server agree.
const vectors = JSON.parse(
  readFileSync(resolve(process.cwd(), "../../backend/pkg/tax/testdata/gstin.json"), "utf8"),
) as {
  known_answer: string;
  cases: { name: string; jurisdiction: string; identifier: string; error: string | null }[];
};

describe("validateTaxRegistration", () => {
  const india = taxScheme("IN")!;

  it("accepts the published known answer", () => {
    expect(validateTaxRegistration(india, "27", vectors.known_answer)).toBeNull();
  });

  for (const c of vectors.cases)
    it(c.name, () => {
      expect(validateTaxRegistration(india, c.jurisdiction, c.identifier)).toBe(c.error);
    });

  it("normalises as typed", () => {
    expect(normalizeTaxRegistration(" 07abcde1234f1z2 ")).toBe("07ABCDE1234F1Z2");
  });

  it("has no scheme for a country that refuses tax setup", () => {
    expect(taxScheme("XX")).toBeNull();
  });
});
