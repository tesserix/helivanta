import { act, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { NavigationGuardProvider, useConfirmLeave, useLeaveGuard } from "@helivanta/ui";
import { chooseOption } from "../../test/select";
import { PharmacyWorkspace } from "./pharmacy-workspace";

const pharmacy = "0199e000-0000-7000-8000-000000000010";
const outlet = "0199e000-0000-7000-8000-000000000001";
const sibling = "0199e000-0000-7000-8000-000000000002";
const product = {
  id: "0199e000-0000-7000-8000-000000000003",
  outlet_id: outlet,
  sku: "TEST-500",
  brand_name: "TEST Counter capsule",
  generic_name: "TEST molecule",
  dosage_form: "capsule",
  route: "oral",
  manufacturer: "TEST Manufacturer",
  marketing_company: "",
  ingredients: [{ name: "TEST molecule", strength: "500", unit: "mg" }],
  pack_size: 10,
  unit_of_issue: "capsule",
  currency: "INR",
  mrp_minor: 12500,
  status: "active",
  version: 1,
  regulatory_status: "pending_review",
  created_at: "2026-10-15T00:00:00Z",
  updated_at: "2026-10-15T00:00:00Z",
};
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
// The page's starting filter comes from the URL (#576 D4); tests set it here.
// A live address: a replace is read back, as the router would (#1303 E8).
const nav = vi.hoisted(() => ({
  search: "",
  listeners: new Set<() => void>(),
  calls: [] as { method: "push" | "replace"; href: string }[],
}));
vi.mock("next/navigation", async (original) => {
  const { useSyncExternalStore } = await import("react");
  const subscribe = (fn: () => void) => {
    nav.listeners.add(fn);
    return () => nav.listeners.delete(fn);
  };
  const go = (method: "push" | "replace") => (href: string) => {
    nav.calls.push({ method, href });
    window.history[method === "push" ? "pushState" : "replaceState"](null, "", href);
  };
  return {
    ...(await original<typeof import("next/navigation")>()),
    useSearchParams: () => new URLSearchParams(useSyncExternalStore(subscribe, () => nav.search)),
    usePathname: () => "/pharmacy",
    useRouter: () => ({ push: go("push"), replace: go("replace") }),
  };
});
const address = () => new URLSearchParams(nav.search);
// A rail link: the same page, its address without any scope.
const followRail = () => act(() => window.history.pushState(null, "", "/pharmacy"));
const renderWorkspace = (view: Parameters<typeof PharmacyWorkspace>[0]["view"]) => {
  const current = new URL(window.location.href);
  current.pathname = "/pharmacy";
  current.search = nav.search;
  window.history.replaceState(null, "", current);
  return renderWithProviders(
    <NavigationGuardProvider>
      <PharmacyWorkspace view={view} />
    </NavigationGuardProvider>,
  );
};
beforeEach(() => {
  for (const method of ["pushState", "replaceState"] as const) {
    const original = window.history[method].bind(window.history);
    vi.spyOn(window.history, method).mockImplementation((data, unused, url) => {
      original(data, unused, url);
      nav.search = new URL(window.location.href).search.slice(1);
      nav.listeners.forEach((listener) => listener());
    });
  }
  window.history.replaceState(null, "", "/pharmacy");
});
afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  sessionStorage.clear();
  nav.search = "";
  nav.calls.length = 0;
});
// denied: the status the access check answers with, or 0 for a grant.
function mockPharmacy(
  level = "editor",
  denied: 0 | 404 | 503 = 0,
  country = "IN",
  extra: string[] = [],
) {
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    if (init?.method && init.method !== "GET")
      return new Response(JSON.stringify({ message: "Unexpected test mutation" }), { status: 405 });
    if (url.endsWith("permissions"))
      return new Response(
        JSON.stringify({
          tenant_id: "11111111-1111-1111-1111-111111111111",
          subject: "TEST-editor",
          data: [
            "pharmacy.catalogue.read",
            "pharmacy.catalogue.write",
            "pharmacy.stock.read",
            "pharmacy.stock.receive",
            ...extra,
          ],
        }),
      );
    if (url.endsWith("/pharmacy/pharmacies"))
      return new Response(
        JSON.stringify(
          page([
            {
              id: pharmacy,
              code: "TEST-INDEPENDENT",
              name: "TEST Independent Pharmacy",
              ownership_type: "independent",
              onboarding_status: "draft",
            },
          ]),
        ),
      );
    if (url.endsWith(`/pharmacies/${pharmacy}/outlets`))
      return new Response(
        JSON.stringify(
          page([
            {
              id: outlet,
              code: "TEST-MAIN",
              name: "TEST Main Outlet",
              timezone: "Asia/Kolkata",
              country_code: country,
              access_level: "manager",
            },
            {
              id: sibling,
              code: "TEST-SISTER",
              name: "TEST Sister Outlet",
              timezone: "Asia/Tashkent",
              country_code: country,
              access_level: "manager",
            },
          ]),
        ),
      );
    if (url.endsWith("/access"))
      return new Response(
        JSON.stringify(
          denied
            ? { error: denied === 404 ? "not_found" : "authorization_unavailable", message: "x" }
            : { level },
        ),
        { status: denied || 200 },
      );
    if (url.endsWith("/revisions")) return new Response(JSON.stringify(page([])));
    // One product, read by its id: a link or a reload reopens it (#1303 E2).
    if (/\/catalogue\/products\/[0-9a-f-]{36}$/.test(url))
      return new Response(
        JSON.stringify(url.includes(sibling) ? { ...product, outlet_id: sibling } : product),
      );
    if (url.includes("/catalogue/products"))
      return new Response(JSON.stringify(page(url.includes(sibling) ? [] : [product])));
    return new Response(JSON.stringify(page([])));
  });
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}

