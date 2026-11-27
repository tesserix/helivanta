import { afterEach, expect, it, vi } from "vitest";
import { fireEvent, screen, within } from "@testing-library/react";
import { renderWithProviders as renderBase } from "@helivanta/api/testing";
import { NavigationGuardProvider } from "@helivanta/ui";
import type { ReactElement } from "react";
import type { PharmacySupplierCreditContext } from "@helivanta/api";
import { SupplierCreditApplications } from "./supplier-credit-applications";

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
const bill = {
  id: "0199e000-0000-7000-8000-000000000006",
  outlet_id: scope.outletId,
  payee_name: "TEST Supplier",
  document_reference: "TEST-FUTURE-BILL",
  issued_on: "2026-11-17",
  due_on: "2026-11-30",
  currency: "INR",
  total_minor: 1180,
  paid_minor: 0,
  credit_minor: 0,
  outstanding_minor: 1180,
  version: 1,
};
const allocation = {
  id: "0199e000-0000-7000-8000-000000000007",
  credit_id: credit.id,
  outlet_id: scope.outletId,
  payable_id: bill.id,
  expected_version: 1,
  expected_available_minor: 750,
  expected_payable_version: 1,
  expected_outstanding_minor: 1180,
  amount_minor: 500,
  occurred_on: "2026-11-20",
  reason: "TEST apply retained actual credit",
  version: 2,
  payable_version: 2,
  available_minor: 250,
  outstanding_minor: 680,
  payable_reference: bill.document_reference,
  credit_document_reference: credit.document_reference,
  payee_name: credit.payee_name,
  recorded_by: scope.subject,
  recorded_at: "2026-11-21T00:00:00Z",
  reversal_id: null,
};
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  sessionStorage.clear();
});
it("applies explicit unused actual credit to a named eligible bill with both current balances", async () => {
  const posted: Record<string, unknown>[] = [];
  const rows: (typeof allocation)[] = [];
  const fetcher = vi.fn(async (url: string, options?: RequestInit) => {
    if (options?.method === "PUT") {
      const input = JSON.parse(options.body as string);
      posted.push(input);
      const row = { ...allocation, ...input, id: url.split("/").at(-1)! };
      rows.push(row);
      return new Response(JSON.stringify({ ...row, reversal_id: undefined }));
    }
    return new Response(
      JSON.stringify({
        data: url.includes("eligible-payables") ? [bill] : rows,
        page: { has_more: false, next_cursor: null },
      }),
    );
  });
  vi.stubGlobal("fetch", fetcher);
  const { user } = render(<SupplierCreditApplications scope={scope} credit={credit} canManage />);
  await user.click(await screen.findByRole("button", { name: "Apply credit to bill" }));
  await user.click(await screen.findByRole("combobox", { name: "Eligible supplier bill" }));
  await user.click(await screen.findByRole("option", { name: /TEST-FUTURE-BILL.*TEST Supplier/ }));
  expect(screen.getByLabelText("Amount to apply (INR)")).toHaveValue("");
  await user.type(screen.getByLabelText("Amount to apply (INR)"), "5.00");
  fireEvent.change(screen.getByLabelText("Application date"), { target: { value: "2026-11-20" } });
  await user.type(screen.getByLabelText("Application reason"), "TEST apply retained actual credit");
  await user.click(screen.getByRole("button", { name: "Save bill application" }));
  expect(await screen.findByText(/Bill application verified/)).toBeInTheDocument();
  expect(posted).toEqual([
    {
      payable_id: bill.id,
      expected_version: 1,
      expected_available_minor: 750,
      expected_payable_version: 1,
      expected_outstanding_minor: 1180,
      amount_minor: 500,
      occurred_on: "2026-11-20",
      reason: "TEST apply retained actual credit",
    },
  ]);
  const history = within(
    await screen.findByRole("table", { name: "Supplier credit bill applications" }),
  );
  expect(history.getByText("TEST-FUTURE-BILL")).toBeInTheDocument();
  expect(history.getByText("INR 5.00")).toBeInTheDocument();
  expect(sessionStorage.length).toBe(0);
  for (const value of [credit.id, bill.id, scope.subject])
    expect(document.body).not.toHaveTextContent(value);
  expect(
    fetcher.mock.calls.some(([url]) => url.includes("/matches") || url.includes("/stock")),
  ).toBe(false);
});

