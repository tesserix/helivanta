import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
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
const nav = vi.hoisted(() => ({ search: "" }));
vi.mock("next/navigation", async (original) => ({
  ...(await original<typeof import("next/navigation")>()),
  useSearchParams: () => new URLSearchParams(nav.search),
  usePathname: () => "/pharmacy",
  useRouter: () => ({ replace: vi.fn(), push: vi.fn() }),
}));

afterEach(() => {
  vi.unstubAllGlobals();
  sessionStorage.clear();
  nav.search = "";
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
              name: "TEST Main Outlet",
              timezone: "Asia/Kolkata",
              country_code: country,
              access_level: "manager",
            },
            {
              id: sibling,
              name: "TEST Sister Outlet",
              timezone: "Asia/Karachi",
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
    if (url.includes("/catalogue/products"))
      return new Response(JSON.stringify(page(url.includes(sibling) ? [] : [product])));
    return new Response(JSON.stringify(page([])));
  });
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}

it("shows private catalogue identity and clears rows when switching outlet", async () => {
  const fetcher = mockPharmacy();
  const { user } = renderWithProviders(<PharmacyWorkspace view="catalogue" />);
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
                country_code: "PK",
                timezone: "Asia/Karachi",
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
      if (url.includes(sibling) && url.includes("/catalogue/products"))
        return new Response(JSON.stringify(page([{ ...product, outlet_id: sibling }])));
      return base(url, init);
    }),
  );
  const { user } = renderWithProviders(<PharmacyWorkspace view="catalogue" />);
  await user.click(await screen.findByRole("button", { name: "Open TEST-500" }));
  expect(screen.getByText("Times are in Asia/Kolkata.")).toBeInTheDocument();
  const firstTime = await screen.findByText("2026-10-17 05:30:00 GMT+05:30");
  expect(firstTime).toHaveAttribute("datetime", "2026-10-17T00:00:00Z");
  await chooseOption(user, screen.getByLabelText("Outlet"), "TEST Sister Outlet");
  expect(await screen.findByText("Times are in Asia/Karachi.")).toBeInTheDocument();
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
  const { user } = renderWithProviders(<PharmacyWorkspace view="stock" />);
  const register = await screen.findByRole("region", { name: "Stock register" });
  expect(within(register).getByText(/\(Asia\/Kolkata\)/)).toBeInTheDocument();
  await chooseOption(user, screen.getByLabelText("Outlet"), "TEST Sister Outlet");
  const nextRegister = await screen.findByRole("region", { name: "Stock register" });
  expect(within(nextRegister).getByText(/\(Asia\/Karachi\)/)).toBeInTheDocument();
  expect(within(nextRegister).queryByText(/\(Asia\/Kolkata\)/)).not.toBeInTheDocument();
});

