import { z } from "zod";
import type { PharmacyScope } from "./pharmacy";

export type PharmacyActorScope = PharmacyScope & { subject: string };

export function pharmacyActorHeaders(scope: PharmacyActorScope) {
  return {
    "X-Helivanta-Expected-Tenant": z.string().guid().parse(scope.tenantId),
    "X-Helivanta-Expected-Subject": z.string().min(1).max(200).parse(scope.subject),
  };
}
