import { act, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { NavigationGuardProvider } from "@helivanta/ui";
import { PharmacyWorkspace } from "./pharmacy-workspace";
const pharmacy = "0199e000-0000-7000-8000-000000000010",
  outlet = "0199e000-0000-7000-8000-000000000001";
const id = "0199e000-0000-7000-8000-000000000020";
const page = (data: unknown[]) => ({
  data,
  page: { has_more: false, next_cursor: null, total: data.length, offset: 0 },
});
// A live address: push and replace change what useSearchParams returns and
// re-render, as the app router does. Each call is recorded.
const nav = vi.hoisted(() => ({
  search: "",
  listeners: new Set<() => void>(),
  calls: [] as { method: "push" | "replace"; href: string }[],
}));
vi.mock("next/navigation", async () => {
  const { useSyncExternalStore } = await import("react");
  const subscribe = (fn: () => void) => {
    nav.listeners.add(fn);
    return () => nav.listeners.delete(fn);
  };
  const go = (method: "push" | "replace") => (href: string) => {
    nav.calls.push({ method, href });
    nav.search = href.split("?")[1] ?? "";
    nav.listeners.forEach((fn) => fn());
  };
  return {
    usePathname: () => "/pharmacy/accounts",
    useSearchParams: () => new URLSearchParams(useSyncExternalStore(subscribe, () => nav.search)),
    useRouter: () => ({ push: go("push"), replace: go("replace") }),
  };
});
afterEach(() => {
  vi.unstubAllGlobals();
  sessionStorage.clear();
  nav.search = "";
  nav.calls.length = 0;
});
function mockAccounts(financial = true, country = "IN") {
  const fetcher = vi.fn(async (url: string) => {
    if (url.endsWith("permissions"))
      return new Response(
        JSON.stringify({
          tenant_id: "11111111-1111-1111-1111-111111111111",
          subject: "TEST-finance",
          data: [
            "pharmacy.catalogue.read",
            ...(financial
              ? [
                  "pharmacy.accounts.read",
                  "pharmacy.accounts.write",
                  "pharmacy.accounts.payment",
                  "pharmacy.accounts.adjust",
                ]
              : []),
          ],
        }),
      );
    if (url.endsWith("/pharmacies"))
      return new Response(
        JSON.stringify(page([{ id: pharmacy, name: "TEST Independent Pharmacy" }])),
      );
    if (url.endsWith("/outlets"))
      return new Response(
        JSON.stringify(
          page([
            {
              id: outlet,
              name: "TEST Main Outlet",
              country_code: country,
              timezone: "Asia/Tashkent",
            },
          ]),
        ),
      );
    if (url.endsWith("/access")) return new Response(JSON.stringify({ level: "editor" }));
    if (url.includes("/accounts/payables/counted"))
      return new Response(
        JSON.stringify(
          page([
            {
              id,
              payee_reference: "TEST-SUPPLIER",
              payee_name: "TEST Supplier",
              document_reference: "TEST-EXPENSE-001",
              currency: "INR",
              issued_on: "2026-10-01",
              due_on: "2026-10-31",
              total_minor: 11800,
              paid_minor: 3000,
              credit_minor: 800,
              outstanding_minor: 8000,
              line_count: 1,
              created_at: "2026-10-15T00:00:00Z",
            },
          ]),
        ),
      );
    if (url.includes("/accounts/inventory-audit/counted"))
      return new Response(
        JSON.stringify(
          page([
            {
              id,
              sku: "TEST-500",
              brand_name: "TEST Capsule",
              lot_number: "TEST-LOT",
              expires_on: "2030-10-31",
              currency: "INR",
              unit_of_issue: "capsule",
              received_units: 25,
              on_hand_units: 25,
              uncosted_receipt_units: 25,
              acquisition_cost_minor: "0",
              valuation_minor: null,
              valuation_status: "cost_missing",
            },
          ]),
        ),
      );
    return new Response(JSON.stringify(page([])));
  });
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}
it("shows paid, credited and remaining amounts", async () => {
  mockAccounts();
  renderWithProviders(
    <NavigationGuardProvider>
      <PharmacyWorkspace view="accounts" />
    </NavigationGuardProvider>,
  );
  expect(await screen.findByText("TEST-EXPENSE-001")).toBeInTheDocument();
  expect(screen.getByText("TEST Supplier")).toBeInTheDocument();
  expect(screen.getByRole("columnheader", { name: "Outstanding" })).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Record bill or expense" })).toBeInTheDocument();
});

it("shows the inventory cost audit on its own tab, without valuing uncosted drugs", async () => {
  nav.search = "tab=audit";
  const fetcher = mockAccounts();
  renderWithProviders(
    <NavigationGuardProvider>
      <PharmacyWorkspace view="accounts" />
    </NavigationGuardProvider>,
  );
  expect(await screen.findByText("Purchase cost missing")).toBeInTheDocument();
  expect(screen.getByText("25 uncosted units")).toBeInTheDocument();
  expect(screen.queryByText("TEST-EXPENSE-001")).not.toBeInTheDocument();
  expect(requested(fetcher, "/accounts/inventory-audit/counted")).toEqual([
    expect.stringMatching(/\/accounts\/inventory-audit\/counted\?limit=25$/),
  ]);
  expect(requested(fetcher, "/accounts/payables")).toEqual([]);
});

// #576 slice 3: each tab is the URL, 25 a page with the total.
describe("bill tabs", () => {
  it("opens Overdue from the URL, filtered and counted", async () => {
    nav.search = "tab=overdue";
    const fetcher = mockAccounts();
    renderWithProviders(
      <NavigationGuardProvider>
        <PharmacyWorkspace view="accounts" />
      </NavigationGuardProvider>,
    );
    expect(await screen.findByText("TEST-EXPENSE-001")).toBeInTheDocument();
    expect(requested(fetcher, "/accounts/payables/counted")).toEqual([
      expect.stringMatching(/\/accounts\/payables\/counted\?due=overdue&limit=25$/),
    ]);
    expect(screen.getByRole("link", { name: "Overdue" })).toHaveAttribute("aria-current", "page");
    expect(screen.getByText(/1–1 of 1/)).toBeInTheDocument();
  });

  it("links every tab at its first page and drops the old filter button", async () => {
    nav.search = "tab=week&page=2&cursor=c2";
    mockAccounts();
    renderWithProviders(
      <NavigationGuardProvider>
        <PharmacyWorkspace view="accounts" />
      </NavigationGuardProvider>,
    );
    await screen.findByText("TEST-EXPENSE-001");
    expect(screen.getByRole("link", { name: "All bills" })).toHaveAttribute(
      "href",
      "/pharmacy/accounts?tab=all",
    );
    expect(screen.getByRole("link", { name: "Inventory cost audit" })).toHaveAttribute(
      "href",
      "/pharmacy/accounts?tab=audit",
    );
    expect(screen.queryByRole("button", { name: "Show all bills" })).not.toBeInTheDocument();
  });

  // Links from before the tabs (?due=) and unknown tabs open All bills.
  it("reads an unknown tab or an old due link as All bills", async () => {
    for (const search of ["due=overdue", "tab=toString"]) {
      nav.search = search;
      const fetcher = mockAccounts();
      const { unmount } = renderWithProviders(
        <NavigationGuardProvider>
          <PharmacyWorkspace view="accounts" />
        </NavigationGuardProvider>,
      );
      await screen.findByText("TEST-EXPENSE-001");
      expect(requested(fetcher, "/accounts/payables/counted")).toEqual([
        expect.stringMatching(/\/accounts\/payables\/counted\?limit=25$/),
      ]);
      unmount();
    }
  });
});

const requested = (fetcher: ReturnType<typeof vi.fn>, path: string) =>
  fetcher.mock.calls.map(([url]) => String(url)).filter((url) => url.includes(path));
it("does not fetch confidential costs without financial read permission", async () => {
  const fetcher = mockAccounts(false);
  renderWithProviders(
    <NavigationGuardProvider>
      <PharmacyWorkspace view="accounts" />
    </NavigationGuardProvider>,
  );
  expect(await screen.findByText("Financial access required")).toBeInTheDocument();
  await waitFor(() =>
    expect(fetcher.mock.calls.some(([url]) => url.includes("/accounts/"))).toBe(false),
  );
  expect(screen.queryByText("TEST Supplier")).not.toBeInTheDocument();
});

it("focuses the expense form and records exact minor amounts with a stable retry ID", async () => {
  const base = mockAccounts();
  const writes: { url: string; body: Record<string, unknown> }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "PUT") {
        const body = JSON.parse(init.body as string);
        writes.push({ url, body });
        return new Response(
          JSON.stringify({ error: "unavailable", message: "TEST record unavailable" }),
          { status: 503 },
        );
      }
      return base(url);
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <PharmacyWorkspace view="accounts" />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Record bill or expense" }));
  const payee = await screen.findByLabelText("Payee name");
  expect(payee).toHaveFocus();
  await user.type(payee, "TEST Supplier");
  await user.type(screen.getByLabelText("Payee reference"), "TEST-SUPPLIER");
  await user.type(screen.getByLabelText("Document reference"), "TEST-LOCAL-BILL");
  await user.type(screen.getByLabelText("Document date"), "2026-10-01");
  await user.type(screen.getByLabelText("Due date"), "2026-10-31");
  await user.type(screen.getByLabelText("Description 1"), "TEST Electricity");
  await user.clear(screen.getByLabelText("Net amount 1 (INR)"));
  await user.type(screen.getByLabelText("Net amount 1 (INR)"), "125.25");
  await user.click(screen.getByRole("button", { name: "Save bill or expense" }));
  expect(await screen.findByText("TEST record unavailable")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Save bill or expense" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[0].url).toBe(writes[1].url);
  expect(writes[0].body.lines).toEqual([
    expect.objectContaining({ net_minor: 12525, tax_minor: 0 }),
  ]);
  expect(writes[0].url).toMatch(/\/payables\/[0-9a-f-]{14}7[0-9a-f-]+$/);
});

it("defaults expenses to the outlet country's currency", async () => {
  mockAccounts(true, "IN");
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <PharmacyWorkspace view="accounts" />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Record bill or expense" }));
  expect(screen.getByLabelText("Currency")).toHaveTextContent("INR");
  expect(screen.getByLabelText("Net amount 1 (INR)")).toBeInTheDocument();
  // #1287: the bill currency offers supported currencies only.
  await user.click(screen.getByLabelText("Currency"));
  expect((await screen.findAllByRole("option")).map((o) => o.textContent)).toEqual(["INR"]);
});

it("uses the configured timezone for payable and settlement recording times without changing document dates", async () => {
  const base = mockAccounts();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.endsWith(`/accounts/payables/${id}`)) {
        const list = await (await base(`${url.slice(0, -(id.length + 1))}/counted`)).json();
        return new Response(
          JSON.stringify({ ...list.data[0], lines: [], actor_subject: "TEST-finance" }),
        );
      }
      if (url.endsWith(`/accounts/payables/${id}/settlements`))
        return new Response(
          JSON.stringify(
            page([
              {
                id: "TEST-payment",
                kind: "payment",
                currency: "INR",
                amount_minor: 3000,
                occurred_on: "2026-10-14",
                paid_by: "TEST payer",
                method: "bank_transfer",
                source_reference: "TEST-external",
                actor_subject: "TEST-finance",
                created_at: "2026-10-14T23:30:00Z",
                reason: "TEST external settlement",
              },
            ]),
          ),
        );
      return base(url);
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <PharmacyWorkspace view="accounts" />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Open TEST-EXPENSE-001" }));
  expect(await screen.findByText("2026-10-15 05:00:00 GMT+05:00")).toHaveAttribute(
    "datetime",
    "2026-10-15T00:00:00Z",
  );
  expect(await screen.findByText("2026-10-15 04:30:00 GMT+05:00")).toHaveAttribute(
    "datetime",
    "2026-10-14T23:30:00Z",
  );
  expect(screen.getByText(/Document date 2026-10-01 · Due 2026-10-31/)).toBeInTheDocument();
  expect(screen.getByText("2026-10-14")).toBeInTheDocument();
});

