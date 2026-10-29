import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
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
    expect(await screen.findByText("user-granted")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Grant lookup access" }));
    expect(await screen.findByText("Choose a member")).toBeInTheDocument();
    expect(writes(fetcher)).toHaveLength(0);
    await user.click(screen.getByRole("combobox", { name: "Pharmacist" }));
    expect(screen.queryByRole("option", { name: "user-granted" })).toBeNull();
    expect(screen.queryByRole("option", { name: "user-receptionist" })).toBeNull();
    await user.click(screen.getByRole("option", { name: "user-pharmacist" }));
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
});
