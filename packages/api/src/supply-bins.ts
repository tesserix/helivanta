import type { z } from "zod";
import { supplyBinFieldsSchema, supplyBinInputSchema } from "./supply-contract";
import {
  supplyRegistryKey,
  supplyRegistryRoot,
  type SupplyRegistryScope,
} from "./supply-registration";

export type SupplyBinInput = z.infer<typeof supplyBinInputSchema>;
export type SupplyBin = z.infer<typeof supplyBinFieldsSchema> & {
  id: string;
  party_id: string;
  premise_id: string;
  country_code: string;
  version: number;
  created_at: string;
  updated_at: string;
};
export const supplyBinsRoot = (scope: SupplyRegistryScope, premiseId: string) =>
  `${supplyRegistryRoot(scope, "premises")}/${encodeURIComponent(premiseId)}/bins`;
export const supplyBinsKey = (scope: SupplyRegistryScope, premiseId: string, ...parts: unknown[]) =>
  supplyRegistryKey(scope, "premises", premiseId, "bins", ...parts);