// #1303: a bill opens beside the list, kept in the URL, and unsaved edits
// are never lost silently.
const second = "0199e000-0000-7000-8000-000000000021";
const third = "0199e000-0000-7000-8000-000000000022";
const bill = (bid: string, ref: string) => ({
  id: bid,
  payee_reference: "TEST-SUPPLIER",
  payee_name: "TEST Supplier",
  document_reference: ref,
  currency: "INR",
  issued_on: "2026-10-01",
  due_on: "2026-10-31",
  total_minor: 11800,
  paid_minor: 3000,
  credit_minor: 800,
  outstanding_minor: 8000,
  line_count: 1,
  created_at: "2026-10-15T00:00:00Z",
  actor_subject: "TEST-finance",
});
const bills = [
  bill(id, "TEST-EXPENSE-001"),
  bill(second, "TEST-EXPENSE-002"),
  bill(third, "TEST-EXPENSE-003"),
];

function mockDetail(opts: { list?: () => unknown[]; missing?: string } = {}) {
  const base = mockAccounts();
  const fetcher = vi.fn(async (url: string) => {
    if (url.includes("/accounts/payables/counted"))
      return new Response(JSON.stringify(page(opts.list?.() ?? bills)));
    if (url.endsWith("/settlements")) return new Response(JSON.stringify(page([])));
    const match = bills.find((b) => url.endsWith(`/accounts/payables/${b.id}`));
    if (match) return new Response(JSON.stringify({ ...match, lines: [] }));
    if (opts.missing && url.endsWith(`/accounts/payables/${opts.missing}`))
      return new Response(JSON.stringify({ error: "not_found", message: "TEST not found" }), {
        status: 404,
      });
    return base(url);
  });
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}

