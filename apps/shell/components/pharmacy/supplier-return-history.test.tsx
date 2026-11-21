import { screen, waitFor } from "@testing-library/react";
import { renderWithProviders as renderBase } from "@helivanta/api/testing";
import { NavigationGuardProvider } from "@helivanta/ui";
import type { ReactElement } from "react";
const renderWithProviders = (ui: ReactElement) =>
  renderBase(<NavigationGuardProvider>{ui}</NavigationGuardProvider>);
import { SupplierReturnHistory } from "./supplier-return-history";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  usePathname: () => "/pharmacy/accounts",
  useSearchParams: () => new URLSearchParams(),
}));
const scope = {
  tenantId: "0199e000-0000-7000-8000-000000000001",
  pharmacyId: "0199e000-0000-7000-8000-000000000002",
  outletId: "0199e000-0000-7000-8000-000000000003",
  subject: "TEST-owner",
};
const id = "0199e000-0000-7000-8000-000000000004";
const row = {
  id,
  outlet_id: scope.outletId,
  number: 17,
  payee_name: "TEST Supplier",
  document_reference: "TEST-INVOICE",
  currency: "INR",
  expected_credit_minor: 1500,
  line_count: 1,
  prepared_at: "2026-11-21T00:00:00Z",
  state: "draft",
  version: 0,
};
const detail = {
  ...row,
  line_count: undefined,
  payable_id: "0199e000-0000-7000-8000-000000000005",
  payee_reference: "TEST-SUP",
  reason: "TEST expired stock",
  prepared_by: "TEST-subject-never-display",
  lines: [
    {
      id: "0199e000-0000-7000-8000-000000000006",
      receipt_id: "0199e000-0000-7000-8000-000000000007",
      quarantine_location_id: "0199e000-0000-7000-8000-000000000008",
      batch_id: "0199e000-0000-7000-8000-000000000009",
      product_id: "0199e000-0000-7000-8000-00000000000a",
      product_version: 1,
      product_name: "TEST Medicine",
      generic_name: "TEST Generic",
      unit_code: "capsule",
      lot_number: "TEST-LOT",
      expires_on: "2026-11-01",
      receipt_units: 80,
      source_reference: "TEST-source",
      source_created_at: "2026-11-01T00:00:00Z",
      quantity_units: 4,
      expected_credit_minor: 1500,
    },
  ],
};
afterEach(() => {
  vi.unstubAllGlobals();
  sessionStorage.clear();
});
it("shows readable supplier return and batch evidence without backend identities", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async (url: string) =>
        new Response(
          JSON.stringify(
            url.includes("?limit=")
              ? { data: [row], page: { has_more: false, next_cursor: null } }
              : detail,
          ),
        ),
    ),
  );
  const { user, container } = renderWithProviders(
    <SupplierReturnHistory scope={scope} canCancel={false} timeZone="Asia/Kolkata" />,
  );
  await user.click(await screen.findByRole("button", { name: "Open return 17" }));
  expect(await screen.findByText("TEST Medicine")).toBeInTheDocument();
  expect(screen.getByText("TEST-LOT")).toBeInTheDocument();
  expect(
    screen.getByText("Expected credit is not a supplier credit or bill settlement."),
  ).toBeInTheDocument();
  expect(container.textContent).not.toContain(id);
  expect(container.textContent).not.toContain("TEST-subject-never-display");
  expect(screen.queryByRole("button", { name: "Cancel draft" })).not.toBeInTheDocument();
});
it("hides stale history when access fails and allows retry", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async () =>
        new Response(JSON.stringify({ error: "authz_unavailable", message: "TEST unavailable" }), {
          status: 503,
        }),
    ),
  );
  renderWithProviders(
    <SupplierReturnHistory scope={scope} canCancel={false} timeZone="Asia/Kolkata" />,
  );
  expect(await screen.findByRole("alert")).toHaveTextContent(/history|unavailable/i);
  expect(screen.queryByRole("button", { name: "Open return 17" })).not.toBeInTheDocument();
});

