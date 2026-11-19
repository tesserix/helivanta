import { afterEach, expect, it, vi } from "vitest";
import {
  readStaffMFAReviewContext,
  resetStaffMFA,
  readStaffMFARecovery,
  cancelStaffMFARecovery,
} from "./staff-mfa-recovery";

const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "acting-admin",
  employerKind: "hospital" as const,
  employerId: "11111111-1111-4111-8111-111111111112",
  staffId: "11111111-1111-4111-8111-111111111113",
};
const id = "019f39c2-1738-7998-b5de-6d4214f614e0";
const input = {
  commandId: id,
  expectedLinkId: "019f39c2-1738-7998-b5de-6d4214f614e1",
  expectedLinkVersion: 2,
  expectedStaffVersion: 1,
  reason: "Verified lost authenticator",
};
const receipt = { commandId: id, state: "awaiting_enrollment" };
afterEach(() => vi.unstubAllGlobals());

it.each([
  ["hospital", "hospitals"],
  ["pharmacy", "pharmacies"],
  ["supplier", "suppliers"],
] as const)(
  "binds %s reset to its verified account, employer and original command",
  async (employerKind, family) => {
    const fetcher = vi.fn(async () => Response.json(receipt));
    vi.stubGlobal("fetch", fetcher);
    expect(await resetStaffMFA({ ...scope, employerKind }, input)).toEqual(receipt);
    expect(fetcher).toHaveBeenCalledTimes(1);
    const [path, request] = fetcher.mock.calls[0] as unknown as [string, RequestInit];
    expect(path).toBe(
      `/api/v1/workforce/${family}/${scope.employerId}/staff/${scope.staffId}/mfa-recoveries/${id}`,
    );
    expect(request.method).toBe("PUT");
    expect(request.cache).toBe("no-store");
    expect(JSON.parse(String(request.body))).toEqual(input);
    expect(new Headers(request.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
    expect(new Headers(request.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
  },
);

it("reconciles or cancels the existing command without sending the reason or a replacement command", async () => {
  const fetcher = vi.fn(async () => Response.json({ commandId: id, state: "cancelled" }));
  vi.stubGlobal("fetch", fetcher);
  await readStaffMFARecovery(scope, id);
  await cancelStaffMFARecovery(scope, id);
  expect(fetcher).toHaveBeenCalledTimes(2);
  const [readPath, readRequest] = fetcher.mock.calls[0] as unknown as [string, RequestInit];
  const [cancelPath, cancelRequest] = fetcher.mock.calls[1] as unknown as [string, RequestInit];
  expect(readPath).toContain(`/mfa-recoveries/${id}`);
  expect(readRequest.body).toBeUndefined();
  expect(cancelPath).toBe(readPath + "/cancel");
  expect(cancelRequest.method).toBe("POST");
  expect(cancelRequest.body).toBe("{}");
});

it.each([
  { commandId: "019f39c2-1738-7998-b5de-6d4214f614e2", state: "completed" },
  { ...receipt, state: "assumed_success" },
  { ...receipt, subject: "private-staff" },
])("refuses a mismatched or private receipt", async (body) => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(body)),
  );
  await expect(readStaffMFARecovery(scope, id)).rejects.toThrow();
});

it("does not retry an ambiguous reset response", async () => {
  const fetcher = vi.fn(async () => Response.json({ error: "unavailable" }, { status: 503 }));
  vi.stubGlobal("fetch", fetcher);
  await expect(resetStaffMFA(scope, input)).rejects.toThrow();
  expect(fetcher).toHaveBeenCalledTimes(1);
});

it.each([
  { ...scope, employerKind: "hospital/other" },
  { ...scope, employerId: "../other" },
  { ...scope, staffId: "00000000-0000-0000-0000-000000000000" },
  { ...scope, subject: "" },
])("rejects an invalid selection before the network", async (selection) => {
  const fetcher = vi.fn();
  vi.stubGlobal("fetch", fetcher);
  await expect(readStaffMFARecovery(selection as typeof scope, id)).rejects.toThrow();
  expect(fetcher).not.toHaveBeenCalled();
});

it("refuses browser-supplied target identity and recased intent fields", async () => {
  const fetcher = vi.fn();
  vi.stubGlobal("fetch", fetcher);
  await expect(
    resetStaffMFA(scope, { ...input, subject: "other-staff" } as typeof input),
  ).rejects.toThrow();
  await expect(resetStaffMFA(scope, { ...input, CommandId: id } as typeof input)).rejects.toThrow();
  expect(fetcher).not.toHaveBeenCalled();
});

const directoryRecord = {
  id: scope.staffId,
  hospital_id: scope.employerId,
  employee_code: "SYN-MFA",
  display_name: "Synthetic Staff",
  job_category: "other",
  employment_status: "active",
  created_at: "2026-11-19T00:00:00Z",
  updated_at: "2026-11-19T00:00:00Z",
  version: 7,
  directory_context_version: "v1",
};
it("reads the current canonical Staff version without inventing one from a legacy row", async () => {
  const fetcher = vi.fn(async () => Response.json(directoryRecord));
  vi.stubGlobal("fetch", fetcher);
  expect(await readStaffMFAReviewContext(scope)).toEqual({ staffVersion: 7, active: true });
  expect((fetcher.mock.calls[0] as unknown as [string])[0]).toBe(
    `/api/v1/workforce/hospitals/${scope.employerId}/staff/${scope.staffId}?directory_context=v1`,
  );
});
it.each([
  { ...directoryRecord, id: scope.employerId },
  { ...directoryRecord, hospital_id: scope.staffId },
  { ...directoryRecord, version: 0 },
  { ...directoryRecord, directory_context_version: undefined },
])("refuses an unbound or legacy Staff review context", async (record) => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(record)),
  );
  await expect(readStaffMFAReviewContext(scope)).rejects.toThrow();
});

it.each([
  ["pharmacy", "pharmacies", "pharmacy_id"],
  ["supplier", "suppliers", "supplier_id"],
] as const)(
  "reads %s recovery context using its canonical employer field",
  async (employerKind, family, ownerField) => {
    const { hospital_id, ...record } = directoryRecord;
    const fetcher = vi.fn(async () => Response.json({ ...record, [ownerField]: hospital_id }));
    vi.stubGlobal("fetch", fetcher);
    expect(await readStaffMFAReviewContext({ ...scope, employerKind })).toEqual({
      staffVersion: 7,
      active: true,
    });
    expect((fetcher.mock.calls[0] as unknown as [string])[0]).toBe(
      `/api/v1/workforce/${family}/${scope.employerId}/staff/${scope.staffId}?directory_context=v1`,
    );
  },
);

it.each(["pharmacy", "supplier"] as const)(
  "refuses a hospital-shaped %s recovery context",
  async (employerKind) => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => Response.json(directoryRecord)),
    );
    await expect(readStaffMFAReviewContext({ ...scope, employerKind })).rejects.toThrow();
  },
);

it.each([
  ["pharmacy", "pharmacy_id"],
  ["supplier", "supplier_id"],
] as const)(
  "binds %s context to its selected employer and Staff record",
  async (employerKind, ownerField) => {
    const { hospital_id, ...record } = directoryRecord;
    for (const changed of [
      { [ownerField]: scope.staffId },
      { id: scope.employerId },
      { version: 0 },
      { directory_context_version: undefined },
    ]) {
      vi.stubGlobal(
        "fetch",
        vi.fn(async () => Response.json({ ...record, [ownerField]: hospital_id, ...changed })),
      );
      await expect(readStaffMFAReviewContext({ ...scope, employerKind })).rejects.toThrow();
    }
  },
);