it("shows private catalogue identity and clears rows when switching outlet", async () => {
  const fetcher = mockPharmacy();
  const { user } = renderWorkspace("catalogue");
  expect(await screen.findByText("TEST Counter capsule")).toBeInTheDocument();
  expect(screen.getByText("Pending regulatory review")).toBeInTheDocument();
  await chooseOption(user, screen.getByLabelText("Outlet"), "TEST Sister Outlet");
  await waitFor(() => expect(screen.queryByText("TEST Counter capsule")).not.toBeInTheDocument());
  expect(await screen.findByText("No products yet")).toBeInTheDocument();
  expect(
    fetcher.mock.calls.some(([url]) =>
      url.includes(`/pharmacies/${pharmacy}/outlets/${sibling}/catalogue/products`),
    ),
  ).toBe(true);
});

it("uses each configured outlet timezone for revision history and clears the previous outlet details", async () => {
  const base = mockPharmacy();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith(`/pharmacies/${pharmacy}/outlets`))
        return new Response(
          JSON.stringify(
            page([
              {
                id: outlet,
                name: "TEST Main Outlet",
                country_code: "IN",
                timezone: "Asia/Kolkata",
              },
              {
                id: sibling,
                name: "TEST Sister Outlet",
                country_code: "IN",
                timezone: "Asia/Tashkent",
              },
            ]),
          ),
        );
      if (url.endsWith("/revisions"))
        return new Response(
          JSON.stringify(
            page([
              {
                id: "TEST-revision",
                version: 1,
                created_at: "2026-10-17T00:00:00Z",
                actor_subject: "TEST-editor",
                snapshot: product,
              },
            ]),
          ),
        );
      if (url.includes(sibling) && url.includes("/catalogue/products/counted"))
        return new Response(JSON.stringify(page([{ ...product, outlet_id: sibling }])));
      return base(url, init);
    }),
  );
  const { user } = renderWorkspace("catalogue");
  await user.click(await screen.findByRole("button", { name: "Open TEST-500" }));
  expect(screen.getByText("Times are in Asia/Kolkata.")).toBeInTheDocument();
  const firstTime = await screen.findByText("2026-10-17 05:30:00 GMT+05:30");
  expect(firstTime).toHaveAttribute("datetime", "2026-10-17T00:00:00Z");
  await chooseOption(user, screen.getByLabelText("Outlet"), "TEST Sister Outlet");
  expect(await screen.findByText("Times are in Asia/Tashkent.")).toBeInTheDocument();
  expect(screen.queryByText("Revision history")).not.toBeInTheDocument();
  expect(screen.queryByText("2026-10-17 05:30:00 GMT+05:30")).not.toBeInTheDocument();
  await user.click(await screen.findByRole("button", { name: "Open TEST-500" }));
  expect(await screen.findByText("2026-10-17 05:00:00 GMT+05:00")).toHaveAttribute(
    "datetime",
    "2026-10-17T00:00:00Z",
  );
});

it("propagates the configured timezone through receiving to the stock register on outlet switches", async () => {
  mockPharmacy();
  const { user } = renderWorkspace("stock");
  const register = await screen.findByRole("region", { name: "Stock register" });
  expect(within(register).getByText(/\(Asia\/Kolkata\)/)).toBeInTheDocument();
  await chooseOption(user, screen.getByLabelText("Outlet"), "TEST Sister Outlet");
  const nextRegister = await screen.findByRole("region", { name: "Stock register" });
  expect(within(nextRegister).getByText(/\(Asia\/Tashkent\)/)).toBeInTheDocument();
  expect(within(nextRegister).queryByText(/\(Asia\/Kolkata\)/)).not.toBeInTheDocument();
});