describe("bill detail pane", () => {
  it("opens the bill named in the URL, with its heading focused", async () => {
    nav.search = `tab=all&open=${id}`;
    mockDetail();
    renderWithProviders(
      <NavigationGuardProvider>
        <PharmacyWorkspace view="accounts" />
      </NavigationGuardProvider>,
    );
    const pane = await screen.findByRole("region", { name: "Bill TEST-EXPENSE-001" });
    const heading = await within(pane).findByRole("heading", { name: "TEST-EXPENSE-001" });
    await waitFor(() => expect(heading).toHaveFocus());
  });

  // Another outlet's or tenant's bill is a 404: say so, and fetch nothing else.
  it("says a bill it cannot open was not found", async () => {
    const missing = "0199e000-0000-7000-8000-000000000099";
    nav.search = `tab=all&open=${missing}`;
    const fetcher = mockDetail({ missing });
    renderWithProviders(
      <NavigationGuardProvider>
        <PharmacyWorkspace view="accounts" />
      </NavigationGuardProvider>,
    );
    expect(
      await screen.findByText("This record was not found, or you do not have access."),
    ).toBeVisible();
    const calls = fetcher.mock.calls.map(([u]) => String(u)).filter((u) => u.includes(missing));
    expect(calls).toEqual([expect.stringMatching(new RegExp(`/accounts/payables/${missing}$`))]);
  });

  it("pushes when opening a bill and replaces when switching", async () => {
    nav.search = "tab=all";
    mockDetail();
    const { user } = renderWithProviders(
      <NavigationGuardProvider>
        <PharmacyWorkspace view="accounts" />
      </NavigationGuardProvider>,
    );
    await user.click(await screen.findByRole("button", { name: "Open TEST-EXPENSE-001" }));
    await screen.findByRole("region", { name: "Bill TEST-EXPENSE-001" });
    await user.click(screen.getByRole("button", { name: "Open TEST-EXPENSE-002" }));
    await screen.findByRole("region", { name: "Bill TEST-EXPENSE-002" });
    expect(nav.calls.map((c) => c.method)).toEqual(["push", "replace"]);
    expect(nav.calls.at(-1)?.href).toBe(`/pharmacy/accounts?tab=all&open=${second}`);
    expect(
      screen.getByRole("button", { name: "Open TEST-EXPENSE-002" }).closest("tr"),
    ).toHaveAttribute("aria-current", "true");
  });

  it("steps through the page with Previous and Next, disabled at the ends", async () => {
    nav.search = "tab=all";
    mockDetail();
    const { user } = renderWithProviders(
      <NavigationGuardProvider>
        <PharmacyWorkspace view="accounts" />
      </NavigationGuardProvider>,
    );
    await user.click(await screen.findByRole("button", { name: "Open TEST-EXPENSE-001" }));
    await screen.findByRole("region", { name: "Bill TEST-EXPENSE-001" });
    expect(screen.getByRole("button", { name: "Previous bill" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "Next bill" }));
    await screen.findByRole("region", { name: "Bill TEST-EXPENSE-002" });
    await user.click(screen.getByRole("button", { name: "Next bill" }));
    await screen.findByRole("region", { name: "Bill TEST-EXPENSE-003" });
    expect(screen.getByRole("button", { name: "Next bill" })).toBeDisabled();
  });

  // Opened from a link or a reload, the neighbours are unknown: no guessing.
  it("has no Previous or Next when opened from the URL", async () => {
    nav.search = `tab=all&open=${second}`;
    mockDetail();
    renderWithProviders(
      <NavigationGuardProvider>
        <PharmacyWorkspace view="accounts" />
      </NavigationGuardProvider>,
    );
    await screen.findByRole("region", { name: "Bill TEST-EXPENSE-002" });
    expect(screen.getByRole("button", { name: "Previous bill" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Next bill" })).toBeDisabled();
  });

  // Review focus 1: a payment drops the bill out of Overdue; it stays open
  // and Next goes to the bill that was below it.
  it("keeps a bill open after it leaves the list, and Next goes to the one below", async () => {
    nav.search = "tab=overdue";
    let rows = bills;
    mockDetail({ list: () => rows });
    const { user } = renderWithProviders(
      <NavigationGuardProvider>
        <PharmacyWorkspace view="accounts" />
      </NavigationGuardProvider>,
    );
    await user.click(await screen.findByRole("button", { name: "Open TEST-EXPENSE-001" }));
    await screen.findByRole("region", { name: "Bill TEST-EXPENSE-001" });
    rows = bills.slice(1);
    // The list refetches, as it does when the pharmacist returns to the window.
    act(() => {
      window.dispatchEvent(new Event("visibilitychange"));
    });
    await waitFor(() =>
      expect(
        screen.queryByRole("button", { name: "Open TEST-EXPENSE-001" }),
      ).not.toBeInTheDocument(),
    );
    expect(screen.getByRole("region", { name: "Bill TEST-EXPENSE-001" })).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Next bill" }));
    await screen.findByRole("region", { name: "Bill TEST-EXPENSE-002" });
  });

  it("asks before discarding a half-typed settlement, and Keep editing keeps it", async () => {
    nav.search = "tab=all";
    mockDetail();
    const { user } = renderWithProviders(
      <NavigationGuardProvider>
        <PharmacyWorkspace view="accounts" />
      </NavigationGuardProvider>,
    );
    await user.click(await screen.findByRole("button", { name: "Open TEST-EXPENSE-001" }));
    const paidBy = await screen.findByLabelText("Paid by");
    await user.type(paidBy, "TEST payer");
    await user.click(screen.getByRole("button", { name: "Open TEST-EXPENSE-002" }));
    expect(await screen.findByRole("dialog")).toHaveTextContent(
      "Discard changes to TEST-EXPENSE-001?",
    );
    await user.click(screen.getByRole("button", { name: "Keep editing" }));
    expect(screen.getByLabelText("Paid by")).toHaveValue("TEST payer");
    expect(nav.search).toBe(`tab=all&open=${id}`);
  });

  it("opens a new bill in the pane with its first field focused", async () => {
    nav.search = "tab=all";
    mockDetail();
    const { user } = renderWithProviders(
      <NavigationGuardProvider>
        <PharmacyWorkspace view="accounts" />
      </NavigationGuardProvider>,
    );
    await user.click(await screen.findByRole("button", { name: "Record bill or expense" }));
    const pane = await screen.findByRole("region", { name: "New supplier bill" });
    expect(nav.search).toBe("tab=all&new=1");
    await waitFor(() => expect(within(pane).getByLabelText("Payee name")).toHaveFocus());
  });

  it("closes on Escape and returns to the row it opened from", async () => {
    nav.search = "tab=all";
    mockDetail();
    const { user } = renderWithProviders(
      <NavigationGuardProvider>
        <PharmacyWorkspace view="accounts" />
      </NavigationGuardProvider>,
    );
    await user.click(await screen.findByRole("button", { name: "Open TEST-EXPENSE-002" }));
    await screen.findByRole("heading", { name: "TEST-EXPENSE-002" });
    await user.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("region", { name: /^Bill / })).toBeNull());
    expect(nav.search).toBe("tab=all");
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Open TEST-EXPENSE-002" })).toHaveFocus(),
    );
  });

  it("closes the bill when the tab changes", async () => {
    nav.search = `tab=all&open=${id}`;
    mockDetail();
    renderWithProviders(
      <NavigationGuardProvider>
        <PharmacyWorkspace view="accounts" />
      </NavigationGuardProvider>,
    );
    await screen.findByRole("region", { name: "Bill TEST-EXPENSE-001" });
    expect(screen.getByRole("link", { name: "Overdue" })).toHaveAttribute(
      "href",
      "/pharmacy/accounts?tab=overdue",
    );
  });
});

