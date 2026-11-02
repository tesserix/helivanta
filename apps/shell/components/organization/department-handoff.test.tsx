import { screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { NavigationGuardProvider } from "@helivanta/ui";
import { DepartmentHandoff } from "./department-handoff";

const hospital = {
  id: "22222222-2222-4222-8222-222222222222",
  name: "Synthetic Hospital",
  access_level: "manager" as const,
  created_at: "2026-11-02T00:00:00Z",
};
const master = {
  id: "33333333-3333-4333-8333-333333333333",
  revision_id: "44444444-4444-4444-8444-444444444444",
  kind: "department" as const,
  code: "SYN-PHARMA",
  name: "Synthetic HR Pharmacy",
  description: "",
  active: true,
  version: 1,
  created_at: hospital.created_at,
  updated_at: hospital.created_at,
};
const department = {
  id: "019bbbbb-bbbb-7bbb-8bbb-bbbbbbbbbbbb",
  hospital_id: hospital.id,
  revision_id: "019ccccc-cccc-7ccc-8ccc-cccccccccccc",
  version: 4,
  code: "PHARMA",
  name: "Synthetic Structural Pharmacy",
  kind: "department",
  status: "active",
  effective_at: hospital.created_at,
  created_at: hospital.created_at,
};
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());
function fixture({ fail = false, allowed = true, reader = false, lostReply = false } = {}) {
  const writes: Array<{ url: string; body: Record<string, unknown> }> = [];
  let receipt: Record<string, unknown> | undefined;
  const fetcher = vi.fn(async (path: string, init?: RequestInit) => {
    if (path.includes("/iam/me/permissions"))
      return Response.json({
        tenant_id: "11111111-1111-4111-8111-111111111111",
        subject: "synthetic-manager",
        data: allowed
          ? ["workforce.master.manage", ...(reader ? [] : ["organization.structure.read"])]
          : [],
      });
    if (path.includes("/organization/")) return Response.json(page([department]));
    if (init?.method === "PUT") {
      const body = JSON.parse(String(init.body));
      writes.push({ url: path, body });
      if (fail)
        return Response.json(
          { error: "conflict", message: "Synthetic stale source" },
          { status: 409 },
        );
      receipt = {
        request_id: path.split("/").at(-1),
        hospital_id: hospital.id,
        master_id: master.id,
        master_revision_id: master.revision_id,
        department_id: department.id,
        department_revision_id: department.revision_id,
        version: 1,
        status: "pending",
        created_at: hospital.created_at,
      };
      if (lostReply && writes.length === 1) {
        receipt.status = "matched";
        receipt.observed_at = hospital.created_at;
        throw new Error("Synthetic ambiguous connection failure after commit");
      }
      return Response.json(receipt);
    }
    return Response.json(page(receipt ? [receipt] : []));
  });
  vi.stubGlobal("fetch", fetcher);
  return { writes, fetcher };
}
it("requires a reason and retains an unchanged command for retry after conflict", async () => {
  const { writes } = fixture({ fail: true });
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <DepartmentHandoff hospital={hospital} master={master} onClose={() => {}} />
    </NavigationGuardProvider>,
  );
  await screen.findByRole("combobox", { name: "Canonical department or sub-unit" });
  await user.click(await screen.findByRole("combobox", {name:"Canonical department or sub-unit"}));
  await user.click(screen.getByRole("option", {name:/Synthetic Structural Pharmacy/}));
  await user.click(screen.getByRole("button", { name: "Record department mapping" }));
  expect(writes).toHaveLength(0);
  await user.type(screen.getByLabelText("Mapping reason"), "Synthetic explicit reconciliation");
  await user.click(screen.getByRole("button", { name: "Record department mapping" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("could not be recorded");
  await user.click(screen.getByRole("button", { name: "Record department mapping" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[0]).toEqual(writes[1]);
  expect(writes[0]?.body).toMatchObject({
    expected_version: 0,
    master_revision_id: master.revision_id,
    department_id: department.id,
    department_revision_id: department.revision_id,
  });
});
it("shows pending snapshot evidence without claiming Staff posting or authority", async () => {
  fixture();
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <DepartmentHandoff hospital={hospital} master={master} onClose={() => {}} />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("combobox", {name:"Canonical department or sub-unit"}));
  await user.click(screen.getByRole("option", {name:/Synthetic Structural Pharmacy/}));
  await user.type(screen.getByLabelText("Mapping reason"), "Synthetic explicit reconciliation");
  await user.click(screen.getByRole("button", { name: "Record department mapping" }));
  expect(await screen.findByText(/Mapping 1: Owner observation pending/)).toBeInTheDocument();
  expect(screen.getByText(/does not assign Staff or grant access/)).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Retry pending observation" })).toBeEnabled();
});
it("requires current permission and does not fetch owner data when it is absent", async () => {
  const { fetcher } = fixture({ allowed: false });
  renderWithProviders(
    <NavigationGuardProvider>
      <DepartmentHandoff hospital={hospital} master={master} onClose={() => {}} />
    </NavigationGuardProvider>,
  );
  expect(await screen.findByRole("alert")).toHaveTextContent("Department mapping access required");
  expect(fetcher.mock.calls.some(([path]) => !path.includes("/iam/me/permissions"))).toBe(false);
});

it("retries the original command after an ambiguous write even when refreshed history shows its commit", async () => {
  const { writes } = fixture({ lostReply: true });
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <DepartmentHandoff hospital={hospital} master={master} onClose={() => {}} />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("combobox", {name:"Canonical department or sub-unit"}));
  await user.click(screen.getByRole("option", {name:/Synthetic Structural Pharmacy/}));
  await user.type(screen.getByLabelText("Mapping reason"), "Synthetic ambiguous retry");
  await user.click(screen.getByRole("button", { name: "Record department mapping" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("could not be recorded");
  await user.click(screen.getByRole("button", { name: "Refresh mapping history" }));
  await screen.findByText(/Mapping 1: Matched snapshot/);
  await user.click(screen.getByRole("button", { name: "Record department mapping" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[1]).toEqual(writes[0]);
});