it("denies missing outlet grants and hides writes from outlet readers", async () => {
  mockPharmacy("reader");
  renderWorkspace("catalogue");
  expect(await screen.findByText("TEST Counter capsule")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Add product" })).not.toBeInTheDocument();
});

it("does not load products after outlet access is denied", async () => {
  const fetcher = mockPharmacy("editor", 404);
  renderWorkspace("catalogue");
  expect(await screen.findByText("You don't have access to this outlet yet")).toBeVisible();
  expect(fetcher.mock.calls.some(([url]) => url.includes("/catalogue/products"))).toBe(false);
});

// #1285: no grant is not a failure, so it offers no retry.
it("says a missing grant is missing and points a grant manager to Setup", async () => {
  mockPharmacy("editor", 404, "IN", ["pharmacy.access.manage"]);
  renderWorkspace("catalogue");
  expect(await screen.findByText("You don't have access to this outlet yet")).toBeVisible();
  // Straight to this outlet's pane, where the grant form is (#1303 E6).
  expect(screen.getByRole("link", { name: "Grant access in Setup" })).toHaveAttribute(
    "href",
    `/pharmacy/setup?pharmacy=${pharmacy}&outlet=${outlet}&open=${outlet}`,
  );
  expect(screen.queryByRole("button", { name: "Try again" })).toBeNull();
});

it("tells someone who cannot grant access to ask an administrator", async () => {
  mockPharmacy("editor", 404);
  renderWorkspace("catalogue");
  expect(await screen.findByText(/Ask your administrator to grant you access/)).toBeVisible();
  expect(screen.queryByRole("link", { name: "Grant access in Setup" })).toBeNull();
  expect(screen.queryByRole("button", { name: "Try again" })).toBeNull();
});

it("keeps retry when the access check itself fails", async () => {
  mockPharmacy("editor", 503);
  renderWorkspace("catalogue");
  expect(await screen.findByRole("alert")).toHaveTextContent("Could not check your access");
  expect(screen.getByRole("button", { name: "Try again" })).toBeVisible();
  expect(screen.queryByText("You don't have access to this outlet yet")).toBeNull();
});

it("focuses the new-product form and keeps a UUIDv7 stable across a failed save", async () => {
  const base = mockPharmacy();
  let attempts = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "PUT" && url.includes("/catalogue/products/")) {
        attempts++;
        if (attempts === 1)
          return new Response(
            JSON.stringify({ error: "unavailable", message: "TEST temporary failure" }),
            { status: 503 },
          );
        const values = JSON.parse(init.body as string);
        return new Response(
          JSON.stringify({ ...product, ...values, id: url.split("/").at(-1), version: 1 }),
        );
      }
      return base(url, init);
    }),
  );
  Element.prototype.scrollIntoView = vi.fn();
  const { user } = renderWorkspace("catalogue");
  await user.click(await screen.findByRole("button", { name: "Add product" }));
  await waitFor(() => expect(screen.getByLabelText("Local SKU")).toHaveFocus());
  for (const [label, value] of [
    ["Local SKU", "TEST-NEW"],
    ["Brand or preparation name", "TEST New capsule"],
    ["Generic / composition name", "TEST New molecule"],
    ["Manufacturer", "TEST Manufacturer"],
    ["Ingredient 1", "TEST New molecule"],
    ["Strength", "250"],
  ]) {
    await user.type(screen.getByLabelText(label), value);
  }
  await user.clear(screen.getByLabelText("Pack MRP (INR)"));
  await user.type(screen.getByLabelText("Pack MRP (INR)"), "125.25");
  await user.click(screen.getByRole("button", { name: "Save product" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("TEST temporary failure");
  await user.click(screen.getByRole("button", { name: "Save product" }));
  await waitFor(() => expect(attempts).toBe(2));
  const calls = (fetch as ReturnType<typeof vi.fn>).mock.calls.filter(
    ([url, init]) => String(url).includes("/catalogue/products/") && init?.method === "PUT",
  );
  expect(calls[0][0]).toBe(calls[1][0]);
  expect(calls[0][0]).toMatch(/\/products\/[0-9a-f-]{14}7[0-9a-f-]{21}$/);
  expect(JSON.parse(calls[0][1].body).expected_version).toBe(0);
  expect(JSON.parse(calls[0][1].body).mrp_minor).toBe(12525);
  expect(await screen.findByText("Revision history")).toBeInTheDocument();
});

it("edits only catalogue input fields and displays a stale-version conflict", async () => {
  const base = mockPharmacy();
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    if (url.endsWith(`/products/${product.id}`) && init?.method === "PUT")
      return new Response(
        JSON.stringify({ error: "conflict", message: "Product changed; reload before saving" }),
        { status: 409 },
      );
    if (url.endsWith("/revisions")) return new Response(JSON.stringify(page([])));
    return base(url, init);
  });
  vi.stubGlobal("fetch", fetcher);
  const { user } = renderWorkspace("catalogue");
  await user.click(await screen.findByRole("button", { name: "Open TEST-500" }));
  await user.click(screen.getByRole("button", { name: "Save product" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Product changed");
  const call = fetcher.mock.calls.find(([, init]) => init?.method === "PUT");
  const body = JSON.parse(call?.[1]?.body as string);
  expect(body.expected_version).toBe(1);
  expect(body).not.toHaveProperty("outlet_id");
  expect(body).not.toHaveProperty("regulatory_status");
});

it("shows quarantine on-hand separately and records a receipt against selected references", async () => {
  const base = mockPharmacy();
  const location = "0199e000-0000-7000-8000-000000000004";
  const batch = "0199e000-0000-7000-8000-000000000005";
  let saved = false;
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    if (url.includes("/stock/locations"))
      return new Response(
        JSON.stringify(
          page([
            {
              id: location,
              outlet_id: outlet,
              code: "TEST-Q",
              name: "TEST Quarantine",
              stock_status: "quarantine",
            },
          ]),
        ),
      );
    if (url.includes("/stock/batches"))
      return new Response(
        JSON.stringify(
          page([
            {
              id: batch,
              outlet_id: outlet,
              product_id: product.id,
              lot_number: "TEST-LOT",
              expires_on: "2030-10-31",
              manufactured_on: null,
            },
          ]),
        ),
      );
    if (url.includes("/stock/balance"))
      return new Response(
        JSON.stringify({
          on_hand_units: saved ? 12 : 0,
          dispensable_units: 0,
          stock_status: "quarantine",
        }),
      );
    if (url.includes("/stock/movements/") && init?.method === "PUT") {
      saved = true;
      return new Response(JSON.stringify({ id: url.split("/").at(-1) }));
    }
    if (url.includes("/stock/movements"))
      return new Response(
        JSON.stringify(
          page([
            {
              id: "TEST-receipt",
              source_reference: "TEST-OLD-RECEIPT",
              reason: "TEST received stock",
              created_at: "2026-10-17T00:00:00Z",
              actor_subject: "TEST-receiver",
              quantity_units: 1,
              kind: "receipt",
            },
          ]),
        ),
      );
    return base(url, init);
  });
  vi.stubGlobal("fetch", fetcher);
  const { user } = renderWorkspace("receiving");
  await chooseOption(
    user,
    await screen.findByLabelText("Quarantine location"),
    "TEST-Q · TEST Quarantine · Address not recorded",
  );
  await chooseOption(user, screen.getByLabelText("Product"), "TEST-500 · TEST Counter capsule");
  await chooseOption(
    user,
    screen.getByLabelText("Manufacturer lot"),
    "TEST-LOT · Expires 2030-10-31",
  );
  expect(await screen.findByText("Available to dispense")).toBeInTheDocument();
  expect(await screen.findByText("2026-10-17 05:30:00 GMT+05:30")).toHaveAttribute(
    "datetime",
    "2026-10-17T00:00:00Z",
  );
  await user.clear(screen.getByLabelText("Quantity in base units"));
  await user.type(screen.getByLabelText("Quantity in base units"), "12");
  await user.type(screen.getByLabelText("Receipt / correction line reference"), "TEST-GRN-12-L1");
  await user.type(screen.getByLabelText("Reason"), "TEST quarantine receipt");
  await user.click(screen.getByRole("button", { name: "Record movement" }));
  await waitFor(() => expect(saved).toBe(true));
  const receipt = fetcher.mock.calls.find(
    ([url, init]) => url.includes("/stock/movements/") && init?.method === "PUT",
  );
  expect(JSON.parse(receipt?.[1]?.body as string)).toMatchObject({
    location_id: location,
    batch_id: batch,
    kind: "receipt",
    quantity_units: 12,
    source_reference: "TEST-GRN-12-L1",
  });
  await waitFor(() => expect(screen.getByText("12")).toBeInTheDocument());
  expect(screen.queryByRole("option", { name: "Correction" })).not.toBeInTheDocument();
});

