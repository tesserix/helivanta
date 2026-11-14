import { afterEach, expect, it, vi } from "vitest";
import {
  createStaffAccount,
  readStaffAccount,
  observeStaffAccount,
  deliverStaffAccountInvitation,
} from "./staff-accounts";

const scope = { tenantId: "11111111-1111-4111-8111-111111111111", subject: "synthetic-owner" };
const id = "019a7777-7777-7777-8777-777777777777";
const delivery = "019a8888-8888-7888-8888-888888888888";
const contact = {
  email: "synthetic@example.invalid",
  given_name: "Synthetic",
  family_name: "Staff",
  reason: "Authorized ordinary account",
};
const receipt = {
  schema_version: 1,
  command_id: id,
  tenant_id: scope.tenantId,
  product_key: "helivanta",
  actor_subject: scope.subject,
  command_state: "created",
  prepared_at: "2026-11-13T00:00:00Z",
  user_id: "synthetic-staff",
  created_at: "2026-11-13T00:00:01Z",
};
afterEach(() => vi.unstubAllGlobals());

it("keeps command identity and frozen contact on explicit retries through the Global API", async () => {
  const fetcher = vi.fn(async () => Response.json(receipt));
  vi.stubGlobal("fetch", fetcher);
  expect(await createStaffAccount(scope, id, contact)).toEqual(receipt);
  expect(await createStaffAccount(scope, id, contact)).toEqual(receipt);
  expect(fetcher).toHaveBeenCalledTimes(2);
  const [path, request] = fetcher.mock.calls[0] as unknown as [string, RequestInit];
  expect(path).toBe("/api/v1/iam/staff-account-commands/" + id);
  expect(request.method).toBe("PUT");
  expect(request.cache).toBe("no-store");
  expect(JSON.parse(String(request.body))).toEqual(contact);
  expect(new Headers(request.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(new Headers(request.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
  expect(fetcher.mock.calls[0]).toEqual(fetcher.mock.calls[1]);
});

it.each([
  { tenant_id: "22222222-2222-4222-8222-222222222222" },
  { actor_subject: "another-owner" },
  { command_id: delivery },
  { product_key: "another-product" },
  { schema_version: 2 },
  { private_code: "do-not-disclose" },
])("rejects an unbound or unexpected source receipt", async (change) => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json({ ...receipt, ...change })),
  );
  await expect(readStaffAccount(scope, id)).rejects.toThrow();
});

it.each(["actor_subject", "tenant_id", "role_key"])(
  "rejects authority-bearing contact field %s before transport",
  async (field) => {
    const fetcher = vi.fn();
    vi.stubGlobal("fetch", fetcher);
    await expect(
      createStaffAccount(scope, id, { ...contact, [field]: "forged" }),
    ).rejects.toThrow();
    expect(fetcher).not.toHaveBeenCalled();
  },
);

it("reads a fresh bound observation without adopting its account by email", async () => {
  const observation = {
    receipt,
    observation: {
      state: "active",
      email_verified: true,
      observed_at: "2026-11-13T00:00:02Z",
      user_revision: "101",
      metadata_revision: "102",
    },
  };
  const fetcher = vi.fn(async () => Response.json(observation));
  vi.stubGlobal("fetch", fetcher);
  expect(await observeStaffAccount(scope, id)).toEqual(observation);
  const [path, request] = fetcher.mock.calls[0] as unknown as [string, RequestInit];
  expect(path).toBe("/api/v1/iam/staff-account-commands/" + id + "/observations");
  expect(request.method).toBe("POST");
  expect(request.body).toBe("{}");
});

it("binds explicit delivery to its own retained id and never sends contact or authority in its body", async () => {
  const sent = {
    state: "email_action_required",
    invitation_id: delivery,
    phase: "email",
    expires_at: "2026-11-13T00:30:00Z",
  };
  const fetcher = vi.fn(async () => Response.json(sent));
  vi.stubGlobal("fetch", fetcher);
  expect(await deliverStaffAccountInvitation(scope, id, delivery)).toEqual(sent);
  const [path, request] = fetcher.mock.calls[0] as unknown as [string, RequestInit];
  expect(path).toBe("/api/v1/iam/staff-account-commands/" + id + "/invitations/" + delivery);
  expect(request.method).toBe("PUT");
  expect(request.body).toBe("{}");
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json({ ...sent, invitation_id: id })),
  );
  await expect(deliverStaffAccountInvitation(scope, id, delivery)).rejects.toThrow();
});
