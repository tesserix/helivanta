import type { SupplierGrantInput, SupplierGrantDecisionInput } from "./supplier-access-contract";
export type SupplierSourceScope = {
  tenantId: string;
  subject: string;
  product: "supply" | "pharmacy";
  organizationId: string;
  locationId: string;
};
export type SupplierHospitalScope = { tenantId: string; subject: string; hospitalId: string };
export async function prepareSupplierGrant(
  _scope: SupplierSourceScope,
  _id: string,
  _input: SupplierGrantInput,
): Promise<unknown> {
  throw new Error("Supplier grant client is not implemented");
}
export async function getSupplierGrant(_scope: SupplierSourceScope, _id: string): Promise<unknown> {
  throw new Error("Supplier grant client is not implemented");
}
export async function decideSupplierGrant(
  _scope: SupplierSourceScope,
  _id: string,
  _command: string,
  _input: SupplierGrantDecisionInput,
): Promise<unknown> {
  throw new Error("Supplier grant client is not implemented");
}
export async function recoverSupplierGrantCommand(
  _scope: SupplierSourceScope,
  _id: string,
  _command: string,
  _input: SupplierGrantDecisionInput,
): Promise<unknown> {
  throw new Error("Supplier grant client is not implemented");
}
export async function getHospitalSupplierGrant(
  _scope: SupplierHospitalScope,
  _id: string,
): Promise<unknown> {
  throw new Error("Supplier grant client is not implemented");
}