it("retains the selected outlet across pharmacy pages without crossing tenants", async () => {
  const base = mockPharmacy();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith(`/pharmacies/${pharmacy}/outlets`))
        return new Response(
          JSON.stringify(
            page([
              {
                id: sibling,
                name: "TEST Sister Outlet",
                country_code: "IN",
                access_level: "manager",
              },
              {
                id: outlet,
                name: "TEST Main Outlet",
                country_code: "IN",
                access_level: "manager",
              },
            ]),
          ),
        );
      return base(url, init);
    }),
  );
  const view = renderWorkspace("catalogue");
  await chooseOption(view.user, await screen.findByLabelText("Outlet"), "TEST Main Outlet");
  view.unmount();
  renderWorkspace("stock");
  await waitFor(() =>
    expect(screen.getByLabelText("Outlet")).toHaveTextContent("TEST Main Outlet"),
  );
});

it("registers an independent pharmacy without asking for a hospital", async () => {
  const base = mockPharmacy();
  let created: Record<string, unknown> | undefined;
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    if (url.endsWith("permissions"))
      return new Response(
        JSON.stringify({
          tenant_id: "11111111-1111-1111-1111-111111111111",
          subject: "TEST-admin",
          data: [
            "pharmacy.catalogue.read",
            "pharmacy.catalogue.write",
            "pharmacy.stock.read",
            "pharmacy.stock.receive",
            "pharmacy.access.manage",
          ],
        }),
      );
    if (
      url.includes("/pharmacy/pharmacies/") &&
      init?.method === "PUT" &&
      !url.includes("/outlets/")
    ) {
      created = JSON.parse(init.body as string);
      return new Response(
        JSON.stringify({ id: url.split("/").at(-1), ...created, onboarding_status: "draft" }),
      );
    }
    return base(url, init);
  });
  vi.stubGlobal("fetch", fetcher);
  const { user } = renderWorkspace("setup");
  await user.type(await screen.findByLabelText("Pharmacy code"), "TEST-NEW-INDEPENDENT");
  await user.type(screen.getByLabelText("Pharmacy name"), "TEST Independent Community Pharmacy");
  await user.click(screen.getByRole("button", { name: "Register pharmacy" }));
  await waitFor(() =>
    expect(created).toEqual({
      code: "TEST-NEW-INDEPENDENT",
      name: "TEST Independent Community Pharmacy",
      ownership_type: "independent",
    }),
  );
  expect(created).not.toHaveProperty("hospital_id");
  expect(screen.getByText(/Each declaration remains pending identity/)).toBeInTheDocument();
});

it("defaults a new product to the outlet country's currency", async () => {
  mockPharmacy("editor", 0, "IN");
  const { user } = renderWorkspace("catalogue");
  expect(await screen.findByText("TEST Counter capsule")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Add product" }));
  // #1287: only a supported country's currency is offered; the server
  // refuses any other.
  expect(screen.getByLabelText("Currency code")).toHaveTextContent("INR");
  await user.click(screen.getByLabelText("Currency code"));
  expect((await screen.findAllByRole("option")).map((o) => o.textContent)).toEqual(["INR"]);
});

it("captures optional physical location labels and retries the same immutable identity", async () => {
  const fallback = mockPharmacy();
  let attempts = 0;
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    if (url.includes("/stock/locations/") && init?.method === "PUT") {
      attempts++;
      return new Response(
        JSON.stringify(attempts === 1 ? { message: "TEST retryable failure" } : { id: "TEST" }),
        { status: attempts === 1 ? 503 : 200 },
      );
    }
    return fallback(url, init);
  });
  vi.stubGlobal("fetch", fetcher);
  const { user } = renderWorkspace("receiving");
  await user.type(await screen.findByLabelText("Location code"), "TEST-BIN");
  await user.type(screen.getByLabelText("Location name"), "TEST quarantine");
  await user.type(screen.getByLabelText("Zone code (optional)"), " test-z1 ");
  await user.type(screen.getByLabelText("Shelf code (optional)"), " test-s1 ");
  await user.click(screen.getByRole("button", { name: "Add location" }));
  expect(await screen.findByText("TEST retryable failure")).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Add location" }));
  await waitFor(() => expect(screen.getByLabelText("Location code")).toHaveValue(""));
  const writes = fetcher.mock.calls.filter(
    ([url, init]) => url.includes("/stock/locations/") && init?.method === "PUT",
  );
  expect(writes).toHaveLength(2);
  expect(writes[0][0]).toBe(writes[1][0]);
  expect(JSON.parse(writes[1][1]?.body as string)).toEqual({
    code: "TEST-BIN",
    name: "TEST quarantine",
    storage_address: { zone_code: "TEST-Z1", aisle_code: "", rack_code: "", shelf_code: "TEST-S1" },
    stock_status: "quarantine",
  });
  expect(screen.getByLabelText("Shelf code (optional)")).toHaveValue("");
});