it("recovers the original cancellation after response loss without another write", async () => {
  let cancelled = false;
  const writes: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init: RequestInit) => {
      if (url.includes("/cancellations/")) {
        if (init?.method === "PUT") {
          writes.push(url);
          cancelled = true;
          throw new Error("TEST response lost");
        }
        return new Response(
          JSON.stringify({
            id: url.split("/").at(-1),
            return_id: id,
            outlet_id: scope.outletId,
            expected_version: 0,
            version: 1,
            state: "cancelled",
            reason: "TEST private cancellation reason",
            cancelled_by: scope.subject,
            cancelled_at: "2026-11-21T01:00:00Z",
          }),
        );
      }
      return new Response(
        JSON.stringify(
          url.includes("?limit=")
            ? {
                data: [{ ...row, ...(cancelled ? { state: "cancelled", version: 1 } : {}) }],
                page: { has_more: false, next_cursor: null },
              }
            : { ...detail, ...(cancelled ? { state: "cancelled", version: 1 } : {}) },
        ),
      );
    }),
  );
  const { user, unmount } = renderWithProviders(
    <SupplierReturnHistory scope={scope} canCancel timeZone="Asia/Kolkata" />,
  );
  await user.click(await screen.findByRole("button", { name: "Open return 17" }));
  await user.type(
    await screen.findByLabelText("Cancellation reason"),
    "TEST private cancellation reason",
  );
  await user.click(screen.getByRole("button", { name: "Cancel draft" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("TEST response lost");
  expect(writes).toHaveLength(1);
  expect(sessionStorage.length).toBe(1);
  expect(sessionStorage.getItem(sessionStorage.key(0)!)).not.toContain("private cancellation");
  unmount();
  const restored = renderWithProviders(
    <SupplierReturnHistory scope={scope} canCancel={false} timeZone="Asia/Kolkata" />,
  );
  await restored.user.click(await screen.findByRole("button", { name: "Open return 17" }));
  await restored.user.click(
    await screen.findByRole("button", { name: "Check cancellation result" }),
  );
  await screen.findByText("Original bill TEST-INVOICE · Cancelled");
  expect(writes).toHaveLength(1);
  await waitFor(() => expect(sessionStorage.length).toBe(0));
});

it("does not send cancellation if its recovery reference cannot be retained", async () => {
  const fetcher = vi.fn(
    async (url: string) =>
      new Response(
        JSON.stringify(
          url.includes("?limit=")
            ? { data: [row], page: { has_more: false, next_cursor: null } }
            : detail,
        ),
      ),
  );
  vi.stubGlobal("fetch", fetcher);
  const { user } = renderWithProviders(
    <SupplierReturnHistory scope={scope} canCancel timeZone="Asia/Kolkata" />,
  );
  await user.click(await screen.findByRole("button", { name: "Open return 17" }));
  await user.type(await screen.findByLabelText("Cancellation reason"), "TEST cancellation");
  const fail = vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
    throw new Error("TEST storage failure");
  });
  try {
    await user.click(screen.getByRole("button", { name: "Cancel draft" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("No cancellation was sent");
    expect(fetcher.mock.calls.some(([url]) => url.includes("/cancellations/"))).toBe(false);
  } finally {
    fail.mockRestore();
  }
});

it("retries a confirmed missing cancellation with its original command reference", async () => {
  const writes: string[] = [];
  let done = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init: RequestInit) => {
      if (url.includes("/cancellations/")) {
        if (init?.method !== "PUT")
          return new Response(
            JSON.stringify({ error: "not_found", message: "TEST not recorded" }),
            { status: 404 },
          );
        writes.push(url);
        if (writes.length === 1) throw new Error("TEST connection lost before commit");
        done = true;
        return new Response(
          JSON.stringify({
            id: url.split("/").at(-1),
            return_id: id,
            outlet_id: scope.outletId,
            expected_version: 0,
            version: 1,
            state: "cancelled",
            reason: "TEST cancellation",
            cancelled_by: scope.subject,
            cancelled_at: "2026-11-21T01:00:00Z",
          }),
        );
      }
      return new Response(
        JSON.stringify(
          url.includes("?limit=")
            ? {
                data: [{ ...row, ...(done ? { state: "cancelled", version: 1 } : {}) }],
                page: { has_more: false, next_cursor: null },
              }
            : { ...detail, ...(done ? { state: "cancelled", version: 1 } : {}) },
        ),
      );
    }),
  );
  const { user } = renderWithProviders(
    <SupplierReturnHistory scope={scope} canCancel timeZone="Asia/Kolkata" />,
  );
  await user.click(await screen.findByRole("button", { name: "Open return 17" }));
  await user.type(await screen.findByLabelText("Cancellation reason"), "TEST cancellation");
  await user.click(screen.getByRole("button", { name: "Cancel draft" }));
  await user.click(await screen.findByRole("button", { name: "Check cancellation result" }));
  await user.click(await screen.findByRole("button", { name: "Retry original cancellation" }));
  await waitFor(() => expect(sessionStorage.length).toBe(0));
  expect(writes).toHaveLength(2);
  expect(writes[1]).toBe(writes[0]);
});

it("labels a physically dispatched return without offering draft cancellation", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async (url: string) =>
        new Response(
          JSON.stringify(
            url.includes("?limit=")
              ? {
                  data: [{ ...row, state: "dispatched", version: 1 }],
                  page: { has_more: false, next_cursor: null },
                }
              : { ...detail, state: "dispatched", version: 1 },
          ),
        ),
    ),
  );
  const { user } = renderWithProviders(
    <SupplierReturnHistory scope={scope} canCancel={true} timeZone="Asia/Kolkata" />,
  );
  expect(await screen.findByText("Dispatched")).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Open return 17" }));
  expect(await screen.findByText(/Original bill TEST-INVOICE · Dispatched/)).toBeVisible();
  expect(screen.queryByRole("button", { name: "Cancel draft" })).not.toBeInTheDocument();
});
