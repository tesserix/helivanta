import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import {
  taxScheme,
  type PharmacyProduct,
  type PharmacyProductTax,
  type PharmacyTaxClassification,
} from "@helivanta/api";
import { chooseOption } from "../../test/select";
import { CataloguePanel } from "./catalogue-panel";
import { ProductTaxSection, taxLabel } from "./tax-classification";

const scope = {
  tenantId: "0199e000-0000-7000-8000-0000000000aa",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};
const india = taxScheme("IN");
const product: PharmacyProduct = {
  id: "0199e000-0000-7000-8000-0000000000p1",
  outlet_id: scope.outletId,
  sku: "TEST-AMOX",
  brand_name: "TEST Amoxil",
  generic_name: "TEST amoxicillin",
  dosage_form: "capsule",
  route: "oral",
  manufacturer: "TEST Manufacturer",
  marketing_company: "",
  ingredients: [{ name: "TEST amoxicillin", strength: "500", unit: "mg" }],
  pack_size: 10,
  unit_of_issue: "capsule",
  currency: "INR",
  mrp_minor: 12500,
  status: "active",
  regulatory_status: "pending_review",
  version: 1,
  created_at: "2026-10-01T00:00:00Z",
  updated_at: "2026-10-01T00:00:00Z",
  tax: null,
};
const classification = (over: Partial<PharmacyTaxClassification>): PharmacyTaxClassification => ({
  id: "0199e000-0000-7000-8000-0000000000c1",
  outlet_id: scope.outletId,
  product_id: product.id,
  classification_code: "3004",
  supply_nature: "taxable",
  rate_ppm: 50000,
  exemption_basis: null,
  effective_from: "2026-11-01",
  reason: null,
  recorded_by: "user-owner",
  created_at: "2026-11-01T06:30:00Z",
  in_force: true,
  replaced: false,
  ...over,
});

const page = (data: unknown[]) =>
  new Response(JSON.stringify({ data, page: { next_cursor: null, has_more: false } }), {
    headers: { "Content-Type": "application/json" },
  });

function serve(
  history: () => PharmacyTaxClassification[],
  products: PharmacyProduct[] = [product],
) {
  const posted: unknown[] = [];
  const urls: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      urls.push(url);
      if (init?.method === "POST") {
        posted.push(JSON.parse(String(init.body)));
        return new Response(JSON.stringify(classification({})), { status: 201 });
      }
      if (url.includes("/tax-classifications")) return page(history());
      const detail = products.find((p) => url.endsWith(`/catalogue/products/${p.id}`));
      if (detail)
        return new Response(JSON.stringify(detail), {
          headers: { "Content-Type": "application/json" },
        });
      if (url.includes("/catalogue/products")) return page(products);
      if (url.includes("/revisions")) return page([]);
      throw new Error(`Unexpected test API request: ${url}`);
    }),
  );
  return { posted, urls };
}

afterEach(() => vi.unstubAllGlobals());

describe("taxLabel", () => {
  it("names each supply nature", () => {
    const tax = (over: Partial<PharmacyProductTax>): PharmacyProductTax => ({
      classification_code: "3004",
      supply_nature: "taxable",
      rate_ppm: 50000,
      effective_from: "2026-11-01",
      ...over,
    });
    expect(taxLabel(tax({}))).toBe("HSN 3004 · 5%");
    expect(taxLabel(tax({ rate_ppm: 2500 }))).toBe("HSN 3004 · 0.25%");
    expect(taxLabel(tax({ supply_nature: "nil_rated", rate_ppm: 0 }))).toBe("Nil-rated");
    expect(taxLabel(tax({ supply_nature: "exempt", rate_ppm: 0 }))).toBe("Exempt");
    expect(taxLabel(null)).toBe("Not classified");
  });
});