it("shows malformed storage labels inline before sending a location", async () => {
  const fetcher = mockPharmacy();
  const { user } = renderWorkspace("receiving");
  await user.type(await screen.findByLabelText("Location code"), "TEST-BIN");
  await user.type(screen.getByLabelText("Location name"), "TEST quarantine");
  await user.type(screen.getByLabelText("Rack code (optional)"), "TEST/R1");
  await user.click(screen.getByRole("button", { name: "Add location" }));
  expect(await screen.findByText("Use the required format.")).toBeVisible();
  expect(fetcher.mock.calls.some(([, init]) => init?.method === "PUT")).toBe(false);
});

// Setup is the page for registering, so the empty state points to the form
// below rather than to "your administrator" (#1258 D3).
it("points an empty setup page at the registration form", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) =>
      url.endsWith("permissions")
        ? new Response(
            JSON.stringify({
              tenant_id: "11111111-1111-1111-1111-111111111111",
              subject: "TEST-admin",
              data: ["pharmacy.catalogue.read", "pharmacy.access.manage"],
            }),
          )
        : new Response(JSON.stringify({ data: [], page: { has_more: false, next_cursor: null } })),
    ),
  );
  renderWorkspace("setup");
  expect(await screen.findByText("No pharmacy registered yet")).toBeInTheDocument();
  expect(screen.queryByText(/Ask your administrator/)).not.toBeInTheDocument();
  expect(screen.getByLabelText("Pharmacy code")).toBeVisible();
});

// #576 D4: a count on the overview opens its list already filtered.
describe("lists open filtered from the overview", () => {
  const requested = (fetcher: ReturnType<typeof vi.fn>, part: string) =>
    fetcher.mock.calls.map(([url]) => String(url)).filter((url) => url.includes(part));

  it("opens Stock on hand at the expiry filter in the URL", async () => {
    nav.search = "balance=on_hand&expiry=due_30";
    const fetcher = mockPharmacy("editor", 0, "IN", ["pharmacy.stock.read"]);
    renderWorkspace("stock");
    await waitFor(() => expect(requested(fetcher, "/stock/positions").length).toBeGreaterThan(0));
    const url = new URL(requested(fetcher, "/stock/positions")[0]!, "http://x");
    expect(url.searchParams.get("expiry")).toBe("due_30");
    expect(url.searchParams.get("balance")).toBe("on_hand");
  });

  it("opens the catalogue at Not classified", async () => {
    nav.search = "tax=unclassified";
    const fetcher = mockPharmacy();
    renderWorkspace("catalogue");
    await waitFor(() =>
      expect(
        requested(fetcher, "/catalogue/products/counted?").some((u) =>
          u.includes("tax=unclassified"),
        ),
      ).toBe(true),
    );
    expect(await screen.findByRole("button", { name: "Not classified" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
  });

  it("opens Supplier bills on the Overdue tab the overview links to", async () => {
    nav.search = "tab=overdue";
    const fetcher = mockPharmacy("editor", 0, "IN", ["pharmacy.accounts.read"]);
    renderWithProviders(
      <NavigationGuardProvider>
        <PharmacyWorkspace view="accounts" />
      </NavigationGuardProvider>,
    );
    await waitFor(() =>
      expect(requested(fetcher, "/accounts/payables/counted?due=overdue").length).toBeGreaterThan(
        0,
      ),
    );
    expect(await screen.findByRole("link", { name: "Overdue" })).toHaveAttribute(
      "aria-current",
      "page",
    );
  });

  it("opens Expiry & safety at the outlet's recent concerns", async () => {
    nav.search = "tab=recent";
    const fetcher = mockPharmacy("editor", 0, "IN", ["pharmacy.stock.read"]);
    renderWorkspace("safety");
    await waitFor(() =>
      expect(requested(fetcher, "/stock/safety-incidents/recent").length).toBeGreaterThan(0),
    );
    expect(
      await screen.findByRole("region", { name: "Concerns recorded in the last 30 days" }),
    ).toBeVisible();
  });
});

// Review: Patients waiting is hospital-wide, so it shows even when there is
// no outlet to show (none registered, no grant) — exactly when the overview
// has nothing else to say.
it("shows the hospital-wide waiting count with no pharmacy registered", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.endsWith("permissions"))
        return new Response(
          JSON.stringify({
            tenant_id: "11111111-1111-1111-1111-111111111111",
            subject: "TEST-pharmacist",
            data: ["pharmacy.catalogue.read", "pharmacy.dispense.read"],
          }),
        );
      if (url.includes("/dispenses/waiting"))
        return new Response(
          JSON.stringify({
            data: [{ id: "d-1", created_at: new Date().toISOString() }],
            page: { has_more: false, next_cursor: null, total: 4, offset: 0 },
          }),
        );
      return new Response(JSON.stringify(page([])));
    }),
  );
  renderWorkspace("overview");
  const waiting = await screen.findByRole("region", { name: "Patients waiting" });
  expect(await within(waiting).findByText("4")).toBeVisible();
});

