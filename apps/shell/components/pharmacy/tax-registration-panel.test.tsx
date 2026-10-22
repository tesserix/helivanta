import { screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import type { PharmacyOutlet, PharmacyTaxRegistration } from "@helivanta/api";
import { chooseOption } from "../../test/select";
import { TaxRegistrationPanel } from "./tax-registration-panel";

const scope = {
  tenantId: "0199e000-0000-7000-8000-0000000000aa",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};
const outlet: PharmacyOutlet = {
  id: scope.outletId,
  pharmacy_id: scope.pharmacyId,
  code: "TEST-MAIN",
  name: "TEST Main Outlet",
  country_code: "IN",
  timezone: "Asia/Kolkata",
  regulatory_status: "pending_review",
  created_at: "2026-10-01T00:00:00Z",
};
const row = (over: Partial<PharmacyTaxRegistration>): PharmacyTaxRegistration => ({
  id: "0199e000-0000-7000-8000-0000000000r1",
  outlet_id: scope.outletId,
  jurisdiction_code: "07",
  identifier_kind: "gstin",
  identifier: "07ABCDE1234F1Z2",
  effective_from: "2026-11-01",
  reason: null,
  recorded_by: "user-owner",
  created_at: "2026-11-01T06:30:00Z",
  in_force: true,
  replaced: false,
  ...over,
});

function serve(rows: () => PharmacyTaxRegistration[], status = 200) {
  const posted: unknown[] = [];
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    if (init?.method === "POST") {
      posted.push(JSON.parse(String(init.body)));
      return new Response(JSON.stringify(row({})), { status: 201 });
    }
    if (url.includes("/tax-registrations"))
      return new Response(
        status === 200
          ? JSON.stringify({ data: rows(), page: { next_cursor: null, has_more: false } })
          : JSON.stringify({ error: "unavailable" }),
        { status, headers: { "Content-Type": "application/json" } },
      );
    throw new Error(`Unexpected test API request: ${url}`);
  });
  vi.stubGlobal("fetch", fetcher);
  return posted;
}

afterEach(() => vi.unstubAllGlobals());

describe("TaxRegistrationPanel", () => {
  it("shows the registration in force and marks a replaced one", async () => {
    serve(() => [
      row({ id: "a", identifier: "27PQRST5678K2Z1", jurisdiction_code: "27", reason: "Moved" }),
      row({ id: "b", in_force: false, replaced: true }),
    ]);
    renderWithProviders(<TaxRegistrationPanel scope={scope} outlet={outlet} today="2026-11-01" />);
    expect(await screen.findByText("In force today")).toBeVisible();
    expect(screen.getAllByText("27PQRST5678K2Z1")).toHaveLength(2);
    expect(screen.getByText("Replaced")).toBeVisible();
    expect(screen.getByText(/Format checked, not verified with GSTN/)).toBeVisible();
  });

  it("refuses a GSTIN with a wrong check character without posting", async () => {
    const posted = serve(() => []);
    const { user } = renderWithProviders(
      <TaxRegistrationPanel scope={scope} outlet={outlet} today="2026-11-01" />,
    );
    await chooseOption(
      user,
      await screen.findByRole("combobox", { name: "State or union territory" }),
      /Delhi/,
    );
    await user.type(screen.getByLabelText("GSTIN"), "07ABCDE1234F1Z3");
    await user.click(screen.getByRole("button", { name: "Record registration" }));
    expect(await screen.findByText(/last character does not match/)).toBeVisible();
    expect(posted).toHaveLength(0);
  });

  it("refuses another state's GSTIN", async () => {
    const posted = serve(() => []);
    const { user } = renderWithProviders(
      <TaxRegistrationPanel scope={scope} outlet={outlet} today="2026-11-01" />,
    );
    await chooseOption(
      user,
      await screen.findByRole("combobox", { name: "State or union territory" }),
      /Maharashtra/,
    );
    await user.type(screen.getByLabelText("GSTIN"), "07ABCDE1234F1Z2");
    await user.click(screen.getByRole("button", { name: "Record registration" }));
    expect(await screen.findByText(/registered in a different state/)).toBeVisible();
    expect(posted).toHaveLength(0);
  });

  it("records a registration from today without a reason", async () => {
    const posted = serve(() => []);
    const { user } = renderWithProviders(
      <TaxRegistrationPanel scope={scope} outlet={outlet} today="2026-11-01" />,
    );
    await chooseOption(
      user,
      await screen.findByRole("combobox", { name: "State or union territory" }),
      /Delhi/,
    );
    await user.type(screen.getByLabelText("GSTIN"), "07abcde1234f1z2");
    await user.click(screen.getByRole("button", { name: "Record registration" }));
    await waitFor(() =>
      expect(posted).toEqual([
        { jurisdiction_code: "07", identifier: "07ABCDE1234F1Z2", effective_from: "2026-11-01" },
      ]),
    );
  });

  it("asks why when a registration already starts on that date", async () => {
    const posted = serve(() => [row({})]);
    const { user } = renderWithProviders(
      <TaxRegistrationPanel scope={scope} outlet={outlet} today="2026-11-01" />,
    );
    await screen.findByText("In force today");
    await chooseOption(
      user,
      screen.getByRole("combobox", { name: "State or union territory" }),
      /Maharashtra/,
    );
    await user.type(screen.getByLabelText("GSTIN"), "27PQRST5678K2Z1");
    await user.click(screen.getByRole("button", { name: "Record registration" }));
    expect(await screen.findByText(/Say why you are correcting it/)).toBeVisible();
    expect(posted).toHaveLength(0);
    await user.type(screen.getByLabelText("Reason for a correction (optional)"), "Wrong state");
    await user.click(screen.getByRole("button", { name: "Record registration" }));
    await waitFor(() => expect(posted).toHaveLength(1));
    expect(posted[0]).toMatchObject({ reason: "Wrong state" });
  });

  it("says tax setup is unavailable, with no form, for a country without a scheme", () => {
    serve(() => []);
    renderWithProviders(
      <TaxRegistrationPanel
        scope={scope}
        outlet={{ ...outlet, country_code: "PK", timezone: "Asia/Karachi" }}
        today="2026-11-01"
      />,
    );
    expect(screen.getByText("Tax setup is not available for Pakistan yet.")).toBeVisible();
    expect(screen.queryByRole("button", { name: "Record registration" })).toBeNull();
  });

  it("shows a failure, not an empty history, when the list cannot load", async () => {
    serve(() => [], 503);
    renderWithProviders(<TaxRegistrationPanel scope={scope} outlet={outlet} today="2026-11-01" />);
    expect(await screen.findByText("Could not load tax registrations.")).toBeVisible();
    expect(screen.queryByText("No registration yet")).toBeNull();
  });
});
