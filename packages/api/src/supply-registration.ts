import { z } from "zod";
import { countryProfile } from "./country-profiles";
import {
  supplyLicenseFieldsSchema,
  supplyLicenseInputSchema,
  supplyPremiseFieldsSchema,
  supplyPremiseInputSchema,
  supplyLicenseRenewalSchema,
} from "./supply-contract";
import { supplyKey, type SupplyScope } from "./supply";

export const supplyPremiseFormSchema = supplyPremiseInputSchema.superRefine((value, ctx) => {
  const zones: readonly string[] = countryProfile(value.country_code)?.timezones ?? [];
  if (!zones.includes(value.time_zone))
    ctx.addIssue({
      code: "custom",
      path: ["time_zone"],
      message: "Choose an operating time zone for this country.",
    });
});
export const supplyLicenseFormSchema = supplyLicenseInputSchema.superRefine((value, ctx) => {
  if (value.issued_on < "1900-01-01")
    ctx.addIssue({ code: "custom", path: ["issued_on"], message: "Use a date from 1900 onwards." });
  if (value.expires_on < value.issued_on)
    ctx.addIssue({
      code: "custom",
      path: ["expires_on"],
      message: "Expiry must be on or after the issue date.",
    });
  if (new Set(value.product_categories).size !== value.product_categories.length)
    ctx.addIssue({
      code: "custom",
      path: ["product_categories"],
      message: "Select each product category once.",
    });
});
type RegistryHead = {
  id: string;
  party_id: string;
  version: number;
  created_at: string;
  updated_at: string;
};
export type SupplyPremise = z.infer<typeof supplyPremiseFieldsSchema> & RegistryHead;
export type SupplyLicense = z.infer<typeof supplyLicenseFieldsSchema> &
  RegistryHead & {
    country_code: string;
    status: "declared";
    renewal?: z.infer<typeof supplyLicenseRenewalSchema>;
  };
export { supplyLicenseRenewalSchema } from "./supply-contract";
export type SupplyPremiseInput = z.infer<typeof supplyPremiseInputSchema>;
export type SupplyLicenseInput = z.infer<typeof supplyLicenseInputSchema>;
export type SupplyRegistryScope = SupplyScope & { partyId: string };
export type SupplyRegistryRevision<T> = {
  id: string;
  party_id: string;
  record_id: string;
  version: number;
  snapshot: T;
  actor_subject: string;
  created_at: string;
};
export const supplyRegistryKey = (scope: SupplyRegistryScope, ...parts: unknown[]) =>
  supplyKey(scope, "party", scope.partyId, "registration", ...parts);
export const supplyRegistryRoot = (scope: SupplyRegistryScope, kind: "premises" | "licenses") =>
  `/supply/parties/${encodeURIComponent(scope.partyId)}/${kind}`;
