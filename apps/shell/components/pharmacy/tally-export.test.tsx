import { createHash } from "node:crypto";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { NavigationGuardProvider } from "@helivanta/ui";
import { TallyExport } from "./tally-export";

const scope = {
  tenantId: "11111111-1111-1111-1111-111111111111",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
  subject: "TEST-owner",
};
const root = `/api/v1/pharmacy/pharmacies/${scope.pharmacyId}/outlets/${scope.outletId}/accounts/tally`;
const first = "0199e000-0000-7000-8000-000000000020";
const heads = [
  { head: "voucher_type:sale", label: "Voucher type for sales" },
  { head: "tender:cash", label: "Counter cash" },
  { head: "supplier:ACME", label: "Supplier ACME", supplier: "TEST Acme Pharma" },
];
const xml = "<ENVELOPE><HEADER></HEADER></ENVELOPE>\n";

type Put = { url: string; body: { replaces: string | null; entries: unknown[] }; headers: Headers };
function serve({
  vouchers = 4,
  put,
}: { vouchers?: number; put?: () => Response | undefined } = {}) {
  const state = {
    map: first as string | null,
    names: { "voucher_type:sale": "Sales", "tender:cash": "Cash" } as Record<string, string>,
    previews: [] as string[],
    puts: [] as Put[],
    downloads: [] as string[],
  };
  const fetcher = vi.fn(async (url: string, init: RequestInit = {}) => {
    const query = new URL(url, "http://test").searchParams;
    if (url.startsWith(`${root}/preview?`)) {
      state.previews.push(url);
      return Response.json({
        outlet_id: scope.outletId,
        from: query.get("from"),
        to: query.get("to"),
        ledger_map_id: state.map,
        sale_count: vouchers ? 2 : 0,
        credit_note_count: vouchers ? 1 : 0,
        supplier_payment_count: vouchers ? 1 : 0,
        payment_reversal_count: 0,
        heads: vouchers
          ? heads.map((h) => ({
              head: h.head,
              label: h.label,
              ledger_name: state.names[h.head] ?? null,
              supplier_name: h.supplier ?? null,
            }))
          : [],
      });
    }
    if (url.startsWith(`${root}/ledger-maps/`) && init.method === "PUT") {
      const body = JSON.parse(String(init.body)) as Put["body"];
      state.puts.push({ url, body, headers: new Headers(init.headers) });
      const refused = put?.();
      if (refused) return refused;
      const id = url.slice(`${root}/ledger-maps/`.length);
      if (body.replaces !== state.map)
        return Response.json(
          { error: "tally_ledger_map_conflict", message: "reload before saving" },
          { status: 409 },
        );
      for (const entry of body.entries as { head: string; ledger_name: string }[])
        state.names[entry.head] = entry.ledger_name;
      state.map = id;
      return Response.json({
        id,
        outlet_id: scope.outletId,
        replaces: body.replaces,
        entries: heads
          .filter((h) => state.names[h.head])
          .map((h) => ({ head: h.head, label: h.label, ledger_name: state.names[h.head] })),
        recorded_by: scope.subject,
        recorded_at: "2026-11-15T10:00:00Z",
      });
    }
    if (url.startsWith(`${root}/vouchers.xml?`)) {
      state.downloads.push(url);
      if (query.get("ledger_map") !== (state.map ?? ""))
        return Response.json(
          {
            error: "tally_ledger_map_changed",
            message:
              "the ledger map changed since the preview; preview the period again before exporting",
          },
          { status: 409 },
        );
      return new Response(xml, {
        headers: {
          "Content-Type": "application/xml; charset=utf-8",
          "Content-Disposition": `attachment; filename="pharmacy-tally-vouchers-${scope.outletId}-${query.get("from")}-${query.get("to")}.xml"`,
          "X-Export-Voucher-Count": String(vouchers),
          "X-Export-Ledger-Map": state.map ?? "",
          "X-Export-Sha256": createHash("sha256").update(xml).digest("hex"),
        },
      });
    }
    throw new Error(`unexpected request ${init.method ?? "GET"} ${url}`);
  });
  vi.stubGlobal("fetch", fetcher);
  return state;
}
function render(writable = true) {
  return renderWithProviders(
    <NavigationGuardProvider>
      <TallyExport scope={scope} today="2026-11-15" writable={writable} />
    </NavigationGuardProvider>,
  );
}
const download = () => screen.queryByRole("button", { name: "Download Tally XML" });
const downloaded = vi.fn();
beforeEach(() => {
  vi.stubGlobal(
    "URL",
    Object.assign(class extends URL {}, {
      createObjectURL: vi.fn(() => "blob:tally"),
      revokeObjectURL: vi.fn(),
    }),
  );
  vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function (
    this: HTMLAnchorElement,
  ) {
    downloaded(this.download);
  });
});
afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  downloaded.mockReset();
});

