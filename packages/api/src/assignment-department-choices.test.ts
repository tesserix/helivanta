import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiFetch } from "./client";
import {
  assignmentDepartmentChoicesKey,
  listAssignmentDepartmentChoices,
} from "./assignment-department-choices";
vi.mock("./client", () => ({ apiFetch: vi.fn() }));
const scope = {
  tenantId: "11111111-1111-1111-1111-111111111111",
  subject: "synthetic-manager",
  hospitalId: "019a55c0-1111-7111-8111-111111111111",
};
const row = {
  id: "019a55c0-1111-7111-8111-111111111112",
  hospital_id: scope.hospitalId,
  revision_id: "019a55c0-1111-7111-8111-111111111113",
  version: 1,
  code: "PHARMACY",
  name: "Pharmacy",
  kind: "department",
  status: "active",
  effective_at: "2026-11-14T00:00:00.000Z",
  created_at: "2026-11-14T00:00:00.000Z",
};
const page = { data: [row], page: { has_more: false, next_cursor: null } };
beforeEach(() => {
  vi.mocked(apiFetch).mockReset();
});
describe("governed department choices", () => {
  it("uses the Organization owner route with the exact account assertions", async () => {
    vi.mocked(apiFetch).mockResolvedValue(page);
    expect(await listAssignmentDepartmentChoices(scope)).toEqual(page);
    expect(apiFetch).toHaveBeenCalledWith(
      `/organization/hospitals/${scope.hospitalId}/assignment-department-choices?limit=20`,
      expect.objectContaining({
        cache: "no-store",
        headers: {
          "X-Helivanta-Expected-Tenant": scope.tenantId,
          "X-Helivanta-Expected-Subject": scope.subject,
        },
      }),
    );
    expect(assignmentDepartmentChoicesKey(scope)).not.toEqual(
      assignmentDepartmentChoicesKey({ ...scope, subject: "other" }),
    );
    expect(assignmentDepartmentChoicesKey(scope)).not.toEqual(
      assignmentDepartmentChoicesKey({ ...scope, hospitalId: row.id }),
    );
  });
  it.each([
    { ...page, data: [{ ...row, hospital_id: row.id }] },
    { ...page, data: [{ ...row, status: "retired" }] },
    { ...page, data: [row, row] },
    { ...page, data: [{ ...row, revision_id: "11111111-1111-4111-8111-111111111111" }] },
    { ...page, data: [{ ...row, kind: "sub_unit" }] },
    { ...page, page: { has_more: true, next_cursor: null } },
    { ...page, data: [], page: { has_more: true, next_cursor: "opaque" } },
    { ...page, data: [{ ...row, actor_subject: "private" }] },
  ])("rejects changed scope, ineligible or malformed owner data", async (value) => {
    vi.mocked(apiFetch).mockResolvedValue(value);
    await expect(listAssignmentDepartmentChoices(scope)).rejects.toThrow();
  });
  it("does not call the owner for an invalid hospital or aborted request", async () => {
    await expect(
      listAssignmentDepartmentChoices({ ...scope, hospitalId: "bad" }),
    ).rejects.toThrow();
    const abort = new AbortController();
    abort.abort();
    await expect(listAssignmentDepartmentChoices(scope, null, abort.signal)).rejects.toThrow();
    expect(apiFetch).not.toHaveBeenCalled();
  });
  it("keeps an authorization refusal and never retries the legacy directory", async () => {
    const denied = Object.assign(new Error("source denied"), { status: 404, code: "not_found" });
    vi.mocked(apiFetch).mockRejectedValue(denied);
    await expect(listAssignmentDepartmentChoices(scope)).rejects.toMatchObject({
      message: "source denied",
      status: 404,
      code: "not_found",
    });
    expect(apiFetch).toHaveBeenCalledTimes(1);
  });
});
