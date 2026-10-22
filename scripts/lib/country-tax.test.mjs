import assert from "node:assert/strict";
import { test } from "node:test";

import { validateTax } from "./country-tax.mjs";

const india = () => ({
  code: "IN",
  tax_identifier_kinds: ["gstin", "pan"],
  tax: {
    scheme: "gst",
    registration_kind: "gstin",
    classification: { name: "HSN", pattern: "^[0-9]{4}$" },
    jurisdictions: [{ code: "07", name: "Delhi", kind: "state" }],
    sources: [{ topic: "codes", url: "https://example.gov.in/x", reviewed_at: "2026-10-23" }],
  },
});

test("a null scheme and a well-formed scheme pass", () => {
  validateTax({ ...india(), tax: null });
  validateTax(india());
});

for (const [name, mutate] of [
  ["registration kind outside the country's identifiers", (p) => (p.tax.registration_kind = "ntn")],
  ["duplicate jurisdiction code", (p) => p.tax.jurisdictions.push({ ...p.tax.jurisdictions[0] })],
  ["unknown jurisdiction kind", (p) => (p.tax.jurisdictions[0].kind = "province")],
  ["non-https source", (p) => (p.tax.sources[0].url = "http://example.gov.in/x")],
  ["uncompilable pattern", (p) => (p.tax.classification.pattern = "^([0-9]$")],
  ["unanchored pattern", (p) => (p.tax.classification.pattern = "[0-9]{4}")],
  ["unsupported scheme", (p) => (p.tax.scheme = "vat")],
  ["missing tax key", (p) => delete p.tax],
]) {
  test(`refuses ${name}`, () => {
    const p = india();
    mutate(p);
    assert.throws(() => validateTax(p), /Invalid tax scheme for IN/);
  });
}