// Review: a page and cursor belong to one outlet's list. Changing outlet
// opens the new outlet's list at its first page, keeping the tab and filters.
it("opens the first page of the other outlet's list when the outlet changes", async () => {
  nav.search = "tab=active&q=amox&page=3&cursor=c3";
  const base = mockPharmacy();
  // The other outlet has rows, so an empty page cannot be what resets it.
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) =>
      url.includes(sibling) && url.includes("/catalogue/products")
        ? new Response(
            JSON.stringify(page([{ ...product, outlet_id: sibling, brand_name: "TEST Sister" }])),
          )
        : base(url, init),
    ),
  );
  const { user } = renderWorkspace("catalogue");
  await screen.findByText("TEST Counter capsule");
  expect(address().get("page")).toBe("3");
  await chooseOption(user, screen.getByLabelText("Outlet"), "TEST Sister Outlet");
  expect(await screen.findByText("TEST Sister")).toBeInTheDocument();
  await waitFor(() =>
    expect(nav.search).toBe(`tab=active&q=amox&pharmacy=${pharmacy}&outlet=${sibling}`),
  );
});

// #1303 E7: the pickers' Load more sat below every page's content, unnamed.
it("keeps the outlet picker's Load more under the picker, named", async () => {
  const base = mockPharmacy();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith(`/pharmacies/${pharmacy}/outlets`)) {
        const body = (await (await base(url, init)).json()) as { data: unknown[] };
        return new Response(
          JSON.stringify({ data: body.data, page: { has_more: true, next_cursor: "c2" } }),
        );
      }
      return base(url, init);
    }),
  );
  renderWorkspace("catalogue");
  const more = await screen.findByRole("button", { name: "Load more outlets" });
  const picker = screen.getByLabelText("Outlet");
  // Both sit in the heading's actions, above the page's own content.
  expect(more.closest("[data-picker]")).toBe(picker.closest("[data-picker]"));
  expect(more.closest("[data-picker]")).not.toBeNull();
  expect(screen.queryByRole("button", { name: /^Load more$/ })).toBeNull();
});

