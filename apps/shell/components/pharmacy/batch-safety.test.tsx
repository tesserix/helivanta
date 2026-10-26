import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useState } from "react";
import userEvent from "@testing-library/user-event";
import { renderWithProviders as render } from "@helivanta/api/testing";
import { NavigationGuardProvider } from "@helivanta/ui";
import { chooseOption } from "../../test/select";
import { BatchSafety } from "./batch-safety";

const scope = {
  tenantId: "11111111-1111-1111-1111-111111111111",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};
const batchId = "0199e000-0000-7000-8000-000000000005";
const productId = "0199e000-0000-7000-8000-000000000003";
const locationId = "0199e000-0000-7000-8000-000000000004";
const incident = {
  id: "0199e000-0000-7000-8000-000000000008",
  outlet_id: scope.outletId,
  batch_id: batchId,
  location_id: null,
  measured_temperature_millicelsius: null,
  kind: "packaging_damage",
  reason: "TEST package seal was damaged on arrival.",
  occurred_at: "2026-10-17T10:00:00.123456+05:30",
  recorded_at: "2026-10-17T11:00:00.123456+05:30",
  actor_subject: "TEST-quality-reviewer",
};
// The batch list lives on its tab; most tests here are about it. A live
// address: the open batch is the URL (#1303 E5).
const nav = vi.hoisted(() => ({
  search: "tab=batch",
  listeners: new Set<() => void>(),
}));
vi.mock("next/navigation", async () => {
  const { useSyncExternalStore } = await import("react");
  const subscribe = (fn: () => void) => {
    nav.listeners.add(fn);
    return () => nav.listeners.delete(fn);
  };
  const go = (href: string) => {
    nav.search = href.split("?")[1] ?? "";
    nav.listeners.forEach((fn) => fn());
  };
  return {
    usePathname: () => "/pharmacy/safety",
    useSearchParams: () => new URLSearchParams(useSyncExternalStore(subscribe, () => nav.search)),
    useRouter: () => ({ replace: go, push: go }),
  };
});
// The page guards an unsaved concern through the shell's provider.
const renderWithProviders = (ui: React.ReactElement) =>
  render(<NavigationGuardProvider>{ui}</NavigationGuardProvider>);
afterEach(() => {
  nav.search = "tab=batch";
});

const page = (data: unknown[], cursor: string | null = null) => ({
  data,
  page: { has_more: cursor !== null, next_cursor: cursor, total: data.length, offset: 0 },
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
});

function fixture(
  options: {
    permissions?: string[];
    permissionTenant?: string;
    batches?: (url: string) => Response;
    products?: (url: string) => Response;
    incidents?: (url: string) => Response;
    recent?: (url: string) => Response;
    save?: (url: string, init: RequestInit) => Response;
  } = {},
) {
  const fetcher = vi.fn(async (url: string, init: RequestInit = {}) => {
    if (url.endsWith("/iam/me/permissions"))
      return new Response(
        JSON.stringify({
          tenant_id:
            options.permissionTenant ??
            new Headers(init.headers).get("X-Helivanta-Expected-Tenant"),
          data: options.permissions ?? [
            "pharmacy.stock.read",
            "pharmacy.stock.quarantine",
            "pharmacy.catalogue.read",
          ],
        }),
      );
    // One batch, read by its id: a reload or a link reopens it (#1303 E5).
    if (/\/stock\/batches\/[0-9a-f-]{36}$/.test(url))
      return new Response(
        JSON.stringify({
          id: batchId,
          product_id: productId,
          lot_number: "TEST-LOT",
          expires_on: "2030-10-31",
        }),
      );
    if (url.includes("/stock/batches"))
      return (
        options.batches?.(url) ??
        new Response(
          JSON.stringify(
            page([
              {
                id: batchId,
                product_id: productId,
                lot_number: "TEST-LOT",
                expires_on: "2030-10-31",
              },
            ]),
          ),
        )
      );
    if (url.includes("/catalogue/products"))
      return (
        options.products?.(url) ??
        new Response(
          JSON.stringify(
            page([
              {
                id: productId,
                sku: "TEST-500",
                brand_name: "TEST capsule",
              },
            ]),
          ),
        )
      );
    if (url.includes("/stock/locations"))
      return new Response(
        JSON.stringify(
          page([
            {
              id: locationId,
              code: "TEST-Q",
              name: "TEST Receiving",
            },
          ]),
        ),
      );
    if (url.includes("/stock/safety-incidents/") && init.method === "PUT")
      return options.save?.(url, init) ?? new Response(JSON.stringify(incident));
    if (url.includes("/stock/safety-incidents/recent"))
      return options.recent?.(url) ?? new Response(JSON.stringify(page([incident])));
    if (url.includes("/stock/safety-incidents?"))
      return options.incidents?.(url) ?? new Response(JSON.stringify(page([incident])));
    throw new Error(`Unexpected test API request: ${url}`);
  });
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}