it("denies missing outlet grants and hides writes from outlet readers", async () => {
  mockPharmacy("reader");
  renderWithProviders(<PharmacyWorkspace view="catalogue" />);
  expect(await screen.findByText("TEST Counter capsule")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Add product" })).not.toBeInTheDocument();
});

it("does not load products after outlet access is denied", async () => {
  const fetcher = mockPharmacy("editor", 404);
  renderWithProviders(<PharmacyWorkspace view="catalogue" />);
  expect(await screen.findByText("You don't have access to this outlet yet")).toBeVisible();
  expect(fetcher.mock.calls.some(([url]) => url.includes("/catalogue/products"))).toBe(false);
});

// #1285: no grant is not a failure, so it offers no retry.
it("says a missing grant is missing and points a grant manager to Setup", async () => {
  mockPharmacy("editor", 404, "IN", ["pharmacy.access.manage"]);
  renderWithProviders(<PharmacyWorkspace view="catalogue" />);
  expect(await screen.findByText("You don't have access to this outlet yet")).toBeVisible();
  expect(screen.getByRole("link", { name: "Grant access in Setup" })).toHaveAttribute(
    "href",
    "/pharmacy/setup",
  );
  expect(screen.queryByRole("button", { name: "Try again" })).toBeNull();
});

it("tells someone who cannot grant access to ask an administrator", async () => {
  mockPharmacy("editor", 404);
  renderWithProviders(<PharmacyWorkspace view="catalogue" />);
  expect(await screen.findByText(/Ask your administrator to grant you access/)).toBeVisible();
  expect(screen.queryByRole("link", { name: "Grant access in Setup" })).toBeNull();
  expect(screen.queryByRole("button", { name: "Try again" })).toBeNull();
});

it("keeps retry when the access check itself fails", async () => {
  mockPharmacy("editor", 503);
  renderWithProviders(<PharmacyWorkspace view="catalogue" />);
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
  const { user } = renderWithProviders(<PharmacyWorkspace view="catalogue" />);
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
  const { user } = renderWithProviders(<PharmacyWorkspace view="catalogue" />);
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
  const { user } = renderWithProviders(<PharmacyWorkspace view="receiving" />);
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
  const view = renderWithProviders(<PharmacyWorkspace view="catalogue" />);
  await chooseOption(view.user, await screen.findByLabelText("Outlet"), "TEST Main Outlet");
  view.unmount();
  renderWithProviders(<PharmacyWorkspace view="stock" />);
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
  const { user } = renderWithProviders(<PharmacyWorkspace view="setup" />);
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

it("defaults a new Pakistan product to PKR without changing an existing INR product", async () => {
  mockPharmacy("editor", 0, "PK");
  const { user } = renderWithProviders(<PharmacyWorkspace view="catalogue" />);
  expect(await screen.findByText("TEST Counter capsule")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Add product" }));
  expect(screen.getByLabelText("Currency code")).toHaveValue("PKR");
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
  const { user } = renderWithProviders(<PharmacyWorkspace view="receiving" />);
  await user.type(await screen.findByLabelText("Location code"), "TEST-BIN");
  await user.type(screen.getByLabelText("Location name"), "TEST quarantine");
  await user.type(screen.getByLabelText("Zone code (optional)"), " test-z1 ");
  await user.type(screen.getByLabelText("Shelf code (optional)"), " test-s1 ");
  await user.click(screen.getByRole("button", { name: "Add quarantine location" }));
  expect(await screen.findByText("TEST retryable failure")).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Add quarantine location" }));
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
  });
  expect(screen.getByLabelText("Shelf code (optional)")).toHaveValue("");
});

it("shows malformed storage labels inline before sending a location", async () => {
  const fetcher = mockPharmacy();
  const { user } = renderWithProviders(<PharmacyWorkspace view="receiving" />);
  await user.type(await screen.findByLabelText("Location code"), "TEST-BIN");
  await user.type(screen.getByLabelText("Location name"), "TEST quarantine");
  await user.type(screen.getByLabelText("Rack code (optional)"), "TEST/R1");
  await user.click(screen.getByRole("button", { name: "Add quarantine location" }));
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
  renderWithProviders(<PharmacyWorkspace view="setup" />);
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
    renderWithProviders(<PharmacyWorkspace view="stock" />);
    await waitFor(() => expect(requested(fetcher, "/stock/positions").length).toBeGreaterThan(0));
    const url = new URL(requested(fetcher, "/stock/positions")[0]!, "http://x");
    expect(url.searchParams.get("expiry")).toBe("due_30");
    expect(url.searchParams.get("balance")).toBe("on_hand");
  });

  it("opens the catalogue at Not classified", async () => {
    nav.search = "tax=unclassified";
    const fetcher = mockPharmacy();
    renderWithProviders(<PharmacyWorkspace view="catalogue" />);
    await waitFor(() =>
      expect(
        requested(fetcher, "/catalogue/products?").some((u) => u.includes("tax=unclassified")),
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
    renderWithProviders(<PharmacyWorkspace view="accounts" />);
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
    nav.search = "recorded=30d";
    const fetcher = mockPharmacy("editor", 0, "IN", ["pharmacy.stock.read"]);
    renderWithProviders(<PharmacyWorkspace view="safety" />);
    await waitFor(() =>
      expect(requested(fetcher, "/stock/safety-incidents?recorded=30d").length).toBeGreaterThan(0),
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
  renderWithProviders(<PharmacyWorkspace view="overview" />);
  const waiting = await screen.findByRole("region", { name: "Patients waiting" });
  expect(await within(waiting).findByText("4")).toBeVisible();
});
