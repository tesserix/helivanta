import { describe, expect, it } from "vitest";
import { countryPolicySubmissionSchema, countryPolicyEditorSchema } from "./trading-policy";

const domains = [
  "licensing",
  "product_registration",
  "prescription",
  "controlled",
  "cold_chain",
  "tax_invoice",
  "credit_note",
  "retention",
  "privacy",
  "residency",
  "reporting",
];
function policy() {
  return {
    id: "01a14400-0000-7000-8000-000000000001",
    scope_id: "01a14400-0000-7000-8000-000000000002",
    expected_revision: 0,
    country: "IN",
    jurisdiction: "TEST-STATE",
    category: "otc",
    operation: "order",
    currency: "INR",
    effective_from: "2026-10-16T00:00:00Z",
    effective_until: "2027-10-16T00:00:00Z",
    sources: [
      {
        key: "TEST-source",
        title: "TEST ONLY fictional source",
        url: "https://regulator.example.invalid/test",
        reviewed_at: "2026-10-15T00:00:00Z",
      },
    ],
    requirements: domains.map((domain) => ({
      domain,
      applicability: "required",
      reason: "TEST ONLY fictional requirement",
      source_keys: ["TEST-source"],
    })),
  };
}
describe("reviewed country policy contract", () => {
  it("validates native UTC date inputs before API timestamp conversion", () => {
    const { currency, ...input } = policy();
    expect(currency).toBe("INR");
    input.effective_from = "2026-10-16T00:00";
    input.effective_until = "2027-10-16T00:00";
    input.sources[0].reviewed_at = "2026-10-15T00:00";
    expect(countryPolicyEditorSchema.safeParse(input).success).toBe(true);
    input.effective_from = "2026-02-30T00:00";
    expect(countryPolicyEditorSchema.safeParse(input).success).toBe(false);
  });
  it("accepts the complete India snapshot", () => {
    expect(countryPolicySubmissionSchema.parse(policy())).toMatchObject({
      country: "IN",
      currency: "INR",
      requirements: expect.any(Array),
    });
  });
  // #1287: only India is supported; any other country is refused.
  it("refuses an unsupported country", () => {
    expect(
      countryPolicySubmissionSchema.safeParse({ ...policy(), country: "US", currency: "USD" })
        .success,
    ).toBe(false);
  });
  it("rejects another country's currency and incomplete or duplicate requirements", () => {
    expect(countryPolicySubmissionSchema.safeParse({ ...policy(), currency: "USD" }).success).toBe(
      false,
    );
    const incomplete = policy();
    incomplete.requirements.pop();
    expect(countryPolicySubmissionSchema.safeParse(incomplete).success).toBe(false);
    const duplicate = policy();
    duplicate.requirements[0] = duplicate.requirements[1];
    expect(countryPolicySubmissionSchema.safeParse(duplicate).success).toBe(false);
  });
  it("rejects source credentials, duplicate keys and references without evidence", () => {
    const credential = policy();
    credential.sources[0].url = "https://user:secret@example.invalid/test";
    expect(countryPolicySubmissionSchema.safeParse(credential).success).toBe(false);
    const unknown = policy();
    unknown.requirements[0].source_keys = ["unknown"];
    expect(countryPolicySubmissionSchema.safeParse(unknown).success).toBe(false);
    const duplicate = policy();
    duplicate.sources.push(duplicate.sources[0]);
    expect(countryPolicySubmissionSchema.safeParse(duplicate).success).toBe(false);
  });
  it("rejects a reversed effective window and malformed version identity", () => {
    const reversed = policy();
    reversed.effective_until = reversed.effective_from;
    expect(countryPolicySubmissionSchema.safeParse(reversed).success).toBe(false);
    expect(
      countryPolicySubmissionSchema.safeParse({ ...policy(), id: "not-a-version" }).success,
    ).toBe(false);
  });
});
