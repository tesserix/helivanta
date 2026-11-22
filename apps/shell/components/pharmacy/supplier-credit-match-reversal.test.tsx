import { afterEach, expect, it, vi } from "vitest";
import { screen, within } from "@testing-library/react";
import { renderWithProviders as renderBase } from "@helivanta/api/testing";
import { NavigationGuardProvider } from "@helivanta/ui";
import type { ReactElement } from "react";
import type { PharmacySupplierCreditMatchContext } from "@helivanta/api";
import { SupplierCreditMatchReversal } from "./supplier-credit-match-reversal";

const render = (ui: ReactElement) =>
  renderBase(<NavigationGuardProvider>{ui}</NavigationGuardProvider>);
const scope = {
  tenantId: "0199e000-0000-7000-8000-000000000001",
  pharmacyId: "0199e000-0000-7000-8000-000000000002",
  outletId: "0199e000-0000-7000-8000-000000000003",
  subject: "TEST-finance",
};
const credit = "0199e000-0000-7000-8000-000000000004",
  source = "0199e000-0000-7000-8000-000000000005",
  match = "0199e000-0000-7000-8000-000000000006";
const context: PharmacySupplierCreditMatchContext = {
  credit: {
    id: credit,
    outlet_id: scope.outletId,
    return_id: source,
    return_number: 17,
    payee_reference: "TEST-SUPPLIER",
    payee_name: "TEST Supplier",
    original_bill_reference: "TEST-ORIGINAL-BILL",
    document_reference: "TEST-CN-ONE",
    issued_on: "2026-11-18",
    amount_minor: 750,
    currency: "INR",
    reason: "TEST actual note",
    correction_of: null,
    recorded_by: "TEST-owner-subject",
    recorded_at: "2026-11-19T00:00:00Z",
    version: 2,
    state: "active",
    matched_minor: 500,
    allocated_minor: 0,
    available_minor: 750,
    local_date: "2026-11-22",
    as_of: "2026-11-21T20:00:00Z",
    timezone: "Asia/Kolkata",
  },
  match: {
    id: match,
    credit_id: credit,
    outlet_id: scope.outletId,
    return_id: source,
    expected_version: 1,
    expected_return_matched_minor: 0,
    amount_minor: 500,
    reason: "TEST initial attribution",
    overage_reason: null,
    version: 2,
    recorded_by: "TEST-other-finance-subject",
    recorded_at: "2026-11-19T00:00:00Z",
    return_number: 17,
    expected_credit_minor: 1000,
    return_matched_minor: 500,
    reversal_id: null,
  },
  returned: {
    id: source,
    outlet_id: scope.outletId,
    number: 17,
    payee_name: "TEST Supplier",
    original_bill_reference: "TEST-ORIGINAL-BILL",
    currency: "INR",
    expected_credit_minor: 1000,
    matched_minor: 600,
  },
};
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  sessionStorage.clear();
});
it("reverses the exact original amount with the current return total including other credit documents", async () => {
  const posted: Record<string, unknown>[] = [];
  let reversal: Record<string, unknown> | null = null;
  const fetcher = vi.fn(async (url: string, options?: RequestInit) => {
    if (options?.method === "PUT") {
      const input = JSON.parse(options.body as string);
      posted.push(input);
      reversal = {
        ...input,
        id: url.split("/").at(-1),
        credit_id: credit,
        outlet_id: scope.outletId,
        match_id: match,
        return_id: source,
        return_number: 17,
        amount_minor: 500,
        return_matched_minor: 100,
        version: 3,
        recorded_by: scope.subject,
        recorded_at: context.match.recorded_at,
      };
      return new Response(JSON.stringify(reversal));
    }
    if (url.endsWith("/reversal")) return new Response(JSON.stringify(reversal));
    return new Response(
      JSON.stringify(
        reversal
          ? {
              ...context,
              credit: { ...context.credit, version: 3, matched_minor: 0 },
              match: { ...context.match, reversal_id: reversal.id },
              returned: { ...context.returned, matched_minor: 100 },
            }
          : context,
      ),
    );
  });
  vi.stubGlobal("fetch", fetcher);
  const { user } = render(
    <SupplierCreditMatchReversal
      scope={scope}
      credit={credit}
      requested={{ id: match, sequence: 1 }}
      canManage
    />,
  );
  expect(
    await screen.findByText(/Current return attribution across credits: INR 6.00/),
  ).toBeInTheDocument();
  await user.type(screen.getByLabelText("Reversal reason"), "TEST explicit reversal");
  await user.click(screen.getByRole("button", { name: "Reverse full match" }));
  expect(await screen.findByText(/Return matching reversal verified/)).toBeInTheDocument();
  expect(posted).toEqual([
    { expected_version: 2, expected_return_matched_minor: 600, reason: "TEST explicit reversal" },
  ]);
  expect(sessionStorage.length).toBe(0);
  expect(fetcher.mock.calls.filter(([, options]) => options?.method === "PUT")).toHaveLength(1);
  expect(
    fetcher.mock.calls.some(
      ([url]) => url.includes("allocations") || url.includes("payables") || url.includes("stock"),
    ),
  ).toBe(false);
  for (const value of [match, credit, source, scope.subject, context.match.recorded_by])
    expect(document.body).not.toHaveTextContent(value);
  expect(await screen.findByText("TEST explicit reversal")).toBeInTheDocument();
});