async function enterApplication(user: ReturnType<typeof render>["user"], amount = "5.00") {
  await user.click(await screen.findByRole("button", { name: "Apply credit to bill" }));
  await user.click(await screen.findByRole("combobox", { name: "Eligible supplier bill" }));
  await user.click(await screen.findByRole("option", { name: /TEST-FUTURE-BILL.*TEST Supplier/ }));
  await user.type(screen.getByLabelText("Amount to apply (INR)"), amount);
  fireEvent.change(screen.getByLabelText("Application date"), { target: { value: "2026-11-20" } });
  await user.type(screen.getByLabelText("Application reason"), "TEST apply retained actual credit");
}
it("recovers the original application after reload without new write authority or eligible bill lookup", async () => {
  let committed: typeof allocation | null = null;
  const writes: string[] = [];
  const fetcher = vi.fn(async (url: string, options?: RequestInit) => {
    if (options?.method === "PUT") {
      writes.push(url);
      const input = JSON.parse(options.body as string);
      committed = {
        ...allocation,
        ...input,
        id: url.split("/").at(-1)!,
        payable_version: input.expected_payable_version + 1,
        available_minor: input.expected_available_minor - input.amount_minor,
        outstanding_minor: input.expected_outstanding_minor - input.amount_minor,
      };
      throw new TypeError("TEST committed application response lost");
    }
    if (url.endsWith("/recovery"))
      return new Response(JSON.stringify({ ...committed, reversal_id: undefined }));
    return new Response(
      JSON.stringify({
        data: url.includes("eligible-payables")
          ? [{ ...bill, paid_minor: 680, outstanding_minor: 500, version: 2 }]
          : committed
            ? [committed]
            : [],
        page: { has_more: false, next_cursor: null },
      }),
    );
  });
  vi.stubGlobal("fetch", fetcher);
  const first = render(<SupplierCreditApplications scope={scope} credit={credit} canManage />);
  await enterApplication(first.user);
  await first.user.click(screen.getByRole("button", { name: "Save bill application" }));
  expect(await screen.findByText(/Bill application outcome is unconfirmed/)).toBeInTheDocument();
  expect(Object.keys(JSON.parse(sessionStorage.getItem(sessionStorage.key(0)!)!))).toEqual(["id"]);
  await first.user.click(screen.getByRole("button", { name: "Close application form" }));
  await first.user.click(await screen.findByRole("button", { name: "Discard" }));
  expect(
    await screen.findByRole("button", { name: "Resume bill application recovery" }),
  ).toBeInTheDocument();
  await first.user.click(screen.getByRole("button", { name: "Resume bill application recovery" }));
  expect(
    await screen.findByRole("button", { name: "Check original bill application" }),
  ).toBeInTheDocument();
  first.unmount();
  fetcher.mockClear();
  const next = render(
    <SupplierCreditApplications
      scope={scope}
      credit={{ ...credit, version: 2, allocated_minor: 500, available_minor: 250 }}
      canManage={false}
    />,
  );
  await next.user.click(
    await screen.findByRole("button", { name: "Check original bill application" }),
  );
  expect(await screen.findByText(/Bill application verified/)).toBeInTheDocument();
  expect(sessionStorage.length).toBe(0);
  expect(writes).toHaveLength(1);
  expect(fetcher.mock.calls.some(([url]) => url.includes("eligible-payables"))).toBe(false);
  expect(screen.queryByRole("button", { name: "Apply credit to bill" })).not.toBeInTheDocument();
});

