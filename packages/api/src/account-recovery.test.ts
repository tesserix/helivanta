import { afterEach, describe, expect, it, vi } from "vitest";
import {
  requestPasswordRecovery,
  verifyPasswordRecovery,
  submitRecoveryPassword,
  getPasswordRecoveryStatus,
  prepareRecoveryPassword,
  cancelRecoveryPassword,
} from "./account-recovery";

const requestId = "019c0000-0000-7000-8000-000000000001";
const commandId = "019c0000-0000-7000-8000-000000000002";
afterEach(() => vi.unstubAllGlobals());

describe("Password recovery client", () => {
  it("sends only public canonical inputs through the Global API", async () => {
    const calls: { url: string; body: unknown }[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init?: RequestInit) => {
        calls.push({ url, body: init?.body ? JSON.parse(String(init.body)) : undefined });
        if (url.endsWith("/verify")) return Response.json({ state: "verified" });
        if (url.endsWith("/password")) return Response.json({ state: "unconfirmed" });
        return Response.json({ state: init?.method === "POST" ? "check_email" : "completed" });
      }),
    );
    await requestPasswordRecovery({
      auth_request_id: "V2_test",
      request_id: requestId,
      email: "person@example.test",
    });
    await verifyPasswordRecovery("01234567");
    expect(
      await submitRecoveryPassword({ command_id: commandId, password: "synthetic-password" }),
    ).toEqual({ state: "unconfirmed" });
    expect(await getPasswordRecoveryStatus()).toEqual({ state: "completed" });
    expect(calls).toEqual([
      {
        url: "/api/v1/auth/password-recovery",
        body: { auth_request_id: "V2_test", request_id: requestId, email: "person@example.test" },
      },
      { url: "/api/v1/auth/password-recovery/verify", body: { code: "01234567" } },
      {
        url: "/api/v1/auth/password-recovery/password",
        body: { command_id: commandId, password: "synthetic-password" },
      },
      { url: "/api/v1/auth/password-recovery", body: undefined },
    ]);
  });

  it("does not retry an unknown password write or accept invented success", async () => {
    const fetch = vi.fn(async () => Response.json({ state: "invented_success" }));
    vi.stubGlobal("fetch", fetch);
    await expect(
      submitRecoveryPassword({ command_id: commandId, password: "synthetic-password" }),
    ).rejects.toThrow();
    expect(fetch).toHaveBeenCalledTimes(1);
  });
});

it("prepares and cancels without password payload and refuses private receipt selectors", async () => {
  const fetch = vi.fn(async (_url: string, init?: RequestInit) =>
    Response.json({ state: init?.body === "{}" ? "cancelled" : "prepared" }),
  );
  vi.stubGlobal("fetch", fetch);
  expect(await prepareRecoveryPassword(commandId)).toEqual({ state: "prepared" });
  expect(await cancelRecoveryPassword()).toEqual({ state: "cancelled" });
  expect(fetch.mock.calls.map(([url, init]) => ({ url, body: init?.body }))).toEqual([
    {
      url: "/api/v1/auth/password-recovery/prepare",
      body: JSON.stringify({ command_id: commandId }),
    },
    { url: "/api/v1/auth/password-recovery/cancel", body: "{}" },
  ]);
  fetch.mockImplementation(async () => Response.json({ state: "prepared", commandId }));
  await expect(getPasswordRecoveryStatus()).rejects.toThrow();
});
