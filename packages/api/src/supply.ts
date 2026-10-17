import { z } from "zod";
import { countryProfile } from "./country-profiles";
import { supplyPartyFieldsSchema, supplyPartyInputSchema } from "./supply-contract";
export { v7 as newSupplyId } from "uuid";
export * from "./supply-contract";

export const supplyPartyFormSchema = supplyPartyInputSchema.superRefine((value, ctx) => {
  for (const collection of ["contacts", "addresses", "tax_registrations"] as const) {
    const ids = new Set<string>();
    value[collection].forEach((row, index) => {
      if (ids.has(row.id))
        ctx.addIssue({
          code: "custom",
          path: [collection, index, "id"],
          message: "Each record must have a unique identity.",
        });
      ids.add(row.id);
    });
  }
  value.addresses.forEach((address, index) => {
    if (address.kind === "registered" && address.country_code !== value.country_code)
      ctx.addIssue({
        code: "custom",
        path: ["addresses", index, "country_code"],
        message: "Registered address must match the organisation's country.",
      });
  });
  const identities = new Set<string>();
  value.tax_registrations.forEach((tax, index) => {
    const permitted: readonly string[] =
      countryProfile(value.country_code)?.tax_identifier_kinds ?? [];
    if (!permitted.includes(tax.kind))
      ctx.addIssue({
        code: "custom",
        path: ["tax_registrations", index, "kind"],
        message: "Choose an identifier for this country.",
      });
    const key = `${tax.kind}:${tax.number}`;
    if (identities.has(key))
      ctx.addIssue({
        code: "custom",
        path: ["tax_registrations", index, "number"],
        message: "This tax identifier is already listed.",
      });
    identities.add(key);
  });
});

export type SupplyPartyInput = z.infer<typeof supplyPartyInputSchema>;
export type SupplyParty = z.infer<typeof supplyPartyFieldsSchema> & {
  id: string;
  onboarding_status: "draft";
  version: number;
  created_at: string;
  updated_at: string;
};
export type SupplyPartySummary = Pick<
  SupplyParty,
  | "id"
  | "code"
  | "record_kind"
  | "legal_name"
  | "trading_name"
  | "business_kind"
  | "country_code"
  | "onboarding_status"
  | "created_at"
> & { access_roles: string[] };
export type SupplyPartyRevision = {
  id: string;
  party_id: string;
  version: number;
  snapshot: SupplyParty;
  actor_subject: string;
  created_at: string;
};
export type SupplyScope = { tenantId: string; subject: string };
export function supplyHeaders(scope: SupplyScope) {
  return new Headers({ "X-Helivanta-Expected-Tenant": scope.tenantId });
}
export function supplyKey(scope: SupplyScope, ...parts: unknown[]) {
  return ["supply", scope.tenantId, scope.subject, ...parts];
}
export function newSupplyPartyInput(): SupplyPartyInput {
  return {
    expected_version: 0,
    code: "",
    record_kind: "counterparty",
    legal_name: "",
    trading_name: "",
    business_kind: "distributor",
    country_code: "IN",
    jurisdiction: "",
    registration_number: "",
    corporate_group: "",
    website: "",
    default_credit_days: 0,
    contacts: [],
    addresses: [],
    tax_registrations: [],
  };
}