it("previews the month so far and holds the file until every head has a ledger", async () => {
  const state = serve();
  render();
  expect(await screen.findByText("Sales invoices")).toBeVisible();
  expect(screen.getByText("Sales invoices").parentElement).toHaveTextContent("Sales invoices2");
  expect(screen.getByText("Credit notes").parentElement).toHaveTextContent("Credit notes1");
  expect(screen.getByText("Supplier payments").parentElement).toHaveTextContent(
    "Supplier payments1",
  );
  expect(screen.getByText("Payment reversals").parentElement).toHaveTextContent(
    "Payment reversals0",
  );
  expect(state.previews).toEqual([`${root}/preview?from=2026-11-01&to=2026-11-15`]);
  expect(screen.getByLabelText("Voucher type for sales")).toHaveValue("Sales");
  expect(screen.getByLabelText("Supplier ACME (TEST Acme Pharma)")).toHaveValue("");
  expect(
    screen.getByText("Name a Tally ledger for 1 account head before downloading."),
  ).toBeVisible();
  expect(download()).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Save ledger names" })).toBeDisabled();
  expect(state.downloads).toEqual([]);
});

it("asks before leaving unsaved ledger names", async () => {
  serve();
  const { user } = render();
  const leave = () => {
    const event = new Event("beforeunload", { cancelable: true });
    fireEvent(window, event);
    return event.defaultPrevented;
  };
  await user.type(await screen.findByLabelText("Counter cash"), " Box");
  expect(leave()).toBe(true);
  await user.clear(screen.getByLabelText("Counter cash"));
  await user.type(screen.getByLabelText("Counter cash"), "Cash");
  expect(leave()).toBe(false);
});

it("saves a named head and leaves an unnamed one for later", async () => {
  const state = serve();
  const { user } = render();
  const cash = await screen.findByLabelText("Counter cash");
  await user.clear(cash);
  await user.type(cash, "Cash Box");
  await user.click(screen.getByRole("button", { name: "Save ledger names" }));
  expect(await screen.findByText("Ledger names saved")).toBeVisible();
  expect(state.puts[0]!.body.entries).toEqual([{ head: "tender:cash", ledger_name: "Cash Box" }]);
  expect(
    await screen.findByText("Name a Tally ledger for 1 account head before downloading."),
  ).toBeVisible();
});

it("saves only the changed names as a new map version, then downloads that version", async () => {
  const state = serve();
  const { user } = render();
  await user.type(await screen.findByLabelText("Supplier ACME (TEST Acme Pharma)"), "Acme Pharma");
  expect(screen.getByText("Save the ledger names before downloading.")).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Save ledger names" }));
  expect(await screen.findByText("Ledger names saved")).toBeVisible();
  expect(state.puts).toHaveLength(1);
  expect(state.puts[0]!.url).toMatch(
    new RegExp(
      `^${root}/ledger-maps/[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`,
    ),
  );
  expect(state.puts[0]!.body).toEqual({
    replaces: first,
    entries: [{ head: "supplier:ACME", ledger_name: "Acme Pharma" }],
  });
  expect(state.puts[0]!.headers.get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
  await waitFor(() => expect(state.previews).toHaveLength(2));
  await user.click(await screen.findByRole("button", { name: "Download Tally XML" }));
  expect(await screen.findByText("XML download started.")).toBeVisible();
  expect(state.downloads).toEqual([
    `${root}/vouchers.xml?from=2026-11-01&to=2026-11-15&ledger_map=${state.map}`,
  ]);
  expect(state.map).not.toBe(first);
  expect(downloaded).toHaveBeenCalledWith(
    `pharmacy-tally-vouchers-${scope.outletId}-2026-11-01-2026-11-15.xml`,
  );
  expect(screen.getByText(/Import each file into Tally once/)).toBeVisible();
});

it("refuses a name Tally would not match before asking the server", async () => {
  const state = serve();
  const { user } = render();
  const cash = await screen.findByLabelText("Counter cash");
  await user.type(cash, " ");
  await user.click(screen.getByRole("button", { name: "Save ledger names" }));
  expect(
    await screen.findByText(
      "Remove the spaces before or after the name. Tally matches ledger names exactly.",
    ),
  ).toBeVisible();
  await user.clear(cash);
  await user.click(screen.getByRole("button", { name: "Save ledger names" }));
  expect(await screen.findByText("Enter the Tally ledger name.")).toBeVisible();
  expect(state.puts).toEqual([]);
});

it("retries an unanswered save under the same map id, so it can replay", async () => {
  let fail = true;
  const state = serve({
    put: () => {
      if (!fail) return undefined;
      fail = false;
      throw new TypeError("network down");
    },
  });
  const { user } = render();
  await user.type(await screen.findByLabelText("Supplier ACME (TEST Acme Pharma)"), "Acme Pharma");
  await user.click(screen.getByRole("button", { name: "Save ledger names" }));
  await waitFor(() => expect(state.puts).toHaveLength(1));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "Save ledger names" })).toBeEnabled(),
  );
  await user.click(screen.getByRole("button", { name: "Save ledger names" }));
  expect(await screen.findByText("Ledger names saved")).toBeVisible();
  expect(state.puts.map((p) => p.url)).toEqual([state.puts[0]!.url, state.puts[0]!.url]);
});

