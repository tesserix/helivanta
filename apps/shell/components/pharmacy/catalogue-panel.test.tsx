import { act, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { NavigationGuardProvider } from "@helivanta/ui";
import { CataloguePanel } from "./catalogue-panel";

// A live address: push and replace change what useSearchParams returns.
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
    usePathname: () => "/pharmacy/catalogue",
    useSearchParams: () => new URLSearchParams(useSyncExternalStore(subscribe, () => nav.search)),
    useRouter: () => ({ push: go("push"), replace: go("replace") }),
  };
});
afterEach(() => {
  vi.unstubAllGlobals();
  nav.search = "";
  nav.calls.length = 0;
});

const scope = {
  tenantId: "11111111-1111-1111-1111-111111111111",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};
const productId = (n: number) => `0199e000-0000-7000-8000-00000000003${n}`;
const product = (n: number) => ({
  id: productId(n),
  outlet_id: scope.outletId,
  sku: `TEST-SKU-${n}`,
  brand_name: `TEST Brand ${n}`,
  generic_name: "TEST molecule",
  dosage_form: "tablet",
  route: "oral",
  manufacturer: "TEST Manufacturer",
  marketing_company: "",
  ingredients: [{ name: "TEST molecule", strength: "500", unit: "mg" }],
  pack_size: 10,
  unit_of_issue: "tablet",
  currency: "INR",
  mrp_minor: 12500,
  status: "active",
  version: 1,
  regulatory_status: "pending_review",
  tax: null,
  created_at: "2026-10-15T00:00:00Z",
  updated_at: "2026-10-15T00:00:00Z",
});
const counted = (data: unknown[]) => ({
  data,
  page: { has_more: false, next_cursor: null, total: data.length, offset: 0 },
});
const plain = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });

function mockCatalogue({ missing = "" } = {}) {
  const fetcher = vi.fn(async (url: string) => {
    if (url.includes("/catalogue/products/counted"))
      return new Response(JSON.stringify(counted([1, 2, 3].map(product))));
    if (url.endsWith("/revisions") || url.endsWith("/tax-classifications"))
      return new Response(JSON.stringify(plain([])));
    const one = /\/catalogue\/products\/([0-9a-f-]{36})$/.exec(url);
    if (one) {
      if (one[1] === missing)
        return new Response(JSON.stringify({ error: "not_found", message: "x" }), { status: 404 });
      return new Response(JSON.stringify(product(Number(one[1].slice(-1)))));
    }
    return new Response(JSON.stringify(plain([])));
  });
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}

const renderCatalogue = () =>
  renderWithProviders(
    <NavigationGuardProvider>
      <CataloguePanel
        scope={scope}
        writable
        defaultCurrency="INR"
        countryCode="IN"
        canManageTax
        timeZone="Asia/Kolkata"
      />
    </NavigationGuardProvider>,
  );
const frame = () => act(() => new Promise((resolve) => requestAnimationFrame(resolve)));

describe("a product opens beside the catalogue (#1303 E2)", () => {
  it("reopens the product named in the address, with its heading focused", async () => {
    nav.search = `tab=all&open=${productId(2)}`;
    mockCatalogue();
    renderCatalogue();
    const pane = await screen.findByRole("region", { name: "Product TEST Brand 2" });
    const heading = await within(pane).findByRole("heading", { name: "TEST Brand 2" });
    await waitFor(() => expect(heading).toHaveFocus());
  });

  it("says a product that is not found, or not ours, is not found", async () => {
    const missing = "0199e000-0000-7000-8000-0000000000ff";
    nav.search = `open=${missing}`;
    mockCatalogue({ missing });
    renderCatalogue();
    expect(
      await screen.findByText("This record was not found, or you do not have access."),
    ).toBeInTheDocument();
  });

  it("steps through the page's products in list order", async () => {
    mockCatalogue();
    const { user } = renderCatalogue();
    await user.click(await screen.findByRole("button", { name: "Open TEST-SKU-1" }));
    expect(await screen.findByRole("region", { name: "Product TEST Brand 1" })).toBeVisible();
    expect(screen.getByRole("button", { name: "Previous product" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "Next product" }));
    expect(await screen.findByRole("region", { name: "Product TEST Brand 2" })).toBeVisible();
    expect(nav.calls.map((call) => call.method)).toEqual(["push", "replace"]);
  });

  it("asks before Next discards an unsaved product edit", async () => {
    mockCatalogue();
    const { user } = renderCatalogue();
    await user.click(await screen.findByRole("button", { name: "Open TEST-SKU-1" }));
    const brand = await screen.findByLabelText("Brand or preparation name");
    await user.clear(brand);
    await user.type(brand, "TEST renamed");
    await user.click(screen.getByRole("button", { name: "Next product" }));
    expect(screen.getByRole("dialog")).toHaveTextContent("Discard changes to TEST Brand 1?");
    await user.click(screen.getByRole("button", { name: "Keep editing" }));
    expect(screen.getByLabelText("Brand or preparation name")).toHaveValue("TEST renamed");
  });

  it("asks before Next discards an unsaved tax classification", async () => {
    mockCatalogue();
    const { user } = renderCatalogue();
    await user.click(await screen.findByRole("button", { name: "Open TEST-SKU-1" }));
    const reason = await screen.findByLabelText(/Reason/);
    await user.type(reason, "TEST classification note");
    await user.click(screen.getByRole("button", { name: "Next product" }));
    expect(screen.getByRole("dialog")).toHaveTextContent("Discard changes to TEST Brand 1?");
  });

  it("titles Add product New product and focuses its first field", async () => {
    mockCatalogue();
    const { user } = renderCatalogue();
    await user.click(await screen.findByRole("button", { name: "Add product" }));
    const pane = await screen.findByRole("region", { name: "New product" });
    await frame();
    expect(within(pane).getAllByRole("textbox")[0]).toHaveFocus();
    expect(new URLSearchParams(nav.search).get("new")).toBe("1");
  });

  it("closes the product when the tab changes", async () => {
    nav.search = `tab=all&open=${productId(1)}`;
    mockCatalogue();
    renderCatalogue();
    await screen.findByRole("region", { name: "Product TEST Brand 1" });
    // A tab is a link to another list: it carries no open record.
    expect(screen.getByRole("link", { name: "Active" })).toHaveAttribute(
      "href",
      "/pharmacy/catalogue?tab=active",
    );
  });

  it("keeps rows to brand, SKU and tax beside an open product", async () => {
    nav.search = `open=${productId(1)}`;
    mockCatalogue();
    renderCatalogue();
    await screen.findByRole("region", { name: "Product TEST Brand 1" });
    const row = screen.getByRole("button", { name: "Open TEST-SKU-2" }).closest("li")!;
    expect(row).toHaveTextContent("TEST Brand 2");
    expect(row).toHaveTextContent("TEST-SKU-2");
    expect(row).not.toHaveTextContent("TEST Manufacturer");
    expect(row).not.toHaveTextContent("Pending regulatory review");
  });
});