it("restores reference-only reversal recovery after reload with new writes disabled", async () => {
  let committed: Record<string, unknown> | null = null;
  const writes: string[] = [];
  const fetcher = vi.fn(async (url: string, options?: RequestInit) => {
    if (options?.method === "PUT") {
      writes.push(url);
      committed = {
        ...JSON.parse(options.body as string),
        id: url.split("/").at(-1),
        credit_id: credit,
        outlet_id: scope.outletId,
        match_id: match,
        return_id: source,
        return_number: 17,
        amount_minor: 500,
        return_matched_minor: 100,
        version: 3,
        recorded_by: scope.subject,
        recorded_at: context.match.recorded_at,
      };
      throw new TypeError("TEST reversal committed but response lost");
    }
    if (url.endsWith("/recovery") || url.endsWith("/reversal"))
      return new Response(JSON.stringify(committed));
    return new Response(
      JSON.stringify(
        committed
          ? {
              ...context,
              credit: { ...context.credit, version: 3, matched_minor: 0 },
              match: { ...context.match, reversal_id: committed.id },
              returned: { ...context.returned, matched_minor: 100 },
            }
          : context,
      ),
    );
  });
  vi.stubGlobal("fetch", fetcher);
  const first = render(
    <SupplierCreditMatchReversal
      scope={scope}
      credit={credit}
      requested={{ id: match, sequence: 1 }}
      canManage
    />,
  );
  await first.user.type(
    await screen.findByLabelText("Reversal reason"),
    "TEST original correction",
  );
  await first.user.click(screen.getByRole("button", { name: "Reverse full match" }));
  expect(await screen.findByText(/reversal outcome is unconfirmed/)).toBeInTheDocument();
  const key = sessionStorage.key(0)!;
  expect(JSON.parse(sessionStorage.getItem(key)!)).toEqual({
    id: writes[0]?.split("/").at(-1),
    match,
  });
  first.unmount();
  fetcher.mockClear();
  const next = render(
    <SupplierCreditMatchReversal
      scope={scope}
      credit={credit}
      requested={null}
      canManage={false}
    />,
  );
  await next.user.click(await screen.findByRole("button", { name: "Check original reversal" }));
  expect(await screen.findByText(/Return matching reversal verified/)).toBeInTheDocument();
  expect(await screen.findByText("TEST original correction")).toBeInTheDocument();
  expect(writes).toHaveLength(1);
  expect(sessionStorage.length).toBe(0);
  expect(screen.queryByRole("button", { name: "Reverse full match" })).not.toBeInTheDocument();
});
it("shows retained reversal evidence after void without offering a new reversal", async () => {
  const entry = "0199e000-0000-7000-8000-000000000007";
  const reversal = {
    id: entry,
    credit_id: credit,
    outlet_id: scope.outletId,
    match_id: match,
    return_id: source,
    return_number: 17,
    amount_minor: 500,
    expected_version: 2,
    expected_return_matched_minor: 600,
    return_matched_minor: 100,
    version: 3,
    reason: "TEST retained void reversal",
    recorded_by: "TEST-reverser-subject",
    recorded_at: context.match.recorded_at,
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async (url: string) =>
        new Response(
          JSON.stringify(
            url.endsWith("/reversal")
              ? reversal
              : {
                  ...context,
                  credit: {
                    ...context.credit,
                    state: "voided",
                    version: 4,
                    matched_minor: 0,
                    available_minor: 0,
                  },
                  match: { ...context.match, reversal_id: entry },
                  returned: { ...context.returned, matched_minor: 100 },
                },
          ),
        ),
    ),
  );
  render(
    <SupplierCreditMatchReversal
      scope={scope}
      credit={credit}
      requested={{ id: match, sequence: 1 }}
      canManage
    />,
  );
  const region = within(await screen.findByRole("region", { name: "Return matching reversal" }));
  expect(await region.findByText("TEST retained void reversal")).toBeInTheDocument();
  expect(region.getByText("This match has already been reversed.")).toBeInTheDocument();
  expect(region.queryByRole("button", { name: "Reverse full match" })).not.toBeInTheDocument();
  expect(document.body).not.toHaveTextContent("TEST-reverser-subject");
});