describe("ProductTaxSection", () => {
  const section = (canManage: boolean) => (
    <ProductTaxSection
      scope={scope}
      product={product}
      scheme={india}
      canManage={canManage}
      today="2026-11-01"
    />
  );

  it("records a 0.25% rate as typed", async () => {
    const { posted } = serve(() => []);
    const { user } = renderWithProviders(section(true));
    await user.type(await screen.findByLabelText("HSN code"), "30049099");
    await user.type(screen.getByLabelText("Rate (%)"), "0.25");
    await user.click(screen.getByRole("button", { name: "Record classification" }));
    await waitFor(() =>
      expect(posted).toEqual([
        {
          classification_code: "30049099",
          supply_nature: "taxable",
          rate_percent: "0.25",
          effective_from: "2026-11-01",
        },
      ]),
    );
  });

  it("requires the basis of an exemption", async () => {
    const { posted } = serve(() => []);
    const { user } = renderWithProviders(section(true));
    await user.type(await screen.findByLabelText("HSN code"), "3006");
    await chooseOption(user, screen.getByRole("combobox", { name: "Supply" }), "Exempt");
    await user.click(screen.getByRole("button", { name: "Record classification" }));
    expect(await screen.findByText(/basis for the exemption/)).toBeVisible();
    expect(posted).toHaveLength(0);
    await user.type(screen.getByLabelText("Exemption basis"), "TEST notification entry");
    await user.click(screen.getByRole("button", { name: "Record classification" }));
    await waitFor(() => expect(posted).toHaveLength(1));
    expect(posted[0]).toMatchObject({
      supply_nature: "exempt",
      rate_percent: "0",
      exemption_basis: "TEST notification entry",
    });
  });

  it("refuses an HSN code of the wrong length", async () => {
    const { posted } = serve(() => []);
    const { user } = renderWithProviders(section(true));
    await user.type(await screen.findByLabelText("HSN code"), "30049");
    await user.type(screen.getByLabelText("Rate (%)"), "5");
    await user.click(screen.getByRole("button", { name: "Record classification" }));
    expect(await screen.findByText("Enter a 4, 6 or 8 digit HSN code.")).toBeVisible();
    expect(posted).toHaveLength(0);
  });

  it("shows history with a replaced row, and no form for someone who cannot set tax", async () => {
    serve(() => [
      classification({ id: "a", rate_ppm: 120000, reason: "Wrong slab" }),
      classification({ id: "b", in_force: false, replaced: true }),
    ]);
    renderWithProviders(section(false));
    expect(await screen.findByText("Replaced")).toBeVisible();
    expect(screen.getAllByText("HSN 3004 · 12%").length).toBeGreaterThan(0);
    expect(screen.queryByRole("button", { name: "Record classification" })).toBeNull();
  });
});

describe("ProductTaxSection in force", () => {
  it("reads the classification in force from the product, not the history page", async () => {
    const classified = {
      ...product,
      tax: {
        classification_code: "3004",
        supply_nature: "taxable" as const,
        rate_ppm: 50000,
        effective_from: "2026-11-01",
      },
    };
    serve(
      () => [
        classification({
          id: "u",
          rate_ppm: 120000,
          effective_from: "2027-01-01",
          in_force: false,
        }),
      ],
      [classified],
    );
    renderWithProviders(
      <ProductTaxSection
        scope={scope}
        product={{ ...product, tax: null }}
        scheme={india}
        canManage={false}
        today="2026-11-01"
      />,
    );
    expect(await screen.findByText("HSN 3004 · 5%")).toBeVisible();
    expect(screen.getByText("Upcoming")).toBeVisible();
  });
});

describe("CataloguePanel tax", () => {
  it("badges unclassified products and filters to them", async () => {
    const { urls } = serve(
      () => [],
      [
        product,
        {
          ...product,
          id: "0199e000-0000-7000-8000-0000000000p2",
          sku: "TEST-DONE",
          tax: classification({}),
        },
      ],
    );
    const { user } = renderWithProviders(
      <CataloguePanel
        scope={scope}
        writable
        defaultCurrency="INR"
        timeZone="Asia/Kolkata"
        countryCode="IN"
      />,
    );
    const rows = await screen.findAllByRole("listitem");
    expect(within(rows[0]!).getByText("Not classified")).toBeVisible();
    expect(within(rows[1]!).getByText("HSN 3004 · 5%")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Not classified" }));
    await waitFor(() => expect(urls.some((url) => url.includes("tax=unclassified"))).toBe(true));
    expect(screen.getByRole("button", { name: "Not classified" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
  });

  it("offers no tax filter for a country without a scheme", async () => {
    serve(() => []);
    renderWithProviders(
      <CataloguePanel scope={scope} writable defaultCurrency="PKR" countryCode="PK" />,
    );
    await screen.findAllByRole("listitem");
    expect(screen.queryByRole("button", { name: "Not classified" })).toBeNull();
  });

  // Review: "not classified" means nothing where the country has no tax
  // scheme; a link carrying it must not leave a hidden filter on the list.
  it("ignores a starting Not classified filter without a scheme", async () => {
    const { urls } = serve(() => []);
    renderWithProviders(
      <CataloguePanel
        scope={scope}
        writable
        defaultCurrency="PKR"
        countryCode="PK"
        initialUnclassified
      />,
    );
    await screen.findAllByRole("listitem");
    expect(urls.some((url) => url.includes("tax=unclassified"))).toBe(false);
  });
});
