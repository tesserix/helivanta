import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import { renderWithProviders } from "@hms/api/testing";
import { TenantPicker } from "./tenant-picker";

const signInWithCustomToken = vi.hoisted(() => vi.fn());
vi.mock("firebase/auth", () => ({ signInWithCustomToken }));
vi.mock("@/lib/firebase", () => ({ firebaseAuth: () => ({ name: "test-auth" }) }));

// Every network call the picker makes, in the order it made them, so a
// test can assert the *sequence* — the original bug was a switcher that
// stopped after step 1 and reloaded, so "the session POST happened" is
// only meaningful together with "after the custom token came back".
const calls: string[] = [];

function stubFetch(switchBody: unknown = { tenant_id: "t2", custom_token: "custom-abc" }) {
  const fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
    calls.push(`${init?.method ?? "GET"} ${url}`);
    if (url.endsWith("/iam/me/tenants")) {
      return {
        ok: true,
        status: 200,
        json: async () => ({
          data: [
            { tenant_id: "t1", roles: ["doctor"] },
            { tenant_id: "t2", roles: ["nurse"] },
          ],
        }),
      };
    }
    if (url.endsWith("/iam/me/tenant")) {
      return { ok: true, status: 200, json: async () => switchBody };
    }
    if (url === "/api/session") {
      return { ok: true, status: 200, json: async () => ({ ok: true }) };
    }
    throw new Error(`unexpected fetch: ${url}`);
  });
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

beforeEach(() => {
  calls.length = 0;
  signInWithCustomToken.mockReset();
  signInWithCustomToken.mockImplementation(async () => {
    calls.push("signInWithCustomToken");
    return { user: { getIdToken: async () => "fresh-id-token" } };
  });
  vi.stubGlobal("location", { reload: vi.fn() });
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("TenantPicker", () => {
  it("renders nothing for a single-tenant user", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({ data: [{ tenant_id: "t1", roles: ["doctor"] }] }),
      }),
    );
    renderWithProviders(<TenantPicker />);

    // renderWithProviders always mounts a sonner <Toaster/> alongside the
    // component under test, so the render container is never fully empty —
    // assert on the picker's own markup instead.
    await waitFor(() => expect(screen.queryByText("Hospital")).not.toBeInTheDocument());
    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
  });

  it("lists every tenant for a multi-hospital user", async () => {
    stubFetch();
    renderWithProviders(<TenantPicker />);

    await waitFor(() => expect(screen.getByRole("combobox")).toBeInTheDocument());
    expect(screen.getAllByRole("option")).toHaveLength(2);
  });

  it("re-mints the session: switch, sign in with the custom token, then replace the cookie", async () => {
    const fetchMock = stubFetch();
    const { user } = renderWithProviders(<TenantPicker />);
    await waitFor(() => expect(screen.getByRole("combobox")).toBeInTheDocument());

    await user.selectOptions(screen.getByRole("combobox"), "t2");

    await waitFor(() => expect(screen.getByText("Switched hospital")).toBeInTheDocument());

    // The whole point: a switch is not done when the backend says yes.
    // Without the middle two steps the user keeps their old tenant_id
    // claim and silently stays in the hospital they started in.
    expect(calls).toEqual([
      "GET /api/v1/iam/me/tenants",
      "POST /api/v1/iam/me/tenant",
      "signInWithCustomToken",
      "POST /api/session",
    ]);

    expect(signInWithCustomToken).toHaveBeenCalledWith(expect.anything(), "custom-abc");

    const sessionCall = fetchMock.mock.calls.find(([url]) => url === "/api/session");
    expect(JSON.parse(sessionCall![1]!.body as string)).toEqual({ idToken: "fresh-id-token" });

    // Reload last, and only after the cookie was replaced — reloading
    // first would just re-render the old tenant's data.
    await waitFor(() => expect(window.location.reload).toHaveBeenCalled());
  });

  it("keeps the old session and does not reload when the exchange fails", async () => {
    stubFetch();
    signInWithCustomToken.mockRejectedValue(new Error("custom token rejected"));
    const { user } = renderWithProviders(<TenantPicker />);
    await waitFor(() => expect(screen.getByRole("combobox")).toBeInTheDocument());

    await user.selectOptions(screen.getByRole("combobox"), "t2");

    await waitFor(() => expect(screen.getByText("custom token rejected")).toBeInTheDocument());
    expect(calls).not.toContain("POST /api/session");
    expect(window.location.reload).not.toHaveBeenCalled();
  });
});
