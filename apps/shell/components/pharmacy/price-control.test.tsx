import { screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import type {
  PharmacyPriceControl,
  PharmacyPriceDeclaration,
  PharmacyProduct,
} from "@helivanta/api";
import { chooseOption } from "../../test/select";
import { ProductPriceControlSection } from "./price-control";

const scope = {
  tenantId: "0199e000-0000-7000-8000-0000000000aa",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};
const product = {
  id: "0199e000-0000-7000-8000-0000000000b1",
  sku: "TEST-PCM",
  currency: "INR",
  status: "active",
} as PharmacyProduct;

const declaration = (over: Partial<PharmacyPriceDeclaration>): PharmacyPriceDeclaration => ({
  id: "0199e000-0000-7000-8000-0000000000d1",
  product_id: product.id,
  controlled: true,
  ceiling_minor: 1000,
  order_reference: "S.O. 1234(E)",
  order_date: "2026-10-01",
  effective_from: "2026-11-01",
  source: "TEST NPPA order",
  declared_by: "user-owner",
  created_at: "2026-11-01T06:30:00Z",
  decision: null,
  decision_reason: null,
  reviewed_by: null,
  reviewed_at: null,
  ...over,
});

function serve(state: PharmacyPriceControl) {
  const sent: { method: string; url: string; body: unknown }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "PUT" || init?.method === "POST") {
        sent.push({ method: init.method, url, body: JSON.parse(String(init.body)) });
        return new Response(JSON.stringify(declaration({})), {
          status: 201,
          headers: { "Content-Type": "application/json" },
        });
      }
      if (url.endsWith("/price-control"))
        return new Response(JSON.stringify(state), {
          headers: { "Content-Type": "application/json" },
        });
      throw new Error(`Unexpected test API request: ${url}`);
    }),
  );
  return sent;
}

const undeclared: PharmacyPriceControl = {
  state: "undeclared",
  today: "2026-11-01",
  in_force: null,
  reason: "no price control has been declared; it sells under its MRP alone",
  history: [],
};

afterEach(() => vi.unstubAllGlobals());

const section = (canDeclare: boolean, canReview: boolean) => (
  <ProductPriceControlSection
    scope={scope}
    product={product}
    today="2026-11-01"
    canDeclare={canDeclare}
    canReview={canReview}
  />
);

