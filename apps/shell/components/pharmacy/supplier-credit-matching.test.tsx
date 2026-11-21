import { afterEach, expect, it, vi } from "vitest";
import { screen, within } from "@testing-library/react";
import { renderWithProviders as renderBase } from "@helivanta/api/testing";
import { NavigationGuardProvider } from "@helivanta/ui";
import type { ReactElement } from "react";
const render = (ui: ReactElement) =>
  renderBase(<NavigationGuardProvider>{ui}</NavigationGuardProvider>);
import { SupplierCreditMatching } from "./supplier-credit-matching";

const scope = {
  tenantId: "0199e000-0000-7000-8000-000000000001",
  pharmacyId: "0199e000-0000-7000-8000-000000000002",
  outletId: "0199e000-0000-7000-8000-000000000003",
  subject: "TEST-finance",
};
const source = "0199e000-0000-7000-8000-000000000005";
const credit = {
  id: "0199e000-0000-7000-8000-000000000004",
  outlet_id: scope.outletId,
  return_id: source,
  return_number: 17,
  payee_reference: "TEST-SUPPLIER",
  payee_name: "TEST Supplier",
  original_bill_reference: "TEST-ORIGINAL-BILL",
  document_reference: "TEST-CN-ONE",
  issued_on: "2026-11-18",
  amount_minor: 750,
  currency: "INR" as const,
  reason: "TEST actual note",
  correction_of: null,
  recorded_by: "TEST-subject-never-display",
  recorded_at: "2026-11-19T00:00:00Z",
  version: 2,
  state: "active" as const,
  matched_minor: 500,
  allocated_minor: 0,
  available_minor: 750,
  local_date: "2026-11-22",
  as_of: "2026-11-21T20:00:00Z",
  timezone: "Asia/Kolkata",
};
const match = {
  id: "0199e000-0000-7000-8000-000000000006",
  credit_id: credit.id,
  outlet_id: scope.outletId,
  return_id: source,
  expected_version: 1,
  expected_return_matched_minor: 0,
  amount_minor: 500,
  reason: "TEST explicit attribution",
  overage_reason: null,
  version: 2,
  recorded_by: credit.recorded_by,
  recorded_at: credit.recorded_at,
  return_number: 17,
  expected_credit_minor: 1000,
  return_matched_minor: 500,
  reversal_id: null,
};
afterEach(() => {
  vi.unstubAllGlobals();
  sessionStorage.clear();
});
it("retains human matching history when new matching is disabled without reading source choices", async () => {
  const fetcher = vi.fn<typeof fetch>(
    async () =>
      new Response(JSON.stringify({ data: [match], page: { has_more: false, next_cursor: null } })),
  );
  vi.stubGlobal("fetch", fetcher);
  render(<SupplierCreditMatching scope={scope} credit={credit} canManage={false} />);
  const table = await screen.findByRole("table", { name: "Supplier credit return matching" });
  expect(within(table).getByText("Return 17")).toBeInTheDocument();
  expect(within(table).getByText("INR 5.00")).toBeInTheDocument();
  expect(within(table).getByText("TEST explicit attribution")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Match credit to return" })).not.toBeInTheDocument();
  expect(fetcher.mock.calls.some(([url]) => String(url).includes("eligible-returns"))).toBe(false);
  for (const id of [match.id, source, scope.subject, credit.recorded_by, credit.id])
    expect(document.body).not.toHaveTextContent(id);
});

const returned = {
  id: source,
  outlet_id: scope.outletId,
  number: 17,
  payee_name: "TEST Supplier",
  original_bill_reference: "TEST-ORIGINAL-BILL",
  currency: "INR" as const,
  expected_credit_minor: 1000,
  matched_minor: 0,
};
it("matches an explicit amount using named current sources without applying credit to bills", async () => {
  const posted: Record<string, unknown>[] = [];
  const rows: (typeof match)[] = [];
  const fetcher = vi.fn(async (url: string, options?: RequestInit) => {
    if (options?.method === "PUT") {
      const input = JSON.parse(options.body as string);
      posted.push(input);
      const receipt = { ...match, ...input, id: url.split("/").at(-1), recorded_by: scope.subject };
      rows.push(receipt);
      return new Response(JSON.stringify({ ...receipt, reversal_id: undefined }));
    }
    return new Response(
      JSON.stringify({
        data: url.includes("eligible-returns") ? [returned] : rows,
        page: { has_more: false, next_cursor: null },
      }),
    );
  });
  vi.stubGlobal("fetch", fetcher);
  const { user } = render(
    <SupplierCreditMatching
      scope={scope}
      credit={{ ...credit, version: 1, matched_minor: 0 }}
      canManage
    />,
  );
  await user.click(await screen.findByRole("button", { name: "Match credit to return" }));
  await user.click(await screen.findByRole("combobox", { name: "Dispatched return to match" }));
  await user.click(await screen.findByRole("option", { name: /Return 17.*TEST-ORIGINAL-BILL/ }));
  expect(screen.getByLabelText("Amount to match (INR)")).toHaveValue("");
  await user.type(screen.getByLabelText("Amount to match (INR)"), "5.00");
  await user.type(screen.getByLabelText("Matching reason"), "TEST explicit attribution");
  await user.click(screen.getByRole("button", { name: "Save return match" }));
  expect(await screen.findByText(/Return matching verified/)).toBeInTheDocument();
  expect(posted).toEqual([
    {
      return_id: source,
      expected_version: 1,
      expected_return_matched_minor: 0,
      amount_minor: 500,
      reason: "TEST explicit attribution",
      overage_reason: null,
    },
  ]);
  expect(sessionStorage.length).toBe(0);
  expect(fetcher.mock.calls.filter(([, options]) => options?.method === "PUT")).toHaveLength(1);
  expect(
    fetcher.mock.calls.some(([url]) => url.includes("allocations") || url.includes("payables")),
  ).toBe(false);
});

async function enterMatch(user: ReturnType<typeof render>["user"], amount = "5.00") {
  await user.click(await screen.findByRole("button", { name: "Match credit to return" }));
  await user.click(await screen.findByRole("combobox", { name: "Dispatched return to match" }));
  await user.click(await screen.findByRole("option", { name: /Return 17.*TEST-ORIGINAL-BILL/ }));
  await user.type(screen.getByLabelText("Amount to match (INR)"), amount);
  await user.type(screen.getByLabelText("Matching reason"), "TEST explicit attribution");
}
it("recovers the same match after reload with recording disabled and no second write", async () => {
  let committed: typeof match | null = null;
  const writes: string[] = [];
  const fetcher = vi.fn(async (url: string, options?: RequestInit) => {
    if (options?.method === "PUT") {
      writes.push(url);
      committed = {
        ...match,
        ...JSON.parse(options.body as string),
        id: url.split("/").at(-1),
        recorded_by: scope.subject,
      };
      throw new TypeError("TEST response lost after commit");
    }
    if (url.endsWith("/recovery"))
      return new Response(JSON.stringify({ ...committed, reversal_id: undefined }));
    return new Response(
      JSON.stringify({
        data: url.includes("eligible-returns") ? [returned] : committed ? [committed] : [],
        page: { has_more: false, next_cursor: null },
      }),
    );
  });
  vi.stubGlobal("fetch", fetcher);
  const mounted = render(
    <SupplierCreditMatching
      scope={scope}
      credit={{ ...credit, version: 1, matched_minor: 0 }}
      canManage
    />,
  );
  await enterMatch(mounted.user);
  await mounted.user.click(screen.getByRole("button", { name: "Save return match" }));
  expect(await screen.findByText(/matching outcome is unconfirmed/)).toBeInTheDocument();
  expect(sessionStorage.length).toBe(1);
  expect(JSON.stringify({ ...sessionStorage })).not.toMatch(
    /TEST explicit attribution|amount_minor/,
  );
  mounted.unmount();
  fetcher.mockClear();
  const next = render(<SupplierCreditMatching scope={scope} credit={credit} canManage={false} />);
  await next.user.click(await screen.findByRole("button", { name: "Check original match" }));
  expect(await screen.findByText(/Return matching verified/)).toBeInTheDocument();
  expect(writes).toHaveLength(1);
  expect(sessionStorage.length).toBe(0);
  expect(fetcher.mock.calls.some(([url]) => url.includes("eligible-returns"))).toBe(false);
});
it("does not accept recovery of a changed in-memory matching intent", async () => {
  let committed: typeof match | null = null;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, options?: RequestInit) => {
      if (options?.method === "PUT") {
        committed = {
          ...match,
          ...JSON.parse(options.body as string),
          id: url.split("/").at(-1),
          recorded_by: scope.subject,
          reason: "TEST changed private intent",
        };
        throw new TypeError("TEST response lost");
      }
      if (url.endsWith("/recovery"))
        return new Response(JSON.stringify({ ...committed, reversal_id: undefined }));
      return new Response(
        JSON.stringify({
          data: url.includes("eligible-returns") ? [returned] : [],
          page: { has_more: false, next_cursor: null },
        }),
      );
    }),
  );
  const { user } = render(
    <SupplierCreditMatching
      scope={scope}
      credit={{ ...credit, version: 1, matched_minor: 0 }}
      canManage
    />,
  );
  await enterMatch(user);
  await user.click(screen.getByRole("button", { name: "Save return match" }));
  await user.click(await screen.findByRole("button", { name: "Check original match" }));
  expect(await screen.findByText(/original match could not be verified/)).toBeInTheDocument();
  expect(screen.queryByText(/Return matching verified/)).not.toBeInTheDocument();
  expect(sessionStorage.length).toBe(1);
});

