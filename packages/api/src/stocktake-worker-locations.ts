import { z } from "zod";
import { apiFetch } from "./client";
import { membershipRequestOptions } from "./membership-commands";
import {
  StocktakeDutyLocationChoiceContract,
  operatingDutyTemplates,
} from "./operating-assignments-contract";
import type { StaffAssignmentScope } from "./staff-assignments";

export type StocktakeWorkerScope = Pick<StaffAssignmentScope, "tenantId" | "subject">;
export type StocktakeWorkerLocation = z.infer<typeof StocktakeDutyLocationChoiceContract>;

const cursorContract = z.string().min(1).max(2048);
const pageContract = z
  .object({
    data: z.array(StocktakeDutyLocationChoiceContract).max(20),
    page: z.object({ has_more: z.boolean(), next_cursor: cursorContract.nullable() }).strict(),
  })
  .strict()
  .superRefine((value, context) => {
    const valid =
      value.page.has_more === (value.page.next_cursor !== null) &&
      new Set(value.data.map((row) => row.duty_id)).size === value.data.length &&
      value.data.every((row) => {
        const template = operatingDutyTemplates.find(
          (item) => item.key === row.template_key && item.employer_kind === "pharmacy",
        );
        if (
          !template ||
          !template.permissions.includes("pharmacy.stocktake.read") ||
          !template.permissions.every((permission) =>
            permission.startsWith("pharmacy.stocktake."),
          ) ||
          template.permissions.length !== row.assigned_permissions.length ||
          new Set(row.assigned_permissions).size !== row.assigned_permissions.length ||
          !row.assigned_permissions.every((permission) =>
            template.permissions.includes(permission),
          ) ||
          !row.employer_name.trim() ||
          !row.location_name.trim() ||
          !row.location_code.trim() ||
          !row.template_label.trim()
        )
          return false;
        try {
          new Intl.DateTimeFormat("en-IN", { timeZone: row.time_zone });
          return true;
        } catch {
          return false;
        }
      });
    if (!valid)
      context.addIssue({
        code: "custom",
        message: "The current stocktake working locations could not be verified.",
      });
  });

export const stocktakeWorkerLocationsKey = (scope: StocktakeWorkerScope) =>
  ["iam", "stocktake-worker-locations", scope.tenantId, scope.subject] as const;

export async function listStocktakeWorkerLocations(
  scope: StocktakeWorkerScope,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const query = new URLSearchParams({ limit: "20" });
  if (cursor !== null) query.set("cursor", cursorContract.parse(cursor));
  const response = pageContract.parse(
    await apiFetch<unknown>(`/iam/me/pharmacy-stocktake-locations?${query}`, {
      ...membershipRequestOptions(scope),
      signal,
    }),
  );
  if (
    response.data.some((row) => row.owner_tenant_id !== scope.tenantId) ||
    (response.page.has_more && response.page.next_cursor === cursor)
  )
    throw new Error("The current stocktake working locations could not be verified.");
  return response;
}