describe("ProductPriceControlSection", () => {
  it("declares an NPPA ceiling per unit before GST", async () => {
    const sent = serve(undeclared);
    const { user } = renderWithProviders(section(true, false));
    expect(await screen.findByText(/Not declared/)).toBeVisible();
    const ceiling = screen.getByLabelText("Ceiling per unit, before GST (INR)");
    await user.clear(ceiling);
    await user.type(ceiling, "10.00");
    await user.type(screen.getByLabelText("NPPA order"), "S.O. 1234(E)");
    await user.type(screen.getByLabelText("Order date"), "2026-10-01");
    await user.type(screen.getByLabelText("Source"), "TEST NPPA order");
    await user.click(screen.getByRole("button", { name: "Record declaration" }));
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0].method).toBe("PUT");
    expect(sent[0].url).toMatch(
      /\/catalogue\/products\/[^/]+\/price-control\/[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-/,
    );
    expect(sent[0].body).toEqual({
      price_control: "controlled",
      ceiling_minor: 1000,
      order_reference: "S.O. 1234(E)",
      order_date: "2026-10-01",
      effective_from: "2026-11-01",
      source: "TEST NPPA order",
    });
  });

  it("asks for the order before recording a ceiling", async () => {
    const sent = serve(undeclared);
    const { user } = renderWithProviders(section(true, false));
    const ceiling = await screen.findByLabelText("Ceiling per unit, before GST (INR)");
    await user.clear(ceiling);
    await user.type(ceiling, "10");
    await user.type(screen.getByLabelText("Source"), "TEST NPPA order");
    await user.click(screen.getByRole("button", { name: "Record declaration" }));
    expect(await screen.findByText("Enter the NPPA order or notification number.")).toBeVisible();
    expect(screen.getByText("Enter the order's date.")).toBeVisible();
    expect(sent).toHaveLength(0);
  });

  it("declares a product not price-controlled without a ceiling", async () => {
    const sent = serve(undeclared);
    const { user } = renderWithProviders(section(true, false));
    await chooseOption(
      user,
      await screen.findByRole("combobox", { name: "Scheduled formulation" }),
      "Not price-controlled",
    );
    expect(screen.queryByLabelText("Ceiling per unit, before GST (INR)")).toBeNull();
    await user.type(screen.getByLabelText("Source"), "TEST not in the schedule");
    await user.click(screen.getByRole("button", { name: "Record declaration" }));
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0].body).toEqual({
      price_control: "not_controlled",
      effective_from: "2026-11-01",
      source: "TEST not in the schedule",
    });
  });

  it("never records a ceiling the form no longer shows", async () => {
    const sent = serve(undeclared);
    const { user } = renderWithProviders(section(true, false));
    const ceiling = await screen.findByLabelText("Ceiling per unit, before GST (INR)");
    await user.clear(ceiling);
    await user.type(ceiling, "5.00");
    const kind = screen.getByRole("combobox", { name: "Scheduled formulation" });
    await chooseOption(user, kind, "Not price-controlled");
    await chooseOption(user, kind, "Price-controlled (NPPA ceiling)");
    expect(screen.getByLabelText("Ceiling per unit, before GST (INR)")).toHaveValue("0.00");
    await chooseOption(user, kind, "Not price-controlled");
    await user.type(screen.getByLabelText("Source"), "TEST not in the schedule");
    await user.click(screen.getByRole("button", { name: "Record declaration" }));
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0].body).not.toHaveProperty("ceiling_minor");
  });

  it("submits a not-controlled declaration after an unreadable ceiling was typed", async () => {
    const sent = serve(undeclared);
    const { user } = renderWithProviders(section(true, false));
    const ceiling = await screen.findByLabelText("Ceiling per unit, before GST (INR)");
    await user.clear(ceiling);
    await user.type(ceiling, "1.2.3");
    await chooseOption(
      user,
      screen.getByRole("combobox", { name: "Scheduled formulation" }),
      "Not price-controlled",
    );
    await user.type(screen.getByLabelText("Source"), "TEST not in the schedule");
    await user.click(screen.getByRole("button", { name: "Record declaration" }));
    await waitFor(() => expect(sent).toHaveLength(1));
  });

  it("uses a new id for the next declaration even when history is at its cap", async () => {
    let first = "0199e000-0000-7000-8000-000000000a00";
    const history = () =>
      [
        first,
        ...Array.from(
          { length: 49 },
          (_, i) => `0199e000-0000-7000-8000-0000000001${String(i).padStart(2, "0")}`,
        ),
      ].map((id) =>
        declaration({ id, controlled: false, ceiling_minor: null, decision: "approved" }),
      );
    const sent: string[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init?: RequestInit) => {
        if (init?.method === "PUT") {
          sent.push(url);
          first = `0199e000-0000-7000-8000-000000000a0${sent.length}`;
          return new Response(JSON.stringify(declaration({})), {
            status: 201,
            headers: { "Content-Type": "application/json" },
          });
        }
        return new Response(
          JSON.stringify({
            state: "in_force",
            today: "2026-11-01",
            in_force: null,
            reason: "",
            history: history(),
          }),
          { headers: { "Content-Type": "application/json" } },
        );
      }),
    );
    const { user } = renderWithProviders(section(true, false));
    for (const note of ["TEST first", "TEST second"]) {
      await chooseOption(
        user,
        await screen.findByRole("combobox", { name: "Scheduled formulation" }),
        "Not price-controlled",
      );
      await user.type(screen.getByLabelText("Source"), note);
      await user.click(screen.getByRole("button", { name: "Record declaration" }));
      await waitFor(() => expect(sent).toHaveLength(note === "TEST first" ? 1 : 2));
      await waitFor(() => expect(screen.getByLabelText("Source")).toHaveValue(""));
    }
    expect(sent[0]).not.toBe(sent[1]);
  });

  it("lets a second person approve or reject a pending declaration, with a reason", async () => {
    const sent = serve({
      state: "blocked",
      today: "2026-11-01",
      in_force: null,
      reason: "the latest declaration awaits review by a second person",
      history: [declaration({})],
    });
    const { user } = renderWithProviders(section(false, true));
    expect(await screen.findByText(/Not sellable/)).toBeVisible();
    expect(screen.getByText(/awaits review by a second person/)).toBeVisible();
    expect(screen.queryByRole("button", { name: "Record declaration" })).toBeNull();
    await user.click(screen.getByRole("button", { name: "Reject" }));
    expect(await screen.findByText("Say what you checked.")).toBeVisible();
    expect(sent).toHaveLength(0);
    await user.type(screen.getByLabelText("What you checked"), "Gazette ceiling is lower");
    await user.click(screen.getByRole("button", { name: "Reject" }));
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0]).toMatchObject({
      method: "POST",
      body: { decision: "rejected", reason: "Gazette ceiling is lower" },
    });
    expect(sent[0].url).toMatch(/\/price-control\/0199e000-0000-7000-8000-0000000000d1\/review$/);
  });

  // Review and declaration forms share the pending declaration's id as a key;
  // as siblings that rendered the review form twice.
  it("renders one review form for someone who may both declare and review", async () => {
    const errors = vi.spyOn(console, "error").mockImplementation(() => {});
    serve({
      state: "blocked",
      today: "2026-11-01",
      in_force: null,
      reason: "the latest declaration awaits review by a second person",
      history: [declaration({})],
    });
    renderWithProviders(section(true, true));
    expect(await screen.findByText(/Not sellable/)).toBeVisible();
    expect(screen.getAllByRole("button", { name: "Record declaration" })).toHaveLength(1);
    expect(errors.mock.calls.flat().join(" ")).not.toContain("same key");
    errors.mockRestore();
  });

  it("shows the ceiling in force and its history read-only", async () => {
    const approved = declaration({
      decision: "approved",
      decision_reason: "checked",
      reviewed_by: "user-pharmacist",
    });
    serve({
      state: "in_force",
      today: "2026-11-01",
      in_force: approved,
      reason: "",
      history: [approved],
    });
    renderWithProviders(section(false, false));
    expect(
      await screen.findByText("In force: NPPA ceiling INR 10.00 per unit + GST"),
    ).toBeVisible();
    expect(screen.getByText(/Approved by user-pharmacist/)).toBeVisible();
    expect(screen.queryByRole("button", { name: "Approve" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Record declaration" })).toBeNull();
  });
});