it("requires explicit excess attribution evidence before sending a match", async () => {
  const posted: Record<string, unknown>[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, options?: RequestInit) => {
      if (options?.method === "PUT") {
        const input = JSON.parse(options.body as string);
        posted.push(input);
        return new Response(
          JSON.stringify({
            ...match,
            ...input,
            id: url.split("/").at(-1),
            recorded_by: scope.subject,
            expected_credit_minor: 300,
            reversal_id: undefined,
          }),
        );
      }
      return new Response(
        JSON.stringify({
          data: url.includes("eligible-returns")
            ? [{ ...returned, expected_credit_minor: 300 }]
            : [],
          page: { has_more: false, next_cursor: null },
        }),
      );
    }),
  );
  const { user } = render(
    <SupplierCreditMatching
      scope={scope}
      credit={{ ...credit, version: 1, matched_minor: 0 }}
      canManage
    />,
  );
  await enterMatch(user);
  await user.click(screen.getByRole("button", { name: "Save return match" }));
  expect(
    await screen.findByText("Explain why the total attribution exceeds the expected return value."),
  ).toBeInTheDocument();
  expect(posted).toHaveLength(0);
  await user.type(
    screen.getByLabelText("Excess attribution explanation"),
    "TEST supplier evidence confirms additional credit",
  );
  await user.click(screen.getByRole("button", { name: "Save return match" }));
  expect(await screen.findByText(/Return matching verified/)).toBeInTheDocument();
  expect(posted).toHaveLength(1);
  expect(posted[0]?.overage_reason).toBe("TEST supplier evidence confirms additional credit");
});
it("keeps the same verified command recoverable when session cleanup fails", async () => {
  const remove = vi.spyOn(Storage.prototype, "removeItem").mockImplementation(() => {
    throw new Error("TEST storage unavailable");
  });
  const writes: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, options?: RequestInit) => {
      if (options?.method === "PUT") {
        writes.push(url);
        return new Response(
          JSON.stringify({
            ...match,
            ...JSON.parse(options.body as string),
            id: url.split("/").at(-1),
            recorded_by: scope.subject,
            reversal_id: undefined,
          }),
        );
      }
      return new Response(
        JSON.stringify({
          data: url.includes("eligible-returns") ? [returned] : [],
          page: { has_more: false, next_cursor: null },
        }),
      );
    }),
  );
  const { user } = render(
    <SupplierCreditMatching
      scope={scope}
      credit={{ ...credit, version: 1, matched_minor: 0 }}
      canManage
    />,
  );
  await enterMatch(user);
  await user.click(screen.getByRole("button", { name: "Save return match" }));
  expect(
    await screen.findByText(/verified but its recovery reference could not be cleared/),
  ).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Match credit to return" })).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Resume matching recovery" }));
  expect(await screen.findByRole("button", { name: "Check original match" })).toBeInTheDocument();
  expect(writes).toHaveLength(1);
  expect(sessionStorage.length).toBe(1);
  remove.mockRestore();
});

