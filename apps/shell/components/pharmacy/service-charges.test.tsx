import { screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import type { PharmacyServiceCharge, PharmacyServiceChargeVersion } from "@helivanta/api";
import { chooseOption } from "../../test/select";
import { ServiceChargesSetup } from "./service-charges";

const scope = {
  tenantId: "0199e000-0000-7000-8000-0000000000aa",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};
const chargeId = "0199e000-0000-7000-8000-0000000000c1";

const version = (over: Partial<PharmacyServiceChargeVersion>): PharmacyServiceChargeVersion => ({
  id: "0199e000-0000-7000-8000-0000000000c2",
  charge_id: chargeId,
  code: "DELIVERY",
  name: "Ward delivery",
  channel: "ward_delivery",
  amount_minor: 5000,
  status: "available",
  tax_treatment: "own_service",
  sac_code: "996812",
  supply_nature: "taxable",
  rate_ppm: 180000,
  exemption_basis: null,
  tax_basis: "TEST accountant note 1",
  effective_from: "2026-11-01",
  declared_by: "user-owner",
  created_at: "2026-11-01T06:30:00Z",
  decision: "approved",
  decision_reason: "checked",
  reviewed_by: "user-manager",
  reviewed_at: "2026-11-01T07:00:00Z",
  ...over,
});

function serve(items: PharmacyServiceCharge[]) {
  const sent: { method: string; url: string; body: unknown }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "PUT" || init?.method === "POST") {
        sent.push({ method: init.method, url, body: JSON.parse(String(init.body)) });
        return new Response(JSON.stringify(version({})), {
          status: 201,
          headers: { "Content-Type": "application/json" },
        });
      }
      if (url.endsWith("/service-charges"))
        return new Response(JSON.stringify({ items }), {
          headers: { "Content-Type": "application/json" },
        });
      throw new Error(`Unexpected test API request: ${url}`);
    }),
  );
  return sent;
}

afterEach(() => vi.unstubAllGlobals());

const setup = (canDeclare: boolean, canReview: boolean) => (
  <ServiceChargesSetup
    scope={scope}
    currency="INR"
    today="2026-11-01"
    canDeclare={canDeclare}
    canReview={canReview}
  />
);

describe("ServiceChargesSetup", () => {
  it("records a service of its own with its SAC and rate", async () => {
    const sent = serve([]);
    const { user } = renderWithProviders(setup(true, false));
    await user.click(await screen.findByRole("button", { name: "New service charge" }));
    await user.type(screen.getByLabelText("Code"), "delivery");
    await user.type(screen.getByLabelText("Name on the invoice"), "Ward delivery");
    const amount = screen.getByLabelText("Amount, GST included (INR)");
    await user.clear(amount);
    await user.type(amount, "50.00");
    await chooseOption(
      user,
      screen.getByRole("combobox", { name: "Applies to" }),
      "Ward delivery (billed to an admission)",
    );
    await chooseOption(
      user,
      screen.getByRole("combobox", { name: "Tax treatment" }),
      "A service of its own (SAC and rate)",
    );
    await user.click(screen.getByRole("button", { name: "Record version" }));
    expect(await screen.findByText("A SAC is 99 and four digits")).toBeVisible();
    expect(screen.getByText("Enter the GST rate, at most two decimals")).toBeVisible();
    expect(sent).toHaveLength(0);

    await user.type(screen.getByLabelText("SAC"), "996812");
    await user.type(screen.getByLabelText("GST rate %"), "18");
    await user.type(screen.getByLabelText("Tax basis"), "TEST accountant note 1");
    await user.click(screen.getByRole("button", { name: "Record version" }));
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0].method).toBe("PUT");
    expect(sent[0].url).toMatch(
      /\/service-charges\/[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[0-9a-f-]+\/versions\/[0-9a-f]{8}-[0-9a-f]{4}-7/,
    );
    expect(sent[0].body).toEqual({
      code: "DELIVERY",
      name: "Ward delivery",
      channel: "ward_delivery",
      amount_minor: 5000,
      status: "available",
      tax_treatment: "own_service",
      sac_code: "996812",
      supply_nature: "taxable",
      rate_ppm: 180000,
      tax_basis: "TEST accountant note 1",
      effective_from: "2026-11-01",
    });
  });

  it("records a charge taxed as the medicines with no tax of its own", async () => {
    const sent = serve([]);
    const { user } = renderWithProviders(setup(true, false));
    await user.click(await screen.findByRole("button", { name: "New service charge" }));
    await user.type(screen.getByLabelText("Code"), "HANDLING");
    await user.type(screen.getByLabelText("Name on the invoice"), "Handling");
    const amount = screen.getByLabelText("Amount, GST included (INR)");
    await user.clear(amount);
    await user.type(amount, "20");
    expect(screen.queryByLabelText("SAC")).toBeNull();
    await user.type(screen.getByLabelText("Tax basis"), "TEST composite supply, CGST s.8(a)");
    await user.click(screen.getByRole("button", { name: "Record version" }));
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0].body).toEqual({
      code: "HANDLING",
      name: "Handling",
      channel: "counter",
      amount_minor: 2000,
      status: "available",
      tax_treatment: "principal_supply",
      tax_basis: "TEST composite supply, CGST s.8(a)",
      effective_from: "2026-11-01",
    });
  });

  it("shows what is billed today and has a second person review the latest version", async () => {
    const pending = version({
      id: "0199e000-0000-7000-8000-0000000000c3",
      amount_minor: 6000,
      decision: null,
      decision_reason: null,
      reviewed_by: null,
      reviewed_at: null,
    });
    const sent = serve([
      { id: chargeId, code: "DELIVERY", today: "2026-11-01", in_force: null, latest: pending },
    ]);
    const { user } = renderWithProviders(setup(false, true));
    expect(await screen.findByText("Not billed today")).toBeVisible();
    expect(screen.getByText(/awaiting review, declared by user-owner/)).toBeVisible();
    expect(screen.queryByRole("button", { name: "New service charge" })).toBeNull();
    expect(screen.queryByRole("button", { name: /Change DELIVERY/ })).toBeNull();
    await user.click(screen.getByRole("button", { name: "Approve" }));
    expect(await screen.findByText("Say what you checked.")).toBeVisible();
    await user.type(screen.getByLabelText("What you checked"), "matches the accountant's note");
    await user.click(screen.getByRole("button", { name: "Approve" }));
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0]).toEqual({
      method: "POST",
      url: expect.stringMatching(
        new RegExp(`/service-charges/${chargeId}/versions/${pending.id}/review$`),
      ),
      body: { decision: "approved", reason: "matches the accountant's note" },
    });
  });

  it("changes a charge from its latest terms, keeping its code", async () => {
    const current = version({});
    const sent = serve([
      { id: chargeId, code: "DELIVERY", today: "2026-11-01", in_force: current, latest: current },
    ]);
    const { user } = renderWithProviders(setup(true, false));
    expect(
      await screen.findByText(
        /Billed today: Ward delivery · INR 50.00 · ward delivery · SAC 996812, 18% GST/,
      ),
    ).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Change DELIVERY" }));
    expect(screen.getByLabelText("Code")).toHaveAttribute("readonly");
    await chooseOption(user, screen.getByRole("combobox", { name: "Status" }), "Withdrawn");
    await user.click(screen.getByRole("button", { name: "Record version" }));
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0].url).toContain(`/service-charges/${chargeId}/versions/`);
    expect(sent[0].body).toMatchObject({ code: "DELIVERY", status: "withdrawn", rate_ppm: 180000 });
  });
});
