import { screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { formatZonedDateTime, type PharmacyProduct } from "@helivanta/api";
import { ProductScheduleSection } from "./schedule-class";

const scope = {
  tenantId: "11111111-1111-1111-1111-111111111111",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};

const product = {
  id: "0199e000-0000-7000-8000-000000000101",
  sku: "TEST-AMOX-500",
  brand_name: "TEST Amoxil",
  status: "active",
} as PharmacyProduct;

const declaration = {
  id: "0199e000-0000-7000-8000-000000000701",
  product_id: product.id,
  regulatory_class: "H1",
  ndps: false,
  source: "TEST pack label",
  declared_by: "user-declarer",
  created_at: "2026-11-09T04:00:00Z",
  decision: null,
  decision_reason: null,
  reviewed_by: null,
  reviewed_at: null,
};
const approved = {
  ...declaration,
  decision: "approved",
  decision_reason: "TEST pack checked",
  reviewed_by: "user-reviewer",
  reviewed_at: "2026-11-09T05:00:00Z",
};
const inForce = (row: typeof approved) => ({
  in_force: row,
  origin: "declared_by_outlet",
  reason: "",
  history: [row],
});
const awaiting = {
  in_force: null,
  origin: "declared_by_outlet",
  reason: "the latest declaration awaits review by a second person",
  history: [declaration],
};

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

type Answer = { status?: number; body: unknown };
function serve(state: unknown | Answer[], writes: Answer[] = []) {
  const reads = Array.isArray(state) ? [...state] : null;
  const fetcher = vi.fn<(url: string, init?: RequestInit) => Promise<Response>>(
    async (_url, init) => {
      const answer =
        init?.method === "PUT" || init?.method === "POST"
          ? (writes.shift() ?? { status: 201, body: declaration })
          : reads
            ? reads.length > 1
              ? reads.shift()!
              : reads[0]!
            : { body: state };
      return new Response(JSON.stringify(answer.body), { status: answer.status ?? 200 });
    },
  );
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}

const reads = (fetcher: ReturnType<typeof serve>) =>
  fetcher.mock.calls.filter(([url, init]) => !init?.method && url.endsWith("/schedule"));

function renderSection(
  props: Partial<Parameters<typeof ProductScheduleSection>[0]> = {},
): ReturnType<typeof renderWithProviders> {
  return renderWithProviders(
    <ProductScheduleSection
      scope={scope}
      product={product}
      timeZone="Asia/Kolkata"
      canDeclare={false}
      canReview={false}
      {...props}
    />,
  );
}

describe("ProductScheduleSection", () => {
  it("shows the class in force with its approval time", async () => {
    serve(inForce(approved));
    renderSection();
    expect(
      await screen.findByText(
        `Schedule H1 · not NDPS, approved ${formatZonedDateTime(approved.reviewed_at, "Asia/Kolkata")}`,
      ),
    ).toBeVisible();
    const history = screen.getByRole("list", { name: "Schedule class history" });
    expect(history).toHaveTextContent("TEST pack label");
    expect(history).toHaveTextContent("user-declarer");
    expect(history).toHaveTextContent("Approved by user-reviewer: TEST pack checked");
  });

  it("reads class none as not scheduled", async () => {
    serve(inForce({ ...approved, regulatory_class: "none" }));
    renderSection();
    expect(await screen.findByText(/^Not scheduled · not NDPS, approved/)).toBeVisible();
  });

  it("says none is in force, with the API's reason", async () => {
    serve(awaiting);
    renderSection();
    expect(
      await screen.findByText(
        "No schedule class in force: the latest declaration awaits review by a second person",
      ),
    ).toBeVisible();
    expect(screen.getByText(/cannot be released or sold/)).toBeVisible();
  });

  it.each([
    ["Schedule X", { ...approved, regulatory_class: "X" }],
    ["NDPS", { ...approved, ndps: true }],
  ])("warns that %s cannot be released or sold yet", async (_, row) => {
    serve(inForce(row));
    renderSection();
    expect(
      await screen.findByText(/Schedule X and NDPS products cannot be released or sold/),
    ).toBeVisible();
  });

  it("says only the latest 50 declarations are shown", async () => {
    const history = Array.from({ length: 50 }, (_, i) => ({
      ...approved,
      id: `0199e000-0000-7000-8000-${String(i).padStart(12, "0")}`,
    }));
    serve({ ...inForce(approved), history });
    renderSection();
    expect(await screen.findByText("Showing the latest 50 declarations.")).toBeVisible();
  });

  it("reports a failed read through RequestFailure, and asks again", async () => {
    const fetcher = serve([
      { status: 503, body: { error: "unavailable", message: "no" } },
      { body: inForce(approved) },
    ]);
    const { user } = renderSection();
    expect(await screen.findByText("Could not load the schedule class.")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByText(/^Schedule H1 · not NDPS, approved/)).toBeVisible();
    expect(reads(fetcher)).toHaveLength(2);
  });

  it("ends the session on a 401 with no retry", async () => {
    serve([{ status: 401, body: { error: "unauthenticated", message: "no" } }]);
    renderSection();
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Your session has ended. Sign in again to continue.",
    );
    expect(screen.queryByRole("button", { name: "Try again" })).toBeNull();
  });

  it("offers no forms without the grants", async () => {
    serve(awaiting);
    renderSection();
    await screen.findByText(/No schedule class in force/);
    expect(screen.queryByRole("button", { name: "Declare schedule class" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Approve" })).toBeNull();
  });
});