it("keeps a refused amount visible and refreshes current sources before another command", async () => {
  let sourceReads = 0,
    writes = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, options?: RequestInit) => {
      if (options?.method === "PUT") {
        writes++;
        return new Response(
          JSON.stringify({ error: "conflict", message: "TEST stale credit or return observation" }),
          { status: 409 },
        );
      }
      if (url.includes("eligible-returns")) {
        sourceReads++;
        return new Response(
          JSON.stringify({
            data: [{ ...returned, matched_minor: sourceReads > 1 ? 100 : 0 }],
            page: { has_more: false, next_cursor: null },
          }),
        );
      }
      return new Response(
        JSON.stringify({ data: [], page: { has_more: false, next_cursor: null } }),
      );
    }),
  );
  const { user } = render(
    <SupplierCreditMatching
      scope={scope}
      credit={{ ...credit, version: 1, matched_minor: 0 }}
      canManage
    />,
  );
  await enterMatch(user);
  await user.click(screen.getByRole("button", { name: "Save return match" }));
  expect(await screen.findByText(/Match was refused/)).toBeInTheDocument();
  expect(screen.getByLabelText("Amount to match (INR)")).toHaveValue("5.00");
  expect(sessionStorage.length).toBe(0);
  expect(writes).toBe(1);
  await user.click(screen.getByRole("button", { name: "Refresh credit and matching sources" }));
  expect(
    await screen.findByText(/Already attributed across credits: INR 1.00/),
  ).toBeInTheDocument();
  expect(writes).toBe(1);
});

