import { afterEach, expect, it, vi } from "vitest";
import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderWithProviders as renderBase } from "@helivanta/api/testing";
import { NavigationGuardProvider } from "@helivanta/ui";
import type { ReactElement } from "react";
import type { PharmacySupplierCreditContext } from "@helivanta/api";
import { SupplierCreditVoid } from "./supplier-credit-void";
const render = (ui: ReactElement) =>
  renderBase(<NavigationGuardProvider>{ui}</NavigationGuardProvider>);
const scope = {
  tenantId: "0199e000-0000-7000-8000-000000000001",
  pharmacyId: "0199e000-0000-7000-8000-000000000002",
  outletId: "0199e000-0000-7000-8000-000000000003",
  subject: "TEST-finance",
};
const credit: PharmacySupplierCreditContext = {
  id: "0199e000-0000-7000-8000-000000000004",
  outlet_id: scope.outletId,
  return_id: "0199e000-0000-7000-8000-000000000005",
  return_number: 17,
  payee_reference: "TEST-SUPPLIER",
  payee_name: "TEST Supplier",
  original_bill_reference: "TEST-PAID-ORIGINAL-BILL",
  document_reference: "TEST-CN-ONE",
  issued_on: "2026-11-18",
  amount_minor: 750,
  currency: "INR",
  reason: "TEST actual credit after paid source bill",
  correction_of: null,
  recorded_by: "TEST-owner-never-display",
  recorded_at: "2026-11-19T00:00:00Z",
  version: 1,
  state: "active",
  matched_minor: 0,
  allocated_minor: 0,
  available_minor: 750,
  local_date: "2026-11-22",
  as_of: "2026-11-21T20:00:00Z",
  timezone: "Asia/Kolkata",
};

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  sessionStorage.clear();
});
const receipt = {
  id: "0199e000-0000-7000-8000-000000000009",
  outlet_id: scope.outletId,
  credit_id: credit.id,
  expected_version: 1,
  version: 2,
  reason: "TEST supplier note incorrectly recorded",
  recorded_by: scope.subject,
  recorded_at: "2026-11-22T00:00:00Z",
};
it("voids only unused unmatched credit using explicit reason and the current version", async () => {
  const fetcher = vi.fn(async (_url: string, options?: RequestInit) => {
    const input = JSON.parse(options?.body as string);
    expect(input).toEqual({ expected_version: 1, reason: receipt.reason });
    return new Response(JSON.stringify({ ...receipt, ...input, id: _url.split("/").at(-1) }));
  });
  vi.stubGlobal("fetch", fetcher);
  const { user } = render(<SupplierCreditVoid scope={scope} credit={credit} canManage />);
  await user.click(await screen.findByRole("button", { name: "Void unused credit" }));
  await user.type(screen.getByLabelText("Void reason"), receipt.reason);
  await user.click(screen.getByRole("button", { name: "Confirm credit void" }));
  expect(await screen.findByText(/Supplier credit void verified/)).toBeInTheDocument();
  expect(fetcher).toHaveBeenCalledTimes(1);
  expect(sessionStorage.length).toBe(0);
  expect(document.body).not.toHaveTextContent(credit.id);
});
it.each([
  ["matched", { matched_minor: 1 }],
  ["applied", { allocated_minor: 1, available_minor: 749 }],
  ["writes disabled", {}],
])("does not offer a new void when %s", async (_label, patch) => {
  const fetcher = vi.fn();
  vi.stubGlobal("fetch", fetcher);
  render(
    <SupplierCreditVoid
      scope={scope}
      credit={{ ...credit, ...patch }}
      canManage={_label !== "writes disabled"}
    />,
  );
  expect(screen.queryByRole("button", { name: "Void unused credit" })).not.toBeInTheDocument();
  expect(fetcher).not.toHaveBeenCalled();
});
it("recovers the same void after lost committed response and reload with writes disabled", async () => {
  let committed: typeof receipt | null = null;
  let writes = 0;
  const fetcher = vi.fn(async (url: string, options?: RequestInit) => {
    if (options?.method === "PUT") {
      writes++;
      committed = { ...receipt, id: url.split("/").at(-1)! };
      throw new TypeError("TEST lost committed response");
    }
    return new Response(JSON.stringify(committed));
  });
  vi.stubGlobal("fetch", fetcher);
  const first = render(<SupplierCreditVoid scope={scope} credit={credit} canManage />);
  await first.user.click(await screen.findByRole("button", { name: "Void unused credit" }));
  await first.user.type(screen.getByLabelText("Void reason"), receipt.reason);
  await first.user.click(screen.getByRole("button", { name: "Confirm credit void" }));
  expect(
    await screen.findByText(/Supplier credit void outcome is unconfirmed/),
  ).toBeInTheDocument();
  expect(sessionStorage.length).toBe(1);
  expect(JSON.parse(sessionStorage.getItem(sessionStorage.key(0)!)!)).toEqual({
    id: committed!.id,
  });
  first.unmount();
  const second = render(
    <SupplierCreditVoid
      scope={scope}
      credit={{ ...credit, state: "voided", version: 2, available_minor: 0 }}
      canManage={false}
    />,
  );
  await second.user.click(
    await screen.findByRole("button", { name: "Check original credit void" }),
  );
  expect(await screen.findByText(/Supplier credit void verified/)).toBeInTheDocument();
  await waitFor(() => expect(screen.getByText(receipt.reason)).toBeInTheDocument());
  expect(writes).toBe(1);
  expect(sessionStorage.length).toBe(0);
  expect(screen.queryByRole("button", { name: "Confirm credit void" })).not.toBeInTheDocument();
});