it("reloads the preview when the map changed after it", async () => {
  const state = serve();
  const { user } = render();
  await user.type(await screen.findByLabelText("Supplier ACME (TEST Acme Pharma)"), "Acme Pharma");
  state.map = "0199e000-0000-7000-8000-000000000021";
  await user.click(screen.getByRole("button", { name: "Save ledger names" }));
  expect(
    await screen.findByText(
      "Someone saved ledger names since this preview. Check the names below and save again.",
    ),
  ).toBeVisible();
  await waitFor(() => expect(state.previews).toHaveLength(2));
  expect(state.puts).toHaveLength(1);
  expect(screen.queryByText("Ledger names saved")).not.toBeInTheDocument();
});

it("refuses a file for a map other than the previewed one and reloads the preview", async () => {
  const state = serve();
  state.names["supplier:ACME"] = "Acme Pharma";
  const { user } = render();
  await user.click(await screen.findByRole("button", { name: "Download Tally XML" }));
  expect(await screen.findByText("XML download started.")).toBeVisible();
  state.map = "0199e000-0000-7000-8000-000000000021";
  await user.click(screen.getByRole("button", { name: "Download Tally XML" }));
  await waitFor(() => expect(state.previews).toHaveLength(2));
  expect(screen.getByRole("alert")).toHaveTextContent(
    "The ledger names changed since this preview. Check them below, then download again.",
  );
  expect(state.downloads).toEqual([
    `${root}/vouchers.xml?from=2026-11-01&to=2026-11-15&ledger_map=${first}`,
    `${root}/vouchers.xml?from=2026-11-01&to=2026-11-15&ledger_map=${first}`,
  ]);
  expect(downloaded).toHaveBeenCalledTimes(1);
});

it("reloads the preview when the period needs a ledger the preview did not show", async () => {
  const state = serve();
  state.names["supplier:ACME"] = "Acme Pharma";
  const { user } = render();
  await user.click(await screen.findByRole("button", { name: "Download Tally XML" }));
  expect(await screen.findByText("XML download started.")).toBeVisible();
  delete state.names["supplier:ACME"];
  vi.mocked(fetch).mockImplementationOnce(async () =>
    Response.json(
      {
        error: "tally_ledgers_unmapped",
        message: "name a Tally ledger for every account head the period needs before exporting",
      },
      { status: 409 },
    ),
  );
  await user.click(screen.getByRole("button", { name: "Download Tally XML" }));
  expect(
    await screen.findByText("Name a Tally ledger for 1 account head before downloading."),
  ).toBeVisible();
  expect(state.previews).toHaveLength(2);
  expect(downloaded).toHaveBeenCalledTimes(1);
});

it("shows the names read-only without the outlet's editor grant", async () => {
  const state = serve();
  render(false);
  expect(await screen.findByText("Sales invoices")).toBeVisible();
  expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Save ledger names" })).not.toBeInTheDocument();
  expect(screen.getByText("Supplier ACME (TEST Acme Pharma)").parentElement).toHaveTextContent(
    "Not named",
  );
  expect(screen.getByText("Counter cash").parentElement).toHaveTextContent("Cash");
  expect(
    screen.getByText("Name a Tally ledger for 1 account head before downloading."),
  ).toBeVisible();
  expect(state.puts).toEqual([]);
});

it("offers no file for a period with nothing to export", async () => {
  const state = serve({ vouchers: 0 });
  render();
  expect(
    await screen.findByText(
      "No sales, credit notes or supplier payments from 2026-11-01 to 2026-11-15.",
    ),
  ).toBeVisible();
  expect(download()).not.toBeInTheDocument();
  expect(state.downloads).toEqual([]);
});

it("previews another period, bounded as the server bounds it", async () => {
  const state = serve();
  const { user } = render();
  await screen.findByText("Sales invoices");
  const from = screen.getByLabelText("From");
  await user.clear(from);
  await user.type(from, "2025-01-01");
  await user.click(screen.getByRole("button", { name: "Preview period" }));
  expect(await screen.findByText("A period is at most 366 days")).toBeVisible();
  await user.clear(from);
  await user.type(from, "2026-10-01");
  await user.click(screen.getByRole("button", { name: "Preview period" }));
  await waitFor(() =>
    expect(state.previews).toEqual([
      `${root}/preview?from=2026-11-01&to=2026-11-15`,
      `${root}/preview?from=2026-10-01&to=2026-11-15`,
    ]),
  );
});
