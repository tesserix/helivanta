import { z } from "zod";
import { apiFetch } from "./client";
import { membershipRequestOptions } from "./membership-commands";
import { HospitalStocktakeScopeChoiceContract } from "./staff-access-contract";
import {
  HospitalStocktakeDepartmentChoiceContract,
  HospitalStocktakeOutletChoiceContract,
} from "./pharmacy-staff-binding-contract";
import { staffAccessTemplates } from "./staff-access-templates";
import type { StocktakeWorkerScope } from "./stocktake-worker-locations";

export type HospitalStocktakeOutletScope = StocktakeWorkerScope & {
  hospitalId: string;
  departmentId: string;
};
const id = z
  .string()
  .guid()
  .refine((value) => value !== "00000000-0000-0000-0000-000000000000");
const continuation = z
  .object({ next_cursor: z.string().min(1).max(2048).nullable(), has_more: z.boolean() })
  .strict();
const scopesPage = z.object({
  data: z.array(HospitalStocktakeScopeChoiceContract).max(20),
  page: continuation,
});
const departmentsPage = z.object({
  data: z.array(HospitalStocktakeDepartmentChoiceContract).max(20),
  page: continuation,
});
const outletsPage = z.object({
  data: z.array(HospitalStocktakeOutletChoiceContract).max(20),
  page: continuation,
});

export function hospitalStocktakeScopesKey(scope: StocktakeWorkerScope) {
  membershipRequestOptions(scope);
  return ["iam", scope.tenantId, "hospital-stocktake-scopes", scope.subject] as const;
}
export function hospitalStocktakeOutletsKey(scope: HospitalStocktakeOutletScope) {
  membershipRequestOptions(scope);
  return [
    "pharmacy",
    scope.tenantId,
    "hospital-stocktake-outlets",
    scope.subject,
    id.parse(scope.hospitalId),
    id.parse(scope.departmentId),
  ] as const;
}
export function hospitalStocktakeDepartmentsKey(scope: HospitalStocktakeOutletScope) {
  membershipRequestOptions(scope);
  return [
    "pharmacy",
    scope.tenantId,
    "hospital-stocktake-departments",
    scope.subject,
    id.parse(scope.hospitalId),
    id.parse(scope.departmentId),
  ] as const;
}
function query(cursor?: string | null) {
  const params = new URLSearchParams({ limit: "20" });
  if (cursor !== undefined && cursor !== null)
    params.set("cursor", z.string().min(1).max(2048).parse(cursor));
  return params;
}
function validContinuation(page: z.infer<typeof continuation>, cursor?: string | null) {
  if (
    page.has_more !== (page.next_cursor !== null) ||
    (page.has_more && page.next_cursor === cursor)
  )
    throw new Error("The current working-location page could not be verified.");
}
export async function listHospitalStocktakeScopes(
  scope: StocktakeWorkerScope,
  cursor?: string | null,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  hospitalStocktakeScopesKey(scope);
  const result = scopesPage.parse(
    await apiFetch<unknown>(`/iam/me/hospital-pharmacy-stocktake-scopes?${query(cursor)}`, {
      ...membershipRequestOptions(scope),
      signal,
      cache: "no-store",
    }),
  );
  validContinuation(result.page, cursor);
  const seen = new Set<string>();
  for (const item of result.data) {
    const definition = staffAccessTemplates.find(
      (template) => template.key === item.template_key && template.family === "pharmacy_stocktake",
    );
    const permissions = new Set(item.assigned_permissions);
    if (
      item.owner_tenant_id !== scope.tenantId ||
      seen.has(item.id) ||
      !definition ||
      item.template_label !== definition.name ||
      permissions.size !== item.assigned_permissions.length ||
      definition.permissions.length !== permissions.size ||
      definition.permissions.some((permission) => !permissions.has(permission))
    ) {
      throw new Error("The current hospital stocktake assignment could not be verified.");
    }
    seen.add(item.id);
  }
  return result;
}
export async function listHospitalStocktakeOutlets(
  scope: HospitalStocktakeOutletScope,
  cursor?: string | null,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  hospitalStocktakeOutletsKey(scope);
  const result = outletsPage.parse(
    await apiFetch<unknown>(
      `/pharmacy/hospitals/${scope.hospitalId}/departments/${scope.departmentId}/stocktake-outlet-choices?${query(cursor)}`,
      { ...membershipRequestOptions(scope), signal, cache: "no-store" },
    ),
  );
  validContinuation(result.page, cursor);
  const bindings = new Set<string>();
  const outlets = new Set<string>();
  for (const item of result.data) {
    if (
      item.owner_tenant_id !== scope.tenantId ||
      item.hospital_id !== scope.hospitalId ||
      item.department_id !== scope.departmentId ||
      bindings.has(item.binding_id) ||
      outlets.has(item.outlet_id)
    )
      throw new Error("The current hospital stocktake outlet could not be verified.");
    new Intl.DateTimeFormat("en-IN", { timeZone: item.time_zone }).format(0);
    bindings.add(item.binding_id);
    outlets.add(item.outlet_id);
  }
  return result;
}

export async function listHospitalStocktakeDepartments(
  scope: HospitalStocktakeOutletScope,
  cursor?: string | null,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  hospitalStocktakeDepartmentsKey(scope);
  const result = departmentsPage.parse(
    await apiFetch<unknown>(
      `/pharmacy/hospitals/${scope.hospitalId}/departments/${scope.departmentId}/stocktake-department-choices?${query(cursor)}`,
      { ...membershipRequestOptions(scope), signal, cache: "no-store" },
    ),
  );
  validContinuation(result.page, cursor);
  const seen = new Set<string>();
  for (const item of result.data) {
    if (
      item.owner_tenant_id !== scope.tenantId ||
      item.hospital_id !== scope.hospitalId ||
      item.scope_department_id !== scope.departmentId ||
      seen.has(item.department_id)
    )
      throw new Error("The current hospital working department could not be verified.");
    seen.add(item.department_id);
  }
  return result;
}