// #1303 E8: the address carries the pharmacy and outlet, so a link opens the
// outlet it was copied from.
describe("the outlet is in the address", () => {
  const save = (outletId: string) =>
    sessionStorage.setItem(
      "helivanta:pharmacy:scope:11111111-1111-1111-1111-111111111111",
      JSON.stringify({ pharmacyId: pharmacy, outletId }),
    );

  it("takes the address's outlet over the saved one, and saves it", async () => {
    save(outlet);
    nav.search = `pharmacy=${pharmacy}&outlet=${sibling}`;
    mockPharmacy();
    renderWorkspace("catalogue");
    await waitFor(() =>
      expect(screen.getByLabelText("Outlet")).toHaveTextContent("TEST Sister Outlet"),
    );
    await waitFor(() =>
      expect(
        JSON.parse(
          sessionStorage.getItem("helivanta:pharmacy:scope:11111111-1111-1111-1111-111111111111") ??
            "null",
        ),
      ).toEqual({ pharmacyId: pharmacy, outletId: sibling }),
    );
  });

  it("restores the resolved outlet address without starting a route transition", async () => {
    nav.search = "tab=all";
    mockPharmacy();
    renderWorkspace("catalogue");
    await screen.findByText("TEST Counter capsule");
    await waitFor(() => expect(address().get("outlet")).toBe(outlet));
    expect(address().get("pharmacy")).toBe(pharmacy);
    expect(address().get("tab")).toBe("all");
    expect(nav.calls).toEqual([]);
  });

  it("waits for an approved pending move before restoring the outlet address", async () => {
    nav.search = "tab=all";
    window.history.replaceState(null, "", "/pharmacy?tab=all");
    const fetcher = mockPharmacy();
    const original = fetcher.getMockImplementation()!;
    let releaseOutlets!: () => void;
    const outletsReady = new Promise<void>((resolve) => {
      releaseOutlets = resolve;
    });
    fetcher.mockImplementation(async (url, init) => {
      if (url.endsWith(`/pharmacies/${pharmacy}/outlets`)) await outletsReady;
      return original(url, init);
    });
    let finishMove!: () => void;
    const moving = new Promise<void>((resolve) => {
      finishMove = resolve;
    });
    function PendingMove() {
      const leave = useConfirmLeave();
      return <button onClick={() => leave(() => moving)}>Move to Lab</button>;
    }
    const { user } = renderWithProviders(
      <NavigationGuardProvider>
        <PharmacyWorkspace view="catalogue" />
        <PendingMove />
      </NavigationGuardProvider>,
    );
    await screen.findByLabelText("Pharmacy");
    await user.click(screen.getByRole("button", { name: "Move to Lab" }));
    await act(async () => releaseOutlets());
    await screen.findByText("TEST Counter capsule");
    expect(window.location.search).toBe("?tab=all");
    expect(
      sessionStorage.getItem("helivanta:pharmacy:scope:11111111-1111-1111-1111-111111111111"),
    ).toBeNull();
    await act(async () => finishMove());
    await waitFor(() => expect(address().get("outlet")).toBe(outlet));
  });

  it("does not let late scope restoration reclaim a different page", async () => {
    nav.search = "tab=all";
    const fetcher = mockPharmacy();
    const original = fetcher.getMockImplementation()!;
    let release!: () => void;
    const ready = new Promise<void>((resolve) => {
      release = resolve;
    });
    fetcher.mockImplementation(async (url, init) => {
      if (url.endsWith(`/pharmacies/${pharmacy}/outlets`)) await ready;
      return original(url, init);
    });
    renderWorkspace("catalogue");
    await screen.findByLabelText("Pharmacy");
    act(() => History.prototype.replaceState.call(window.history, null, "", "/emergency"));
    await act(async () => release());
    await screen.findByText("TEST Counter capsule");
    expect(window.location.pathname).toBe("/emergency");
    expect(nav.calls).toEqual([]);
    expect(
      sessionStorage.getItem("helivanta:pharmacy:scope:11111111-1111-1111-1111-111111111111"),
    ).toBeNull();
  });

  it("does not let late scope restoration overwrite a newer query", async () => {
    nav.search = "tab=all";
    const fetcher = mockPharmacy();
    const original = fetcher.getMockImplementation()!;
    let release!: () => void;
    const ready = new Promise<void>((resolve) => {
      release = resolve;
    });
    fetcher.mockImplementation(async (url, init) => {
      if (url.endsWith(`/pharmacies/${pharmacy}/outlets`)) await ready;
      return original(url, init);
    });
    renderWorkspace("catalogue");
    await screen.findByLabelText("Pharmacy");
    act(() =>
      History.prototype.replaceState.call(
        window.history,
        null,
        "",
        `/pharmacy?pharmacy=${pharmacy}&outlet=${sibling}&tab=overdue`,
      ),
    );
    await act(async () => release());
    await screen.findByText("TEST Counter capsule");
    const current = new URL(window.location.href);
    expect(current.searchParams.get("outlet")).toBe(sibling);
    expect(current.searchParams.get("tab")).toBe("overdue");
    expect(nav.calls).toEqual([]);
    expect(
      sessionStorage.getItem("helivanta:pharmacy:scope:11111111-1111-1111-1111-111111111111"),
    ).toBeNull();
  });

  // Fail closed: an open record from one outlet is never looked up under another.
  it("does not swap an outlet it cannot find for another one", async () => {
    nav.search = `pharmacy=${pharmacy}&outlet=0199e000-0000-7000-8000-0000000000ff&open=${product.id}`;
    const fetcher = mockPharmacy();
    renderWorkspace("catalogue");
    expect(await screen.findByText("Choose an outlet")).toBeInTheDocument();
    expect(fetcher.mock.calls.some(([url]) => String(url).includes("/catalogue/"))).toBe(false);
    expect(address().get("outlet")).toBe("0199e000-0000-7000-8000-0000000000ff");
  });

  it("closes the open record when another outlet is chosen", async () => {
    nav.search = `pharmacy=${pharmacy}&outlet=${outlet}&open=${product.id}`;
    mockPharmacy();
    const { user } = renderWorkspace("catalogue");
    await chooseOption(user, await screen.findByLabelText("Outlet"), "TEST Sister Outlet");
    await waitFor(() => expect(address().get("outlet")).toBe(sibling));
    expect(address().has("open")).toBe(false);
  });

  it("asks before another outlet replaces an unsaved record", async () => {
    nav.search = `pharmacy=${pharmacy}&outlet=${outlet}`;
    mockPharmacy();
    function Unsaved() {
      useLeaveGuard(true, "TEST draft");
      return null;
    }
    const { user } = renderWithProviders(
      <NavigationGuardProvider>
        <Unsaved />
        <PharmacyWorkspace view="catalogue" />
      </NavigationGuardProvider>,
    );
    await chooseOption(user, await screen.findByLabelText("Outlet"), "TEST Sister Outlet");
    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveTextContent("Discard changes to TEST draft?");
    await user.click(within(dialog).getByRole("button", { name: "Keep editing" }));
    expect(address().get("outlet")).toBe(outlet);
    expect(screen.getByLabelText("Outlet")).toHaveTextContent("TEST Main Outlet");
  });

  // Review Important 1: the saved choice was read once, at mount.
  it("a rail link to the same page keeps the outlet chosen since it opened", async () => {
    nav.search = `pharmacy=${pharmacy}&outlet=${outlet}`;
    mockPharmacy();
    const { user } = renderWorkspace("catalogue");
    await chooseOption(user, await screen.findByLabelText("Outlet"), "TEST Sister Outlet");
    await waitFor(() => expect(address().get("outlet")).toBe(sibling));
    followRail();
    await waitFor(() => expect(address().get("outlet")).toBe(sibling));
    expect(screen.getByLabelText("Outlet")).toHaveTextContent("TEST Sister Outlet");
  });

  // Review Important 2: the same outlet resolving again wrote nothing back.
  it("a rail link to the same outlet still puts the scope back in the address", async () => {
    nav.search = `pharmacy=${pharmacy}&outlet=${outlet}`;
    mockPharmacy();
    renderWorkspace("catalogue");
    await screen.findByText("TEST Counter capsule");
    followRail();
    await waitFor(() => expect(address().get("outlet")).toBe(outlet));
    expect(address().get("pharmacy")).toBe(pharmacy);
  });

  // Review (slice 2) Important 2: a location, product or lot id belongs to
  // one outlet; another outlet's list must not be asked for it.
  it("drops the old outlet's location, product and lot when another outlet is chosen", async () => {
    nav.search = `pharmacy=${pharmacy}&outlet=${outlet}&expiry=due_30&location=${product.id}&product=${product.id}&open=${product.id}&at=${product.id}`;
    mockPharmacy();
    const { user } = renderWorkspace("catalogue");
    await chooseOption(user, await screen.findByLabelText("Outlet"), "TEST Sister Outlet");
    await waitFor(() => expect(address().get("outlet")).toBe(sibling));
    for (const key of ["location", "product", "open", "at"]) expect(address().has(key)).toBe(false);
    expect(address().get("expiry")).toBe("due_30");
  });
});

