import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { DrugCataloguePicker } from "./drug-catalogue-picker";

const scope = {
  tenantId: "11111111-1111-1111-1111-111111111111",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};
const id = "0199e000-0000-7000-8000-000000000030";
const medicine = {
  product_id: id,
  version_id: id,
  formulation_id: id,
  formulation_version_id: id,
  release_id: id,
  source_name: "TEST licensed source",
  source_version: "TEST-v1",
  licence_ref: "TEST-LICENCE",
  brand_name: "TEST medicine",
  manufacturer: "TEST manufacturer",
  formulation_label: "TEST molecule 500 mg capsule",
  dose_form: "capsule",
  route: "oral",
  regulatory_class: "H",
  valid_from: "2026-01-01",
  pack_size: 10,
  unit_of_issue: "capsule",
  gtin: null,
  hsn_code: "30041010",
  ndps: false,
  ingredients: [
    {
      molecule_id: id,
      inn_name: "TEST molecule",
      strength_value: "500",
      strength_unit: "mg",
      per_value: null,
      per_unit: null,
    },
  ],
};
afterEach(() => vi.unstubAllGlobals());

it("imports a deliberately selected pack using its exact product/formulation versions and local MRP", async () => {
  const saved = vi.fn();
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    if (url.includes("/catalogue/drugmaster?q="))
      return new Response(JSON.stringify({ data: [medicine] }));
    expect(url).toContain("/from-drugmaster");
    expect(init?.method).toBe("PUT");
    expect(JSON.parse(init?.body as string)).toEqual({
      version_id: id,
      formulation_version_id: id,
      currency: "INR",
      mrp_minor: 0,
    });
    return new Response(JSON.stringify({ id, drug_master: medicine }));
  });
  vi.stubGlobal("fetch", fetcher);
  renderWithProviders(<DrugCataloguePicker scope={scope} onSaved={saved} />);
  const user = userEvent.setup();
  expect(screen.queryByRole("button", { name: "Add selected medicine" })).not.toBeInTheDocument();
  await user.type(screen.getByLabelText("Medicine name, composition or barcode"), "TEST");
  await user.click(screen.getByRole("button", { name: "Search medicines" }));
  await user.click(await screen.findByRole("button", { name: /TEST medicine ·/ }));
  expect(screen.getByText(/HSN: 30041010/)).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Add selected medicine" }));
  await waitFor(() => expect(saved).toHaveBeenCalledWith({ id, drug_master: medicine }));
});

it("reports an empty published catalogue without fabricating medicine choices", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response(JSON.stringify({ data: [] }))),
  );
  renderWithProviders(<DrugCataloguePicker scope={scope} onSaved={vi.fn()} />);
  const user = userEvent.setup();
  await user.type(screen.getByLabelText("Medicine name, composition or barcode"), "missing");
  await user.click(screen.getByRole("button", { name: "Search medicines" }));
  expect(await screen.findByRole("status")).toHaveTextContent("No published medicines match");
  expect(screen.queryByRole("button", { name: "Add selected medicine" })).not.toBeInTheDocument();
});