// Review Important 2: a save that finishes after the person has left the
// page records the bill, but must not pull them back to it.
it("does not pull the person back when a save finishes after they left", async () => {
  nav.search = "tab=all&new=1";
  const base = mockDetail();
  let finish: (r: Response) => void = () => {};
  vi.stubGlobal(
    "fetch",
    vi.fn((url: string, init?: RequestInit) =>
      init?.method === "PUT"
        ? new Promise<Response>((resolve) => {
            finish = resolve;
          })
        : base(url),
    ),
  );
  const { user, unmount } = renderWithProviders(
    <NavigationGuardProvider>
      <PharmacyWorkspace view="accounts" />
    </NavigationGuardProvider>,
  );
  await user.type(await screen.findByLabelText("Payee name"), "TEST Supplier");
  await user.type(screen.getByLabelText("Payee reference"), "TEST-SUPPLIER");
  await user.type(screen.getByLabelText("Document reference"), "TEST-LATE-BILL");
  await user.type(screen.getByLabelText("Document date"), "2026-10-01");
  await user.type(screen.getByLabelText("Due date"), "2026-10-31");
  await user.type(screen.getByLabelText("Description 1"), "TEST Electricity");
  await user.clear(screen.getByLabelText("Net amount 1 (INR)"));
  await user.type(screen.getByLabelText("Net amount 1 (INR)"), "125.25");
  await user.click(screen.getByRole("button", { name: "Save bill or expense" }));
  await waitFor(() => expect(finish).not.toBe(undefined));
  const before = nav.calls.length;
  unmount();
  await act(async () => {
    finish(new Response(JSON.stringify({ ...bills[0], lines: [] })));
  });
  await new Promise((resolve) => setTimeout(resolve, 50));
  expect(nav.calls.slice(before)).toEqual([]);
});
