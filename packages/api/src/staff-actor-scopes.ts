import { z } from "zod";
import { accountScopedRequest, type AccountRequestScope } from "./account-scoped-request";
import { StaffActorHospitalChoiceContract } from "./staff-actor-contract";

export type StaffActorAccountScope = AccountRequestScope;
const cursorSchema = z.string().min(1).max(2048);
const pageSchema = z
  .object({
    data: z.array(StaffActorHospitalChoiceContract).max(20),
    page: z.object({ has_more: z.boolean(), next_cursor: cursorSchema.nullable() }).strict(),
  })
  .strict();

export async function listStaffActorHospitalChoices(
  scope: StaffActorAccountScope,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  const query = new URLSearchParams({ limit: "20" });
  if (cursor !== null) query.set("cursor", cursorSchema.parse(cursor));
  const page = pageSchema.parse(
    await accountScopedRequest(scope, "/workforce/staff-actor-scopes?" + query, signal),
  );
  if (
    page.page.has_more !== (page.page.next_cursor !== null) ||
    (cursor !== null && page.page.next_cursor === cursor) ||
    new Set(page.data.map((row) => row.hospital_id.toLowerCase())).size !== page.data.length
  )
    throw new Error("The Staff hospital choices could not be verified.");
  return page;
}