it("refuses to match more than the actual credit remaining for attribution", async () => {
  let writes = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, options?: RequestInit) => {
      if (options?.method === "PUT") writes++;
      return new Response(
        JSON.stringify({
          data: url.includes("eligible-returns") ? [returned] : [match],
          page: { has_more: false, next_cursor: null },
        }),
      );
    }),
  );
  const { user } = render(<SupplierCreditMatching scope={scope} credit={credit} canManage />);
  await enterMatch(user, "3.00");
  await user.click(screen.getByRole("button", { name: "Save return match" }));
  expect(screen.getByLabelText("Amount to match (INR)")).toHaveAttribute("aria-invalid", "true");
  expect(writes).toBe(0);
  expect(sessionStorage.length).toBe(0);
});
it("hides malformed foreign source choices without sending a command", async () => {
  let writes = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, options?: RequestInit) => {
      if (options?.method === "PUT") writes++;
      return new Response(
        JSON.stringify({
          data: url.includes("eligible-returns")
            ? [{ ...returned, outlet_id: credit.id, payee_name: "TEST PRIVATE FOREIGN SUPPLIER" }]
            : [],
          page: { has_more: false, next_cursor: null },
        }),
      );
    }),
  );
  const { user } = render(
    <SupplierCreditMatching
      scope={scope}
      credit={{ ...credit, version: 1, matched_minor: 0 }}
      canManage
    />,
  );
  await user.click(await screen.findByRole("button", { name: "Match credit to return" }));
  expect(await screen.findByText(/Dispatched return choices are unavailable/)).toBeInTheDocument();
  expect(document.body).not.toHaveTextContent("TEST PRIVATE FOREIGN SUPPLIER");
  expect(screen.queryByRole("button", { name: "Save return match" })).not.toBeInTheDocument();
  expect(writes).toBe(0);
});