it("keeps the explicit reason after stale refusal and does not silently resubmit", async () => {
  const fetcher = vi.fn(
    async () =>
      new Response(JSON.stringify({ error: { code: "conflict", message: "TEST stale" } }), {
        status: 409,
      }),
  );
  vi.stubGlobal("fetch", fetcher);
  const { user } = render(<SupplierCreditVoid scope={scope} credit={credit} canManage />);
  await user.click(await screen.findByRole("button", { name: "Void unused credit" }));
  await user.type(screen.getByLabelText("Void reason"), receipt.reason);
  await user.click(screen.getByRole("button", { name: "Confirm credit void" }));
  expect(await screen.findByText(/Supplier credit void was refused/)).toBeInTheDocument();
  expect(screen.getByLabelText("Void reason")).toHaveValue(receipt.reason);
  expect(sessionStorage.length).toBe(0);
  expect(fetcher).toHaveBeenCalledTimes(1);
});
it("hides cached void reason and recorder after a refreshed read is refused", async () => {
  let denied = false;
  const fetcher = vi.fn(async () =>
    denied
      ? new Response(JSON.stringify({ error: { code: "forbidden", message: "TEST denied" } }), {
          status: 403,
        })
      : new Response(JSON.stringify({ ...receipt, recorded_by: "TEST-private-actor" })),
  );
  vi.stubGlobal("fetch", fetcher);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <SupplierCreditVoid
        scope={scope}
        credit={{ ...credit, state: "voided", version: 2, available_minor: 0 }}
        canManage={false}
      />
    </QueryClientProvider>,
  );
  expect(await screen.findByText(receipt.reason)).toBeInTheDocument();
  denied = true;
  await act(() => client.invalidateQueries());
  expect(await screen.findByRole("alert")).toHaveTextContent(/don't have access/);
  expect(screen.queryByText(receipt.reason)).not.toBeInTheDocument();
  expect(document.body).not.toHaveTextContent("TEST-private-actor");
});
it("prevents void submission when the recovery reference cannot be stored", async () => {
  const fetcher = vi.fn();
  vi.stubGlobal("fetch", fetcher);
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
    throw new Error("TEST full storage");
  });
  const { user } = render(<SupplierCreditVoid scope={scope} credit={credit} canManage />);
  await user.click(await screen.findByRole("button", { name: "Void unused credit" }));
  await user.type(screen.getByLabelText("Void reason"), receipt.reason);
  await user.click(screen.getByRole("button", { name: "Confirm credit void" }));
  expect(await screen.findByText(/No financial command has been submitted/)).toBeInTheDocument();
  expect(fetcher).not.toHaveBeenCalled();
});
it("refuses an overlong void reason inline without sending a request", async () => {
  const fetcher = vi.fn();
  vi.stubGlobal("fetch", fetcher);
  const { user } = render(<SupplierCreditVoid scope={scope} credit={credit} canManage />);
  await user.click(await screen.findByRole("button", { name: "Void unused credit" }));
  fireEvent.change(screen.getByLabelText("Void reason"), { target: { value: "x".repeat(501) } });
  await user.click(screen.getByRole("button", { name: "Confirm credit void" }));
  expect(await screen.findByText("Use at most 500 characters.")).toBeVisible();
  expect(screen.getByLabelText("Void reason")).toHaveValue("x".repeat(501));
  expect(fetcher).not.toHaveBeenCalled();
});