async function selectBatch(user: ReturnType<typeof renderWithProviders>["user"]) {
  await user.click(await screen.findByRole("button", { name: "Open lot TEST-LOT" }));
}

it("requires an explicit batch and shows immutable concern history without suggesting clearance", async () => {
  const fetcher = fixture();
  const { user } = renderWithProviders(<BatchSafety scope={scope} />);
  await screen.findByRole("list", { name: "Batches at this outlet" });
  expect(fetcher.mock.calls.some(([url]) => url.includes("/stock/safety-incidents"))).toBe(false);
  await selectBatch(user);
  expect(await screen.findByText(incident.reason)).toBeVisible();
  expect(screen.getByRole("heading", { name: "Safety history · TEST-LOT" })).toHaveFocus();
  const occurred = screen.getByTitle(incident.occurred_at);
  expect(occurred).toHaveAttribute("datetime", incident.occurred_at);
  expect(occurred).toHaveTextContent(/04:30:00\.123456/);
  expect(occurred).not.toHaveTextContent(incident.occurred_at);
  expect(screen.getByTitle(incident.recorded_at)).toHaveAttribute("datetime", incident.recorded_at);
  expect(screen.getByText(/does not verify a recall or release stock/i)).toBeVisible();
  expect(screen.queryByRole("button", { name: /release|resolve|clear/i })).not.toBeInTheDocument();
  for (const [, init] of fetcher.mock.calls) {
    expect(new Headers(init?.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
    expect(init?.cache).toBe("no-store");
  }
});

it("retries an ambiguous write with the same identity and exact submitted payload", async () => {
  let attempts = 0;
  const fetcher = fixture({
    save: () => {
      attempts += 1;
      return attempts === 1
        ? new Response(
            JSON.stringify({ error: "unavailable", message: "TEST connection interrupted" }),
            { status: 503 },
          )
        : new Response(JSON.stringify(incident));
    },
  });
  const { user } = renderWithProviders(<BatchSafety scope={scope} />);
  await selectBatch(user);
  await chooseOption(
    user,
    await screen.findByRole("combobox", { name: "Concern type" }),
    "Temperature excursion",
  );
  await user.type(
    screen.getByLabelText("Reason and observed evidence"),
    "  TEST fridge sensor recorded a high temperature.  ",
  );
  fireEvent.change(screen.getByLabelText("Occurred at (UTC)"), {
    target: { value: "2026-10-17T09:30" },
  });
  await user.type(screen.getByLabelText("Measured temperature (°C)"), "8.125");
  await user.click(screen.getByRole("button", { name: "Record safety concern" }));
  expect(await screen.findByText("TEST connection interrupted")).toBeVisible();
  expect(screen.getByLabelText("Reason and observed evidence")).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Retry same concern" }));
  expect(
    await screen.findByText("Safety concern recorded. Stock remains quarantined."),
  ).toHaveFocus();
  const writes = fetcher.mock.calls.filter(([, init]) => init?.method === "PUT");
  expect(writes).toHaveLength(2);
  expect(writes[0]?.[0]).toBe(writes[1]?.[0]);
  expect(writes[0]?.[1]?.body).toBe(writes[1]?.[1]?.body);
  expect(JSON.parse(writes[0]?.[1]?.body as string)).toEqual({
    batch_id: batchId,
    kind: "temperature_excursion",
    reason: "TEST fridge sensor recorded a high temperature.",
    occurred_at: "2026-10-17T09:30:00.000Z",
    measured_temperature_millicelsius: 8125,
  });
  expect(screen.getByLabelText("Reason and observed evidence")).toHaveValue("");
});

it("allows read-only auditors to inspect concerns without a write form", async () => {
  const fetcher = fixture({ permissions: ["pharmacy.stock.read"] });
  const { user } = renderWithProviders(<BatchSafety scope={scope} />);
  await selectBatch(user);
  expect(await screen.findByText(incident.reason)).toBeVisible();
  expect(screen.queryByRole("button", { name: "Record safety concern" })).not.toBeInTheDocument();
  expect(fetcher.mock.calls.some(([url]) => url.includes("/catalogue/products"))).toBe(false);
});

it("keeps authorized safety history usable if optional product details are unavailable", async () => {
  fixture({
    products: () =>
      new Response(
        JSON.stringify({ error: "forbidden", message: "TEST catalogue access removed" }),
        { status: 403 },
      ),
  });
  const { user } = renderWithProviders(<BatchSafety scope={scope} />);
  await selectBatch(user);
  expect(await screen.findByText(incident.reason)).toBeVisible();
  expect(screen.getByRole("list", { name: "Batches at this outlet" })).toHaveTextContent(
    `Product ${productId}`,
  );
  expect(screen.getByText(/product names are unavailable/i)).toBeVisible();
});

it("lets users correct a definite rejected request while retaining its identity", async () => {
  let attempts = 0;
  const fetcher = fixture({
    save: () => {
      attempts += 1;
      return attempts === 1
        ? new Response(
            JSON.stringify({ error: "invalid", message: "TEST occurred time is in the future" }),
            { status: 400 },
          )
        : new Response(JSON.stringify(incident));
    },
  });
  const { user } = renderWithProviders(<BatchSafety scope={scope} />);
  await selectBatch(user);
  await user.type(
    await screen.findByLabelText("Reason and observed evidence"),
    "TEST observed seal damage.",
  );
  await user.click(screen.getByRole("button", { name: "Record safety concern" }));
  await screen.findByText("TEST occurred time is in the future");
  expect(screen.getByLabelText("Occurred at (UTC)")).not.toBeDisabled();
  fireEvent.change(screen.getByLabelText("Occurred at (UTC)"), {
    target: { value: "2026-10-17T09:30" },
  });
  await user.click(screen.getByRole("button", { name: "Record safety concern" }));
  await screen.findByText("Safety concern recorded. Stock remains quarantined.");
  const writes = fetcher.mock.calls.filter(([, init]) => init?.method === "PUT");
  expect(writes).toHaveLength(2);
  expect(writes[0]?.[0]).toBe(writes[1]?.[0]);
  expect(JSON.parse(writes[1]?.[1]?.body as string).occurred_at).toBe("2026-10-17T09:30:00.000Z");
});

it("hides all history and the form after refresh is denied, then recovers explicitly", async () => {
  let reads = 0;
  fixture({
    incidents: () => {
      reads += 1;
      return reads === 2
        ? new Response(JSON.stringify({ error: "forbidden", message: "TEST access removed" }), {
            status: 403,
          })
        : new Response(JSON.stringify(page([incident])));
    },
  });
  const { user } = renderWithProviders(<BatchSafety scope={scope} />);
  await selectBatch(user);
  expect(await screen.findByText(incident.reason)).toBeVisible();
  await user.type(screen.getByLabelText("Reason and observed evidence"), "TEST private draft");
  await user.click(screen.getByRole("button", { name: "Refresh safety history" }));
  expect(await screen.findByText(/could not load this batch's safety history/i)).toBeVisible();
  expect(screen.queryByText(incident.reason)).not.toBeInTheDocument();
  expect(
    screen.queryByRole("form", { name: "Record batch safety concern" }),
  ).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Try again" }));
  expect(await screen.findByText(incident.reason)).toBeVisible();
  expect(screen.getByLabelText("Reason and observed evidence")).toHaveValue("");
});

it("pages concern history using the opaque cursor and required batch scope", async () => {
  const second = {
    ...incident,
    id: "0199e000-0000-7000-8000-000000000009",
    reason: "TEST second package inspection concern.",
  };
  const fetcher = fixture({
    incidents: (url) =>
      new Response(
        JSON.stringify(
          new URL(url, "http://localhost").searchParams.has("cursor")
            ? page([second])
            : page([incident], "TEST/cursor+opaque="),
        ),
      ),
  });
  const { user } = renderWithProviders(<BatchSafety scope={scope} />);
  await selectBatch(user);
  const history = await screen.findByRole("region", { name: "Safety history for TEST-LOT" });
  await user.click(within(history).getByRole("button", { name: "Load more concerns" }));
  expect(await screen.findByText(second.reason)).toBeVisible();
  expect(screen.getByText(incident.reason)).toBeVisible();
  expect(
    within(history).queryByRole("button", { name: "Load more concerns" }),
  ).not.toBeInTheDocument();
  const call = fetcher.mock.calls.find(([url]) => url.includes("cursor="));
  const params = new URL(call![0], "http://localhost").searchParams;
  expect(params.get("cursor")).toBe("TEST/cursor+opaque=");
  expect(params.get("batch_id")).toBe(batchId);
  expect(params.get("limit")).toBe("25");
});

it("rejects invalid evidence and temperature, and removes temperature for a different concern kind", async () => {
  const fetcher = fixture();
  const { user } = renderWithProviders(<BatchSafety scope={scope} />);
  await selectBatch(user);
  await user.click(await screen.findByRole("button", { name: "Record safety concern" }));
  expect(await screen.findByText(/at least 10 character/i)).toBeVisible();
  expect(fetcher.mock.calls.some(([, init]) => init?.method === "PUT")).toBe(false);
  await user.type(
    screen.getByLabelText("Reason and observed evidence"),
    "TEST observed packaging concern",
  );
  await chooseOption(
    user,
    screen.getByRole("combobox", { name: "Concern type" }),
    "Temperature excursion",
  );
  await user.click(screen.getByRole("button", { name: "Record safety concern" }));
  expect(
    await screen.findByText("Record a temperature only for a temperature excursion."),
  ).toBeVisible();
  await user.type(screen.getByLabelText("Measured temperature (°C)"), "-274");
  await user.click(screen.getByRole("button", { name: "Record safety concern" }));
  expect(fetcher.mock.calls.some(([, init]) => init?.method === "PUT")).toBe(false);
  await chooseOption(
    user,
    screen.getByRole("combobox", { name: "Concern type" }),
    "Packaging damage",
  );
  expect(screen.queryByLabelText("Measured temperature (°C)")).not.toBeInTheDocument();
  await chooseOption(
    user,
    screen.getByRole("combobox", { name: "Concern location" }),
    "TEST-Q · TEST Receiving · Address not recorded",
  );
  await user.click(screen.getByRole("button", { name: "Record safety concern" }));
  await screen.findByText("Safety concern recorded. Stock remains quarantined.");
  const write = fetcher.mock.calls.find(([, init]) => init?.method === "PUT");
  expect(JSON.parse(write?.[1]?.body as string)).toEqual(
    expect.objectContaining({
      kind: "packaging_damage",
      location_id: locationId,
    }),
  );
  expect(JSON.parse(write?.[1]?.body as string)).not.toHaveProperty(
    "measured_temperature_millicelsius",
  );
});

it("fails closed on a permission response from a different tenant", async () => {
  const fetcher = fixture({ permissionTenant: "22222222-2222-2222-2222-222222222222" });
  renderWithProviders(<BatchSafety scope={scope} />);
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Safety access does not match the active tenant",
  );
  expect(fetcher.mock.calls).toHaveLength(1);
  expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
});

it("does not fetch stock options without stock read access", async () => {
  const fetcher = fixture({ permissions: ["pharmacy.stock.quarantine"] });
  renderWithProviders(<BatchSafety scope={scope} />);
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "You do not have permission to view batch safety concerns.",
  );
  expect(fetcher.mock.calls).toHaveLength(1);
});

it("clears selected batch and private draft if loading more batch options fails", async () => {
  let calls = 0;
  fixture({
    batches: (url) => {
      calls += 1;
      if (new URL(url, "http://localhost").searchParams.has("cursor"))
        return new Response(JSON.stringify({ error: "forbidden", message: "TEST denied" }), {
          status: 403,
        });
      return new Response(
        JSON.stringify(
          page(
            [
              {
                id: batchId,
                product_id: productId,
                lot_number: "TEST-LOT",
                expires_on: "2030-10-31",
              },
            ],
            calls === 1 ? "TEST-next-batches" : null,
          ),
        ),
      );
    },
  });
  const { user } = renderWithProviders(<BatchSafety scope={scope} />);
  await selectBatch(user);
  await screen.findByText(incident.reason);
  await user.type(screen.getByLabelText("Reason and observed evidence"), "TEST hidden draft");
  await user.click(screen.getByRole("button", { name: "Load more batches" }));
  await screen.findByText(/could not load safety batch options/i);
  expect(screen.queryByText(incident.reason)).not.toBeInTheDocument();
  expect(screen.queryByRole("list", { name: "Batches at this outlet" })).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Try again" }));
  // The batch is still the address's, so it reopens, without the hidden draft.
  await screen.findByText(incident.reason);
  expect(screen.getByLabelText("Reason and observed evidence")).toHaveValue("");
});

it.each(["tenantId", "pharmacyId", "outletId"] as const)(
  "clears selected batch, records and draft on a %s switch",
  async (field) => {
    fixture();
    function Harness() {
      const [current, setCurrent] = useState(scope);
      return (
        <>
          <button
            onClick={() => {
              // As the workspace does (#1303 E8): another outlet closes the record.
              nav.search = "tab=batch";
              setCurrent({ ...scope, [field]: "0199e000-0000-7000-8000-000000000099" });
            }}
          >
            Switch scope
          </button>
          <BatchSafety scope={current} />
        </>
      );
    }
    const { user } = renderWithProviders(<Harness />);
    await selectBatch(user);
    await screen.findByText(incident.reason);
    await user.type(screen.getByLabelText("Reason and observed evidence"), "TEST old scope draft");
    await user.click(screen.getByRole("button", { name: "Switch scope" }));
    await waitFor(() => expect(screen.queryByText(incident.reason)).not.toBeInTheDocument());
    await screen.findByRole("list", { name: "Batches at this outlet" });
    expect(screen.queryByRole("region", { name: "Batch TEST-LOT" })).toBeNull();
    expect(screen.queryByLabelText("Reason and observed evidence")).not.toBeInTheDocument();
    await selectBatch(user);
    await screen.findByText(incident.reason);
    expect(screen.getByLabelText("Reason and observed evidence")).toHaveValue("");
  },
);

it.each([
  ["Asia/Kolkata", "2026-10-17T10:00", "2026-10-17T04:30:00.000Z", "10:00:00.123456"],
  ["Asia/Tashkent", "2026-10-17T09:30", "2026-10-17T04:30:00.000Z", "09:30:00.123456"],
])(
  "uses persisted %s for entry, display and UTC submission independently of browser timezone",
  async (timeZone, local, instant, historyTime) => {
    vi.stubEnv("TZ", "Pacific/Honolulu");
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date("2026-10-17T04:30:00.000Z"));
    const fetcher = fixture();
    const { user } = renderWithProviders(<BatchSafety scope={scope} timeZone={timeZone} />);
    await selectBatch(user);
    const date = await screen.findByLabelText(`Occurred at (${timeZone})`);
    expect(date).toHaveValue(local);
    expect(screen.getByTitle(incident.occurred_at)).toHaveTextContent(historyTime);
    expect(screen.getByText(`Times shown in ${timeZone}.`)).toBeVisible();
    await user.type(
      screen.getByLabelText("Reason and observed evidence"),
      "TEST observed damaged package seal.",
    );
    fireEvent.change(date, { target: { value: local } });
    await user.click(screen.getByRole("button", { name: "Record safety concern" }));
    await screen.findByText("Safety concern recorded. Stock remains quarantined.");
    const write = fetcher.mock.calls.find(([, init]) => init?.method === "PUT");
    expect(JSON.parse(write?.[1]?.body as string).occurred_at).toBe(instant);
  },
);

it.each(["2026-03-08T02:30", "2026-11-01T01:30"])(
  "rejects nonexistent or ambiguous customer local time %s with useful guidance",
  async (local) => {
    const fetcher = fixture();
    const { user } = renderWithProviders(<BatchSafety scope={scope} timeZone="America/New_York" />);
    await selectBatch(user);
    await user.type(
      await screen.findByLabelText("Reason and observed evidence"),
      "TEST observed damaged package seal.",
    );
    fireEvent.change(screen.getByLabelText("Occurred at (America/New_York)"), {
      target: { value: local },
    });
    await user.click(screen.getByRole("button", { name: "Record safety concern" }));
    expect(await screen.findByText(/may not exist or may occur twice/i)).toBeVisible();
    expect(screen.getByLabelText("Occurred at (America/New_York)")).toHaveFocus();
    expect(fetcher.mock.calls.some(([, init]) => init?.method === "PUT")).toBe(false);
  },
);

it("clears batch selection and draft when the configured outlet timezone changes", async () => {
  fixture();
  function Harness() {
    const [timeZone, setTimeZone] = useState("Asia/Kolkata");
    return (
      <>
        <button onClick={() => setTimeZone("Asia/Tashkent")}>Change outlet timezone</button>
        <BatchSafety scope={scope} timeZone={timeZone} />
      </>
    );
  }
  const { user } = renderWithProviders(<Harness />);
  await selectBatch(user);
  await user.type(
    await screen.findByLabelText("Reason and observed evidence"),
    "TEST old timezone draft",
  );
  await user.click(screen.getByRole("button", { name: "Change outlet timezone" }));
  // The batch stays the address's; the record is read again in the new zone,
  // and the old zone's draft is gone.
  expect(await screen.findByLabelText("Occurred at (Asia/Tashkent)")).toBeVisible();
  expect(screen.queryByLabelText("Occurred at (Asia/Kolkata)")).not.toBeInTheDocument();
  expect(screen.getByLabelText("Reason and observed evidence")).toHaveValue("");
});

it.each(["", "Not/A_Timezone"])(
  "denies invalid configured timezone %j without requests or a form",
  async (timeZone) => {
    const fetcher = fixture();
    renderWithProviders(<BatchSafety scope={scope} timeZone={timeZone} />);
    expect(await screen.findByRole("alert")).toHaveTextContent(
      /ask an administrator to update the outlet timezone/i,
    );
    expect(fetcher).not.toHaveBeenCalled();
    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
  },
);

// #576 slice 3: the outlet's last 30 days is a tab, paged with its total.
describe("safety tabs", () => {
  it("opens on the last 30 days, counted and 25 a page", async () => {
    nav.search = "";
    const fetcher = fixture();
    renderWithProviders(<BatchSafety scope={scope} />);
    expect(await screen.findByText(incident.reason)).toBeVisible();
    const recent = fetcher.mock.calls
      .map(([url]) => String(url))
      .filter((u) => u.includes("/recent"));
    expect(recent).toEqual([expect.stringMatching(/\/stock\/safety-incidents\/recent\?limit=25$/)]);
    expect(screen.getByRole("link", { name: "Last 30 days" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(screen.getByText(/1–1 of 1/)).toBeInTheDocument();
    expect(
      screen.queryByRole("combobox", { name: "Safety concern batch" }),
    ).not.toBeInTheDocument();
    for (const [, init] of fetcher.mock.calls)
      expect(new Headers(init?.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  });

  // The overview's link from before the tabs lands on the same list.
  it("reads the old recorded=30d link as the last 30 days", async () => {
    nav.search = "recorded=30d";
    fixture();
    renderWithProviders(<BatchSafety scope={scope} />);
    expect(await screen.findByRole("link", { name: "Last 30 days" })).toHaveAttribute(
      "aria-current",
      "page",
    );
  });

  it("links the batch tab, which holds the picker", async () => {
    nav.search = "page=2&cursor=c2";
    fixture();
    renderWithProviders(<BatchSafety scope={scope} />);
    expect(await screen.findByRole("link", { name: "By batch" })).toHaveAttribute(
      "href",
      "/pharmacy/safety?tab=batch",
    );
  });
});

// #1303 E5: a batch opens beside its list, from either tab, and the URL holds it.
describe("a batch opens beside the list", () => {
  const otherBatch = "0199e000-0000-7000-8000-00000000000d";
  const third = "0199e000-0000-7000-8000-00000000000e";
  it("reopens the batch in the address, read by its id", async () => {
    nav.search = `tab=batch&open=${batchId}`;
    const fetcher = fixture();
    renderWithProviders(<BatchSafety scope={scope} />);
    const pane = await screen.findByRole("region", { name: "Batch TEST-LOT" });
    expect(await within(pane).findByText(incident.reason)).toBeVisible();
    expect(
      fetcher.mock.calls.some(([url]) => String(url).endsWith(`/stock/batches/${batchId}`)),
    ).toBe(true);
  });

  it("says a batch that is not found is not found, and can be closed", async () => {
    nav.search = `tab=batch&open=${otherBatch}`;
    const base = fixture();
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init?: RequestInit) =>
        url.endsWith(`/stock/batches/${otherBatch}`)
          ? new Response(JSON.stringify({ error: "not_found", message: "x" }), { status: 404 })
          : base(url, init),
      ),
    );
    const { user } = renderWithProviders(<BatchSafety scope={scope} />);
    expect(
      await screen.findByText("This record was not found, or you do not have access."),
    ).toBeInTheDocument();
    expect(screen.queryByLabelText("Reason and observed evidence")).toBeNull();
    await user.click(screen.getByRole("button", { name: "Close batch" }));
    expect(new URLSearchParams(nav.search).has("open")).toBe(false);
  });

  it("asks before Next discards an unsaved concern", async () => {
    fixture({
      batches: () =>
        new Response(
          JSON.stringify(
            page([
              {
                id: batchId,
                product_id: productId,
                lot_number: "TEST-LOT",
                expires_on: "2030-10-31",
              },
              {
                id: otherBatch,
                product_id: productId,
                lot_number: "TEST-LOT-2",
                expires_on: "2031-01-31",
              },
            ]),
          ),
        ),
    });
    const { user } = renderWithProviders(<BatchSafety scope={scope} />);
    await selectBatch(user);
    await user.type(
      await screen.findByLabelText("Reason and observed evidence"),
      "TEST unsaved observation",
    );
    await user.click(screen.getByRole("button", { name: "Next batch" }));
    expect(await screen.findByRole("dialog")).toHaveTextContent("Discard changes to TEST-LOT?");
    await user.click(screen.getByRole("button", { name: "Keep editing" }));
    expect(screen.getByLabelText("Reason and observed evidence")).toHaveValue(
      "TEST unsaved observation",
    );
  });

  it("steps over a second concern about the same lot on Last 30 days", async () => {
    nav.search = "";
    const concern = (id: string, batch: string) => ({ ...incident, id, batch_id: batch });
    fixture({
      recent: () =>
        new Response(
          JSON.stringify(
            page([
              concern("0199e000-0000-7000-8000-000000000101", batchId),
              concern("0199e000-0000-7000-8000-000000000102", batchId),
              concern("0199e000-0000-7000-8000-000000000103", third),
            ]),
          ),
        ),
    });
    const { user } = renderWithProviders(<BatchSafety scope={scope} />);
    const opens = await screen.findAllByRole("button", { name: /^Open batch for / });
    await user.click(opens[0]);
    await user.click(await screen.findByRole("button", { name: "Next batch" }));
    expect(new URLSearchParams(nav.search).get("open")).toBe(third);
    expect(screen.getByRole("button", { name: "Next batch" })).toBeDisabled();
  });

  it("closes the batch when the tab changes", async () => {
    nav.search = `tab=batch&open=${batchId}`;
    fixture();
    renderWithProviders(<BatchSafety scope={scope} />);
    await screen.findByRole("region", { name: "Batch TEST-LOT" });
    expect(screen.getByRole("link", { name: "Last 30 days" })).toHaveAttribute(
      "href",
      "/pharmacy/safety?tab=recent",
    );
  });

  // Review Important 1: the tabs sat outside the pane's link hold, so a
  // half-typed concern went without a word.
  it("asks before the Last 30 days tab discards an unsaved concern", async () => {
    nav.search = `tab=batch&open=${batchId}`;
    fixture();
    const { user } = renderWithProviders(<BatchSafety scope={scope} />);
    await user.type(
      await screen.findByLabelText("Reason and observed evidence"),
      "TEST unsaved observation",
    );
    fireEvent.click(screen.getByRole("link", { name: "Last 30 days" }));
    expect(await screen.findByRole("dialog")).toHaveTextContent("Discard changes to TEST-LOT?");
    await userEvent.click(screen.getByRole("button", { name: "Keep editing" }));
    expect(screen.getByLabelText("Reason and observed evidence")).toHaveValue(
      "TEST unsaved observation",
    );
    expect(new URLSearchParams(nav.search).get("tab")).toBe("batch");
  });
});