it("keeps a stale refusal visible and uses refreshed owner observations for a new command", async () => {
  let refreshed = false;
  const posted: Record<string, unknown>[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, options?: RequestInit) => {
      if (options?.method === "PUT") {
        const input = JSON.parse(options.body as string);
        posted.push(input);
        if (posted.length === 1) {
          refreshed = true;
          return new Response(
            JSON.stringify({ error: "conflict", message: "TEST stale observations" }),
            { status: 409 },
          );
        }
        return new Response(
          JSON.stringify({
            ...input,
            id: url.split("/").at(-1),
            credit_id: credit,
            outlet_id: scope.outletId,
            match_id: match,
            return_id: source,
            return_number: 17,
            amount_minor: 500,
            return_matched_minor: 200,
            version: 4,
            recorded_by: scope.subject,
            recorded_at: context.match.recorded_at,
          }),
        );
      }
      return new Response(
        JSON.stringify(
          refreshed
            ? {
                ...context,
                credit: { ...context.credit, version: 3 },
                returned: { ...context.returned, matched_minor: 700 },
              }
            : context,
        ),
      );
    }),
  );
  const { user } = render(
    <SupplierCreditMatchReversal
      scope={scope}
      credit={credit}
      requested={{ id: match, sequence: 1 }}
      canManage
    />,
  );
  await user.type(await screen.findByLabelText("Reversal reason"), "TEST current correction");
  await user.click(screen.getByRole("button", { name: "Reverse full match" }));
  expect(await screen.findByText(/Reversal was refused/)).toBeInTheDocument();
  expect(screen.getByLabelText("Reversal reason")).toHaveValue("TEST current correction");
  expect(sessionStorage.length).toBe(0);
  await user.click(screen.getByRole("button", { name: "Refresh current match observations" }));
  expect(
    await screen.findByText(/Current return attribution across credits: INR 7.00/),
  ).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Reverse full match" }));
  expect(await screen.findByText(/Return matching reversal verified/)).toBeInTheDocument();
  expect(posted).toEqual([
    { expected_version: 2, expected_return_matched_minor: 600, reason: "TEST current correction" },
    { expected_version: 3, expected_return_matched_minor: 700, reason: "TEST current correction" },
  ]);
});
it("hides invalid current source evidence and never offers a financial command", async () => {
  const fetcher = vi.fn<typeof fetch>(
    async () =>
      new Response(
        JSON.stringify({
          ...context,
          returned: {
            ...context.returned,
            id: credit,
            original_bill_reference: "TEST PRIVATE UNRELATED BILL",
          },
        }),
      ),
  );
  vi.stubGlobal("fetch", fetcher);
  render(
    <SupplierCreditMatchReversal
      scope={scope}
      credit={credit}
      requested={{ id: match, sequence: 1 }}
      canManage
    />,
  );
  expect(await screen.findByText(/Match reversal evidence is unavailable/)).toBeInTheDocument();
  expect(document.body).not.toHaveTextContent("TEST PRIVATE UNRELATED BILL");
  expect(document.body).not.toHaveTextContent("TEST initial attribution");
  expect(screen.queryByRole("button", { name: "Reverse full match" })).not.toBeInTheDocument();
  expect(fetcher.mock.calls.some(([, options]) => options?.method === "PUT")).toBe(false);
});
it("does not verify changed retained reversal intent after response loss", async () => {
  let committed: Record<string, unknown> | null = null;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, options?: RequestInit) => {
      if (options?.method === "PUT") {
        committed = {
          ...JSON.parse(options.body as string),
          reason: "TEST changed intent",
          id: url.split("/").at(-1),
          credit_id: credit,
          outlet_id: scope.outletId,
          match_id: match,
          return_id: source,
          return_number: 17,
          amount_minor: 500,
          return_matched_minor: 100,
          version: 3,
          recorded_by: scope.subject,
          recorded_at: context.match.recorded_at,
        };
        throw new TypeError("TEST lost response");
      }
      return new Response(JSON.stringify(url.endsWith("/recovery") ? committed : context));
    }),
  );
  const { user } = render(
    <SupplierCreditMatchReversal
      scope={scope}
      credit={credit}
      requested={{ id: match, sequence: 1 }}
      canManage
    />,
  );
  await user.type(await screen.findByLabelText("Reversal reason"), "TEST intended correction");
  await user.click(screen.getByRole("button", { name: "Reverse full match" }));
  await user.click(await screen.findByRole("button", { name: "Check original reversal" }));
  expect(await screen.findByText(/original reversal could not be verified/)).toBeInTheDocument();
  expect(screen.queryByText(/Return matching reversal verified/)).not.toBeInTheDocument();
  expect(sessionStorage.length).toBe(1);
});