// #1303 E6: Outlets & access lists the pharmacy's outlets; one opens beside
// the list with its details, its access and its tax registration.
describe("Outlets & access", () => {
  const adminFetch = (overrides: (url: string, init?: RequestInit) => Response | undefined) => {
    const base = mockPharmacy("editor", 0, "IN", ["pharmacy.access.manage", "pharmacy.tax.manage"]);
    const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
      const one = /\/pharmacies\/[0-9a-f-]{36}\/outlets\/([0-9a-f-]{36})$/.exec(url);
      if (one && (!init?.method || init.method === "GET")) {
        const custom = overrides(url, init);
        if (custom) return custom;
        const found = [outlet, sibling].includes(one[1]);
        return found
          ? new Response(
              JSON.stringify({
                id: one[1],
                pharmacy_id: pharmacy,
                code: one[1] === outlet ? "TEST-MAIN" : "TEST-SISTER",
                name: one[1] === outlet ? "TEST Main Outlet" : "TEST Sister Outlet",
                country_code: "IN",
                timezone: "Asia/Kolkata",
                regulatory_status: "pending_review",
              }),
            )
          : new Response(JSON.stringify({ error: "not_found", message: "x" }), { status: 404 });
      }
      return overrides(url, init) ?? base(url, init);
    });
    vi.stubGlobal("fetch", fetcher);
    return fetcher;
  };

  it("lists outlets; one opens with its details, access and tax registration", async () => {
    adminFetch(() => undefined);
    nav.search = `pharmacy=${pharmacy}&outlet=${outlet}&open=${sibling}`;
    renderWorkspace("setup");
    const pane = await screen.findByRole("region", { name: "Outlet TEST Sister Outlet" });
    expect(await within(pane).findByRole("heading", { name: "TEST Sister Outlet" })).toBeVisible();
    expect(within(pane).getByText("Asia/Kolkata")).toBeVisible();
    expect(within(pane).getByRole("heading", { name: "Outlet access" })).toBeVisible();
    expect(await within(pane).findByRole("heading", { name: "Tax registration" })).toBeVisible();
    // The list is the outlet chooser here: the heading keeps only the pharmacy.
    expect(screen.queryByLabelText("Outlet")).toBeNull();
    expect(screen.getByLabelText("Pharmacy")).toBeVisible();
  });

  it("opening an outlet does not change the working outlet", async () => {
    adminFetch(() => undefined);
    nav.search = `pharmacy=${pharmacy}&outlet=${outlet}`;
    const { user } = renderWorkspace("setup");
    await user.click(await screen.findByRole("button", { name: "Open TEST-SISTER" }));
    await screen.findByRole("region", { name: "Outlet TEST Sister Outlet" });
    expect(address().get("outlet")).toBe(outlet);
    expect(address().get("open")).toBe(sibling);
    expect(
      JSON.parse(
        sessionStorage.getItem("helivanta:pharmacy:scope:11111111-1111-1111-1111-111111111111")!,
      ).outletId,
    ).toBe(outlet);
  });

  it("registers an outlet in the pane and opens it", async () => {
    const created = "0199e000-0000-7000-8000-0000000000aa";
    let put: string | undefined;
    adminFetch((url, init) => {
      if (init?.method === "PUT" && url.includes("/outlets/")) {
        put = url.split("/").at(-1);
        return new Response(JSON.stringify({ id: put }));
      }
      if (put && url.endsWith(`/outlets/${put}`))
        return new Response(
          JSON.stringify({
            id: put,
            pharmacy_id: pharmacy,
            code: "TEST-NEW",
            name: "TEST New Outlet",
            country_code: "IN",
            timezone: "Asia/Kolkata",
            regulatory_status: "pending_review",
          }),
        );
      return undefined;
    });
    void created;
    nav.search = `pharmacy=${pharmacy}&outlet=${outlet}`;
    const { user } = renderWorkspace("setup");
    await user.click(await screen.findByRole("button", { name: "Register outlet" }));
    const pane = await screen.findByRole("region", { name: "New outlet" });
    await user.type(within(pane).getByLabelText("Outlet code"), "TEST-NEW");
    await user.type(within(pane).getByLabelText("Outlet name"), "TEST New Outlet");
    await user.click(within(pane).getByRole("button", { name: "Register outlet" }));
    await waitFor(() => expect(address().get("open")).toBe(put));
    expect(address().has("new")).toBe(false);
    expect(await screen.findByRole("heading", { name: "TEST New Outlet" })).toBeVisible();
  });

  it("asks before Next discards an unsaved access grant", async () => {
    adminFetch(() => undefined);
    nav.search = `pharmacy=${pharmacy}&outlet=${outlet}`;
    const { user } = renderWorkspace("setup");
    await user.click(await screen.findByRole("button", { name: "Open TEST-MAIN" }));
    await user.click(
      await screen.findByLabelText("Grant my account pharmacy access to this outlet"),
    );
    await user.click(screen.getByRole("button", { name: "Next outlet" }));
    expect(await screen.findByRole("dialog")).toHaveTextContent(
      "Discard changes to TEST Main Outlet?",
    );
  });

  it("says an outlet that is not found, or not ours, is not found", async () => {
    adminFetch(() => undefined);
    nav.search = `pharmacy=${pharmacy}&outlet=${outlet}&open=0199e000-0000-7000-8000-0000000000ff`;
    const { user } = renderWorkspace("setup");
    expect(
      await screen.findByText("This record was not found, or you do not have access."),
    ).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Close outlet" }));
    expect(address().has("open")).toBe(false);
  });
});