it("fully reverses an original application to a paid bill while retaining intervening payment", async () => {
  let reversal: Record<string, unknown> | null = null;
  const posted: Record<string, unknown>[] = [];
  const currentCredit = { ...credit, version: 2, allocated_minor: 500, available_minor: 250 };
  const fetcher = vi.fn(async (url: string, options?: RequestInit) => {
    if (options?.method === "PUT") {
      const input = JSON.parse(options.body as string);
      posted.push(input);
      reversal = {
        ...input,
        id: url.split("/").at(-1),
        outlet_id: scope.outletId,
        credit_id: credit.id,
        allocation_id: allocation.id,
        payable_id: bill.id,
        amount_minor: 500,
        version: 3,
        payable_version: 4,
        available_minor: 750,
        outstanding_minor: 500,
        payable_reference: bill.document_reference,
        credit_document_reference: credit.document_reference,
        payee_name: credit.payee_name,
        recorded_by: scope.subject,
        recorded_at: allocation.recorded_at,
      };
      return new Response(JSON.stringify(reversal));
    }
    if (url.endsWith("/reversal")) return new Response(JSON.stringify(reversal));
    if (url.endsWith("/context"))
      return new Response(
        JSON.stringify({
          credit: reversal
            ? { ...currentCredit, version: 3, allocated_minor: 0, available_minor: 750 }
            : currentCredit,
          allocation: { ...allocation, reversal_id: reversal?.id ?? null },
          payable: {
            ...bill,
            paid_minor: 680,
            credit_minor: reversal ? 0 : 500,
            outstanding_minor: reversal ? 500 : 0,
            version: reversal ? 4 : 3,
          },
        }),
      );
    return new Response(
      JSON.stringify({
        data: [{ ...allocation, reversal_id: reversal?.id ?? null }],
        page: { has_more: false, next_cursor: null },
      }),
    );
  });
  vi.stubGlobal("fetch", fetcher);
  const { user } = render(
    <SupplierCreditApplications scope={scope} credit={currentCredit} canManage />,
  );
  await user.click(
    await screen.findByRole("button", { name: "Review application for TEST-FUTURE-BILL" }),
  );
  expect(await screen.findByText(/Current bill outstanding: INR 0.00/)).toBeInTheDocument();
  expect(screen.getByText(/Payments retained on bill: INR 6.80/)).toBeInTheDocument();
  fireEvent.change(screen.getByLabelText("Application reversal date"), {
    target: { value: "2026-11-22" },
  });
  await user.type(
    screen.getByLabelText("Application reversal reason"),
    "TEST reverse application after payment",
  );
  await user.click(screen.getByRole("button", { name: "Reverse full bill application" }));
  expect(await screen.findByText(/Bill application reversal verified/)).toBeInTheDocument();
  expect(posted).toEqual([
    {
      expected_version: 2,
      expected_available_minor: 250,
      expected_payable_version: 3,
      expected_outstanding_minor: 0,
      occurred_on: "2026-11-22",
      reason: "TEST reverse application after payment",
    },
  ]);
  expect(await screen.findByText(/Current bill outstanding: INR 5.00/)).toBeInTheDocument();
  expect(screen.getByText(/Payments retained on bill: INR 6.80/)).toBeInTheDocument();
  expect(
    fetcher.mock.calls.some(
      ([url]) =>
        url.includes("eligible-payables") || url.includes("/payments") || url.includes("/stock"),
    ),
  ).toBe(false);
  expect(sessionStorage.length).toBe(0);
});

function applicationReceipt(url: string, options: RequestInit) {
  const input = JSON.parse(options.body as string);
  return {
    ...allocation,
    ...input,
    id: url.split("/").at(-1)!,
    version: input.expected_version + 1,
    payable_version: input.expected_payable_version + 1,
    available_minor: input.expected_available_minor - input.amount_minor,
    outstanding_minor: input.expected_outstanding_minor - input.amount_minor,
    reversal_id: undefined,
  };
}
it("can apply credit that is fully attributed to returns because matching does not spend it", async () => {
  const posted: Record<string, unknown>[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, options?: RequestInit) => {
      if (options?.method === "PUT") {
        posted.push(JSON.parse(options.body as string));
        return new Response(JSON.stringify(applicationReceipt(url, options)));
      }
      return new Response(
        JSON.stringify({
          data: url.includes("eligible-payables") ? [bill] : [],
          page: { has_more: false, next_cursor: null },
        }),
      );
    }),
  );
  const { user } = render(
    <SupplierCreditApplications
      scope={scope}
      credit={{ ...credit, version: 2, matched_minor: 750 }}
      canManage
    />,
  );
  await enterApplication(user);
  await user.click(screen.getByRole("button", { name: "Save bill application" }));
  expect(await screen.findByText(/Bill application verified/)).toBeInTheDocument();
  expect(posted[0]).toMatchObject({
    expected_version: 2,
    expected_available_minor: 750,
    amount_minor: 500,
  });
});
it.each([
  { amount: "8.00", date: "2026-11-20", outstanding: 1180 },
  { amount: "5.00", date: "2026-11-20", outstanding: 400 },
  { amount: "5.00", date: "2026-11-17", outstanding: 1180 },
  { amount: "5.00", date: "2026-11-23", outstanding: 1180 },
])(
  "refuses an application outside both budgets or the owner's date bounds: %j",
  async ({ amount, date, outstanding }) => {
    let writes = 0;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, options?: RequestInit) => {
        if (options?.method === "PUT") writes++;
        return new Response(
          JSON.stringify({
            data: url.includes("eligible-payables")
              ? [
                  {
                    ...bill,
                    paid_minor: bill.total_minor - outstanding,
                    outstanding_minor: outstanding,
                  },
                ]
              : [],
            page: { has_more: false, next_cursor: null },
          }),
        );
      }),
    );
    const { user } = render(<SupplierCreditApplications scope={scope} credit={credit} canManage />);
    await enterApplication(user, amount);
    fireEvent.change(screen.getByLabelText("Application date"), { target: { value: date } });
    await user.click(screen.getByRole("button", { name: "Save bill application" }));
    expect(await screen.findByRole("alert")).toBeInTheDocument();
    expect(writes).toBe(0);
    expect(sessionStorage.length).toBe(0);
  },
);
it("does not accept a recovered application with changed retained intent", async () => {
  let committed: ReturnType<typeof applicationReceipt> | null = null;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, options?: RequestInit) => {
      if (options?.method === "PUT") {
        committed = {
          ...applicationReceipt(url, options),
          reason: "TEST different commercial intent",
        };
        throw new TypeError("TEST lost response");
      }
      if (url.endsWith("/recovery")) return new Response(JSON.stringify(committed));
      return new Response(
        JSON.stringify({
          data: url.includes("eligible-payables") ? [bill] : [],
          page: { has_more: false, next_cursor: null },
        }),
      );
    }),
  );
  const { user } = render(<SupplierCreditApplications scope={scope} credit={credit} canManage />);
  await enterApplication(user);
  await user.click(screen.getByRole("button", { name: "Save bill application" }));
  await user.click(await screen.findByRole("button", { name: "Check original bill application" }));
  expect(await screen.findByText(/original result could not be verified/)).toBeInTheDocument();
  expect(screen.queryByText(/Bill application verified/)).not.toBeInTheDocument();
  expect(sessionStorage.length).toBe(1);
});

