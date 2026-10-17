import { screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import type { SupplyPremise } from "@helivanta/api";
import { WarehouseBins } from "./warehouse-bins";

const scope = {
  tenantId: "11111111-1111-1111-1111-111111111111",
  subject: "TEST-owner",
  partyId: "0199e000-0000-7000-8000-000000000001",
};
const premise = {
  id: "0199e000-0000-7000-8000-000000000002",
  code: "TEST-WAREHOUSE",
  name: "TEST Warehouse",
  status: "active",
  kind: "warehouse",
} as SupplyPremise;
const bin = {
  id: "0199e000-0000-7000-8000-000000000003",
  party_id: scope.partyId,
  premise_id: premise.id,
  country_code: "IN",
  code: "TEST-BIN",
  name: "TEST Private Bin",
  aisle: "A",
  rack: "R1",
  shelf: "S1",
  storage_class: "ambient",
  status: "active",
  version: 1,
};
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
function fixture(permissions = ["supply.bin.read", "supply.bin.write"]) {
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    if (url.endsWith("permissions"))
      return new Response(
        JSON.stringify({ tenant_id: scope.tenantId, subject: scope.subject, data: permissions }),
      );
    if (init?.method === "PUT")
      return new Response(
        JSON.stringify({ message: "TEST service unavailable", error: "unavailable" }),
        { status: 503 },
      );
    return new Response(JSON.stringify(page([])));
  });
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}
afterEach(() => vi.unstubAllGlobals());
it("makes no bin query without permission, even with an organisation role", async () => {
  const fetcher = fixture([]);
  renderWithProviders(
    <WarehouseBins scope={scope} premise={premise} roles={["administrator"]} onClose={() => {}} />,
  );
  await waitFor(() =>
    expect(fetcher.mock.calls.some(([url]) => url.endsWith("permissions"))).toBe(true),
  );
  expect(fetcher.mock.calls.some(([url]) => url.includes("/bins"))).toBe(false);
  expect(screen.queryByRole("region", { name: "Warehouse bins" })).not.toBeInTheDocument();
});
it("allows auditor history without rendering write controls", async () => {
  const initial = fixture();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.includes("/revisions"))
        return new Response(
          JSON.stringify(
            page([
              {
                id: "TEST-revision",
                version: 1,
                snapshot: bin,
                actor_subject: "TEST-owner",
                created_at: "2026-10-17T00:00:00Z",
              },
            ]),
          ),
        );
      if (url.includes("/bins")) return new Response(JSON.stringify(page([bin])));
      return initial(url, init);
    }),
  );
  const { user } = renderWithProviders(
    <WarehouseBins scope={scope} premise={premise} roles={["auditor"]} onClose={() => {}} />,
  );
  expect(await screen.findByText("TEST-BIN · TEST Private Bin")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Add bin" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Edit bin TEST-BIN" })).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "History for bin TEST-BIN" }));
  expect(await screen.findByRole("region", { name: "Bin revision history" })).toHaveTextContent(
    "Version 1",
  );
});
it("focuses the new form and retains the same ID and draft across failed saves", async () => {
  const fetcher = fixture();
  HTMLElement.prototype.scrollIntoView = vi.fn();
  const { user } = renderWithProviders(
    <WarehouseBins scope={scope} premise={premise} roles={["administrator"]} onClose={() => {}} />,
  );
  await user.click(await screen.findByRole("button", { name: "Add bin" }));
  await waitFor(() => expect(screen.getByLabelText("Bin code")).toHaveFocus());
  await user.type(screen.getByLabelText("Bin code"), "test-bin-new");
  await user.type(screen.getByLabelText("Bin name"), "TEST New Bin");
  expect(screen.getByRole("combobox", { name: "Storage class" })).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Save bin" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("TEST service unavailable");
  expect(screen.getByLabelText("Bin name")).toHaveValue("TEST New Bin");
  await user.click(screen.getByRole("button", { name: "Save bin" }));
  await waitFor(() =>
    expect(fetcher.mock.calls.filter(([, init]) => init?.method === "PUT")).toHaveLength(2),
  );
  const writes = fetcher.mock.calls.filter(([, init]) => init?.method === "PUT");
  expect(writes[0][0]).toBe(writes[1][0]);
  expect(JSON.parse(writes[0][1]?.body as string)).toMatchObject({
    expected_version: 0,
    code: "TEST-BIN-NEW",
    name: "TEST New Bin",
  });
  expect(JSON.parse(writes[0][1]?.body as string)).not.toHaveProperty("tenant_id");
});
it("hides previously loaded rows and forms if refresh fails after a save", async () => {
  const initial = fixture();
  let failed = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "PUT") {
        failed = true;
        return new Response(JSON.stringify({ ...bin, version: 2 }));
      }
      if (url.includes("/bins"))
        return failed
          ? new Response(JSON.stringify({ error: "forbidden", message: "TEST access revoked" }), {
              status: 403,
            })
          : new Response(JSON.stringify(page([bin])));
      return initial(url, init);
    }),
  );
  const { user } = renderWithProviders(
    <WarehouseBins scope={scope} premise={premise} roles={["administrator"]} onClose={() => {}} />,
  );
  await user.click(await screen.findByRole("button", { name: "Edit bin TEST-BIN" }));
  await user.click(screen.getByRole("button", { name: "Save bin" }));
  expect(
    await screen.findByText(
      "Could not load warehouse bins. Refresh access or contact your administrator.",
    ),
  ).toBeInTheDocument();
  expect(screen.queryByText("TEST-BIN · TEST Private Bin")).not.toBeInTheDocument();
  expect(screen.queryByRole("form", { name: "Storage bin form" })).not.toBeInTheDocument();
});
