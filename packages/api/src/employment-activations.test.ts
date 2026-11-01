import { afterEach, expect, it, vi } from "vitest";
import { getEmploymentActivationSource, activateEmployment } from "./employment-activations";

const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  hospitalId: "22222222-2222-4222-8222-222222222222",
  staffId: "33333333-3333-4333-8333-333333333333",
  subject: "synthetic-activator",
  hrIntake: true as const,
};
const review = "019a0000-0000-7000-8000-000000000001";
const revision = "019a0000-0000-7000-8000-000000000002";
const id = "019a0000-0000-7000-8000-000000000003";
const command = "019a0000-0000-7000-8000-000000000004";
const source = {
  hospital_id: scope.hospitalId,
  staff_id: scope.staffId,
  review_id: review,
  review_revision_id: revision,
  review_version: 2,
  staff_version: 1,
  employment_epoch: 1,
  current_state: "eligible",
  can_activate: true,
  starts_at: "2026-11-01T00:00:00Z",
  ends_at: null,
};
const input = {
  command_id: command,
  review_id: review,
  review_revision_id: revision,
  review_version: 2,
  staff_version: 1,
  employment_epoch: 1,
  reason: "Synthetic independently approved appointment",
  activation_examined: true,
};
const receipt = {
  generation: {
    id,
    revision_id: command,
    hospital_id: scope.hospitalId,
    staff_id: scope.staffId,
    version: 1,
    review_id: review,
    review_revision_id: revision,
    review_version: 2,
    review_staff_version: 1,
    review_employment_epoch: 1,
    staff_version: 2,
    employment_epoch: 2,
    identity_hash: "a".repeat(64),
    starts_at: source.starts_at,
    ends_at: null,
    access_intent: "digital",
    declared_subject: "synthetic-recipient",
    activated_by_subject: scope.subject,
    reason: input.reason,
    activated_at: "2026-11-01T01:00:00Z",
  },
  current_state: "effective",
  grants_access: false,
};
afterEach(() => vi.unstubAllGlobals());
it("does not send an activation without the required examination confirmation", async () => {
  const fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);
  await expect(
    activateEmployment(scope, id, { ...input, activation_examined: false }),
  ).rejects.toThrow();
  expect(fetch).not.toHaveBeenCalled();
});
it("binds source eligibility to the original workspace and activation mode", async () => {
  const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify(source), { status: 200 }));
  vi.stubGlobal("fetch", fetch);
  expect(await getEmploymentActivationSource(scope, review)).toEqual(source);
  const [url, request] = fetch.mock.calls[0]!;
  expect(url).toBe(
    `/api/v1/workforce/hospitals/${scope.hospitalId}/staff/${scope.staffId}/employment-reviews/${review}/activation-source?employment_activation=v1`,
  );
  expect(new Headers(request.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
  expect(new Headers(request.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
});
it("rejects a source from another Staff record before disclosing it", async () => {
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify({ ...source, staff_id: scope.hospitalId }), { status: 200 }),
      ),
  );
  await expect(getEmploymentActivationSource(scope, review)).rejects.toThrow("source");
});
it("verifies the exact consumption relation and rejects a mismatched receipt", async () => {
  const fetch = vi
    .fn()
    .mockResolvedValueOnce(new Response(JSON.stringify(receipt), { status: 200 }))
    .mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          ...receipt,
          generation: { ...receipt.generation, review_staff_version: 2 },
        }),
        { status: 200 },
      ),
    );
  vi.stubGlobal("fetch", fetch);
  expect(await activateEmployment(scope, id, input)).toEqual(receipt);
  expect(fetch.mock.calls[0]![1].method).toBe("PUT");
  expect(JSON.parse(fetch.mock.calls[0]![1].body)).toEqual(input);
  await expect(activateEmployment(scope, id, input)).rejects.toThrow("receipt");
});
it("refuses a receipt claiming that activation grants access", async () => {
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify({ ...receipt, grants_access: true }), { status: 200 }),
      ),
  );
  await expect(activateEmployment(scope, id, input)).rejects.toThrow("receipt");
});
it("retains the historical receipt when identity changes after activation", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ ...receipt, current_state: "identity_changed" }), {
        status: 200,
      }),
    ),
  );
  expect((await activateEmployment(scope, id, input)).current_state).toBe("identity_changed");
});
