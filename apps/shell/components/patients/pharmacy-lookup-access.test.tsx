import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithNamedAccounts, accountText } from "../../test/account-names";
const renderWithProviders = (ui: Parameters<typeof renderWithNamedAccounts>[0]) =>
  renderWithNamedAccounts(ui, {
    "user-admin": "QA Admin",
    "user-pharmacist": "QA Pharmacist",
    "user-granted": "QA Granted",
    "user-receptionist": "QA Receptionist",
    "synthetic-maker": "QA Maker",
    "synthetic-checker": "QA Checker",
  });
import { PharmacyLookupAccess } from "./pharmacy-lookup-access";

const hospitalId = "22222222-2222-4222-8222-222222222222";
const pharmacyId = "0199e000-0000-7000-8000-000000000010";
const page = (data: unknown[]) =>
  JSON.stringify({ data, page: { has_more: false, next_cursor: null } });

afterEach(() => vi.unstubAllGlobals());

function stub(perms: string[], grants: { subject: string }[]) {
  const fetcher = vi.fn(async (url: string, init: RequestInit = {}) => {
    if (url.endsWith("permissions"))
      return new Response(JSON.stringify({ tenant_id: "t", subject: "user-admin", data: perms }));
    if (init.method === "PUT" || init.method === "DELETE") return new Response("{}");
    if (url.includes("/iam/members"))
      return new Response(
        page([
          { id: "m1", subject: "user-pharmacist", role_key: "pharmacist" },
          { id: "m2", subject: "user-granted", role_key: "pharmacist" },
          { id: "m3", subject: "user-receptionist", role_key: "receptionist" },
        ]),
      );
    return new Response(
      page(
        grants.map((g) => ({
          ...g,
          pharmacy_id: pharmacyId,
          granted_by: "user-admin",
          created_at: "",
        })),
      ),
    );
  });
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}

const writes = (fetcher: ReturnType<typeof stub>) =>
  fetcher.mock.calls.filter(([, init]) => {
    const method = (init as RequestInit | undefined)?.method;
    return method === "PUT" || method === "DELETE";
  }) as [string, RequestInit][];

function render() {
  return renderWithProviders(
    <PharmacyLookupAccess
      hospitalId={hospitalId}
      pharmacyId={pharmacyId}
      pharmacyName="TEST Ward Pharmacy"
    />,
  );
}

describe("PharmacyLookupAccess", () => {
  it("grants a member who does not have access yet", async () => {
    const fetcher = stub(["iam.member.manage"], [{ subject: "user-granted" }]);
    const { user } = render();
    expect(await screen.findByText(accountText("QA Granted"))).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Grant lookup access" }));
    expect(await screen.findByText("Choose a member")).toBeInTheDocument();
    expect(writes(fetcher)).toHaveLength(0);
    await user.click(screen.getByRole("combobox", { name: "Pharmacist" }));
    expect(screen.queryByRole("option", { name: "QA Granted · Staff member" })).toBeNull();
    expect(screen.queryByRole("option", { name: "QA Receptionist · Staff member" })).toBeNull();
    await user.click(screen.getByRole("option", { name: "QA Pharmacist · Staff member" }));
    await user.click(screen.getByRole("button", { name: "Grant lookup access" }));
    await waitFor(() => expect(writes(fetcher)).toHaveLength(1));
    const [url, init] = writes(fetcher)[0];
    expect(init.method).toBe("PUT");
    expect(url).toContain(
      `/patients/hospitals/${hospitalId}/pharmacy-lookup-access/${pharmacyId}/user-pharmacist`,
    );
  });

  it("revokes only after confirming", async () => {
    const fetcher = stub(["iam.member.manage"], [{ subject: "user-granted" }]);
    const { user } = render();
    await user.click(await screen.findByRole("button", { name: "Revoke" }));
    const dialog = await screen.findByRole("dialog");
    expect(writes(fetcher)).toHaveLength(0);
    await user.click(within(dialog).getByRole("button", { name: "Revoke" }));
    await waitFor(() => expect(writes(fetcher)).toHaveLength(1));
    expect(writes(fetcher)[0][1].method).toBe("DELETE");
    expect(writes(fetcher)[0][0]).toContain("/user-granted");
  });

  it("offers no grant form without member management", async () => {
    stub([], []);
    render();
    expect(await screen.findByText("No pharmacist has lookup access.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Grant lookup access" })).toBeNull();
  });

  // #1591: a refused read is not an outage, so it offers no retry.
  it("shows a 403 as a refusal with no retry", async () => {
    stub(["iam.member.manage"], []);
    const base = vi.mocked(fetch);
    const original = base.getMockImplementation()!;
    base.mockImplementation(async (url, init) =>
      String(url).includes("pharmacy-lookup-access")
        ? new Response(JSON.stringify({ error: "forbidden", message: "no" }), { status: 403 })
        : original(url, init),
    );
    render();
    expect(
      await screen.findByText("This wasn't found, or you don't have access to it."),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Try again" })).toBeNull();
  });
});