it("restores a committed paid-bill reversal from scoped references after reload with writes disabled", async () => {
  let reversal: Record<string, unknown> | null = null;
  const writes: string[] = [];
  const currentCredit = { ...credit, version: 2, allocated_minor: 500, available_minor: 250 };
  const fetcher = vi.fn(async (url: string, options?: RequestInit) => {
    if (options?.method === "PUT") {
      writes.push(url);
      const input = JSON.parse(options.body as string);
      reversal = {
        ...input,
        id: url.split("/").at(-1),
        outlet_id: scope.outletId,
        credit_id: credit.id,
        allocation_id: allocation.id,
        payable_id: bill.id,
        amount_minor: 500,
        version: 3,
        payable_version: 4,
        available_minor: 750,
        outstanding_minor: 500,
        payable_reference: bill.document_reference,
        credit_document_reference: credit.document_reference,
        payee_name: credit.payee_name,
        recorded_by: scope.subject,
        recorded_at: allocation.recorded_at,
      };
      throw new TypeError("TEST reversal committed but response lost");
    }
    if (url.endsWith("/reversal") || url.endsWith("/recovery"))
      return new Response(JSON.stringify(reversal));
    if (url.endsWith("/context"))
      return new Response(
        JSON.stringify({
          credit: reversal
            ? { ...currentCredit, version: 3, allocated_minor: 0, available_minor: 750 }
            : currentCredit,
          allocation: { ...allocation, reversal_id: reversal?.id ?? null },
          payable: {
            ...bill,
            paid_minor: 680,
            credit_minor: reversal ? 0 : 500,
            outstanding_minor: reversal ? 500 : 0,
            version: reversal ? 4 : 3,
          },
        }),
      );
    return new Response(
      JSON.stringify({
        data: [{ ...allocation, reversal_id: reversal?.id ?? null }],
        page: { has_more: false, next_cursor: null },
      }),
    );
  });
  vi.stubGlobal("fetch", fetcher);
  const first = render(
    <SupplierCreditApplications scope={scope} credit={currentCredit} canManage />,
  );
  await first.user.click(
    await screen.findByRole("button", { name: "Review application for TEST-FUTURE-BILL" }),
  );
  fireEvent.change(await screen.findByLabelText("Application reversal date"), {
    target: { value: "2026-11-22" },
  });
  await first.user.type(
    screen.getByLabelText("Application reversal reason"),
    "TEST retained paid-bill correction",
  );
  await first.user.click(screen.getByRole("button", { name: "Reverse full bill application" }));
  expect(
    await screen.findByText(/Bill application reversal outcome is unconfirmed/),
  ).toBeInTheDocument();
  expect(JSON.parse(sessionStorage.getItem(sessionStorage.key(0)!)!)).toEqual({
    id: writes[0]?.split("/").at(-1),
    target: allocation.id,
  });
  first.unmount();
  fetcher.mockClear();
  const next = render(
    <SupplierCreditApplications
      scope={scope}
      credit={{ ...credit, version: 3 }}
      canManage={false}
    />,
  );
  await next.user.click(
    await screen.findByRole("button", { name: "Check original bill application reversal" }),
  );
  expect(await screen.findByText(/Bill application reversal verified/)).toBeInTheDocument();
  expect(await screen.findByText("TEST retained paid-bill correction")).toBeInTheDocument();
  expect(writes).toHaveLength(1);
  expect(sessionStorage.length).toBe(0);
  expect(fetcher.mock.calls.some(([url]) => url.includes("eligible-payables"))).toBe(false);
  expect(
    screen.queryByRole("button", { name: "Reverse full bill application" }),
  ).not.toBeInTheDocument();
});
it("keeps retained application history readable with no new-write authority", async () => {
  const fetcher = vi.fn<typeof fetch>(
    async () =>
      new Response(
        JSON.stringify({ data: [allocation], page: { has_more: false, next_cursor: null } }),
      ),
  );
  vi.stubGlobal("fetch", fetcher);
  render(
    <SupplierCreditApplications
      scope={scope}
      credit={{ ...credit, version: 2, allocated_minor: 500, available_minor: 250 }}
      canManage={false}
    />,
  );
  const history = within(
    await screen.findByRole("table", { name: "Supplier credit bill applications" }),
  );
  expect(history.getByText("TEST-FUTURE-BILL")).toBeInTheDocument();
  expect(history.getByText("INR 5.00")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Apply credit to bill" })).not.toBeInTheDocument();
  expect(fetcher.mock.calls.some(([url]) => String(url).includes("eligible-payables"))).toBe(false);
});
it("refuses an overlong application reason inline without sending a write", async () => {
  let writes = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, options?: RequestInit) => {
      if (options?.method === "PUT") writes++;
      return new Response(
        JSON.stringify({
          data: url.includes("eligible-payables") ? [bill] : [],
          page: { has_more: false, next_cursor: null },
        }),
      );
    }),
  );
  const { user } = render(<SupplierCreditApplications scope={scope} credit={credit} canManage />);
  await enterApplication(user);
  fireEvent.change(screen.getByLabelText("Application reason"), {
    target: { value: "x".repeat(501) },
  });
  await user.click(screen.getByRole("button", { name: "Save bill application" }));
  expect(await screen.findByText("Use at most 500 characters.")).toBeVisible();
  expect(screen.getByLabelText("Application reason")).toHaveValue("x".repeat(501));
  expect(writes).toBe(0);
});
it("refuses an overlong application reversal reason inline without sending a write", async () => {
  let writes = 0;
  const currentCredit = { ...credit, version: 2, allocated_minor: 500, available_minor: 250 };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, options?: RequestInit) => {
      if (options?.method === "PUT") writes++;
      if (url.endsWith("/reversal")) return new Response("{}", { status: 404 });
      if (url.endsWith("/context"))
        return new Response(
          JSON.stringify({
            credit: currentCredit,
            allocation,
            payable: {
              ...bill,
              paid_minor: 680,
              credit_minor: 500,
              outstanding_minor: 0,
              version: 3,
            },
          }),
        );
      return new Response(
        JSON.stringify({ data: [allocation], page: { has_more: false, next_cursor: null } }),
      );
    }),
  );
  const { user } = render(
    <SupplierCreditApplications scope={scope} credit={currentCredit} canManage />,
  );
  await user.click(
    await screen.findByRole("button", { name: "Review application for TEST-FUTURE-BILL" }),
  );
  await screen.findByText(/Current bill outstanding: INR 0.00/);
  fireEvent.change(screen.getByLabelText("Application reversal date"), {
    target: { value: "2026-11-22" },
  });
  fireEvent.change(screen.getByLabelText("Application reversal reason"), {
    target: { value: "x".repeat(501) },
  });
  await user.click(screen.getByRole("button", { name: "Reverse full bill application" }));
  expect(await screen.findByText("Use at most 500 characters.")).toBeVisible();
  expect(writes).toBe(0);
});
