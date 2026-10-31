import { screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import type { PharmacyOutlet } from "@helivanta/api";
import { OverviewTiles, WaitingTile } from "./overview-tiles";

vi.mock("next/navigation", () => ({ useRouter: () => ({ push: vi.fn() }) }));

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

const full = {
  as_of: "2026-11-01T12:00:00+05:30",
  time_zone: "Asia/Kolkata",
  stock: { expired: 1, due_30: 2, due_90: 4 },
  safety: { recent: 3 },
  bills: { overdue: 1, week: 2 },
  setup: { unclassified: 5, registration_in_force: false },
};

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

function serve(opts: { overview?: unknown; overviewStatus?: number; granted?: string[] } = {}) {
  const granted = opts.granted ?? [
    "pharmacy.dispense.read",
    "pharmacy.catalogue.read",
    "pharmacy.stock.read",
    "pharmacy.accounts.read",
    "pharmacy.tax.manage",
  ];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.includes("/iam/me/permissions")) return json({ data: granted });
      if (url.includes("/dispenses/waiting"))
        return json({
          data: [
            {
              id: "d-1",
              patient_name: "Asha Rao",
              status: "pending",
              created_at: "2026-11-01T03:15:00Z",
            },
          ],
          page: { next_cursor: "c", has_more: true, total: 12, offset: 0 },
        });
      if (url.endsWith("/overview"))
        return opts.overviewStatus
          ? json({ error: "unavailable" }, opts.overviewStatus)
          : json(opts.overview ?? full);
      throw new Error(`Unexpected test API request: ${url}`);
    }),
  );
}

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

const tile = (name: string) => screen.getByRole("region", { name });

describe("OverviewTiles", () => {
  it("shows the counts in order of urgency", async () => {
    serve();
    renderWithProviders(<OverviewTiles scope={scope} outlet={outlet} />);
    expect(await screen.findByRole("region", { name: "Setup gaps" })).toBeVisible();
    const order = within(screen.getByTestId("overview-tiles"))
      .getAllByRole("region")
      .map((r) => r.getAttribute("aria-label"));
    expect(order).toEqual(["Stock expiring", "Safety concerns", "Supplier bills", "Setup gaps"]);
  });

  it("counts patients waiting hospital-wide, linking to the queue", async () => {
    serve();
    renderWithProviders(<WaitingTile />);
    expect(await screen.findByText("12")).toBeVisible();
    expect(within(tile("Patients waiting")).getByText(/hospital-wide/i)).toBeVisible();
    expect(within(tile("Patients waiting")).getByText(/Oldest waiting since/)).toBeVisible();
    expect(
      within(tile("Patients waiting")).getByRole("link", { name: /waiting/i }),
    ).toHaveAttribute("href", "/pharmacy/dispense?tab=waiting");
  });

  // Every number opens the list it counts, already filtered.
  it("links each count to its filtered list", async () => {
    serve();
    renderWithProviders(<OverviewTiles scope={scope} outlet={outlet} />);
    await screen.findByRole("region", { name: "Setup gaps" });
    const href = (region: string, name: RegExp) =>
      within(tile(region)).getByRole("link", { name }).getAttribute("href");
    expect(href("Stock expiring", /Expired/)).toBe(
      "/pharmacy/stock?balance=on_hand&expiry=expired",
    );
    expect(href("Stock expiring", /Within 30 days/)).toBe(
      "/pharmacy/stock?balance=on_hand&expiry=due_30",
    );
    expect(href("Stock expiring", /Within 90 days/)).toBe(
      "/pharmacy/stock?balance=on_hand&expiry=due_90",
    );
    expect(href("Safety concerns", /last 30 days/)).toBe("/pharmacy/safety?tab=recent");
    expect(href("Supplier bills", /Overdue/)).toBe("/pharmacy/accounts?tab=overdue");
    expect(href("Supplier bills", /Due within 7 days/)).toBe("/pharmacy/accounts?tab=week");
    expect(href("Setup gaps", /Not classified/)).toBe("/pharmacy/catalogue?tax=unclassified");
    expect(href("Setup gaps", /GSTIN/)).toBe("/pharmacy/setup");
  });

  // An absent section means "not permitted": no tile, never a zero.
  it("shows no tile for a section the server did not return", async () => {
    serve({
      overview: { ...full, bills: undefined, setup: { unclassified: 0 } },
      granted: ["pharmacy.catalogue.read", "pharmacy.stock.read"],
    });
    renderWithProviders(<OverviewTiles scope={scope} outlet={outlet} />);
    expect(await screen.findByRole("region", { name: "Stock expiring" })).toBeVisible();
    expect(screen.queryByRole("region", { name: "Supplier bills" })).toBeNull();
    expect(within(tile("Setup gaps")).queryByText(/GSTIN/)).toBeNull();
    expect(within(tile("Setup gaps")).getByText(/Every product has a tax rate/)).toBeVisible();
  });

  // A failure is never a zero.
  it("says the outlet counts could not load, without zeros", async () => {
    serve({ overviewStatus: 503 });
    renderWithProviders(<OverviewTiles scope={scope} outlet={outlet} />);
    expect(await screen.findByText("Could not load this outlet's counts.")).toBeVisible();
    expect(screen.getByRole("button", { name: "Try again" })).toBeVisible();
    expect(screen.queryByText("0")).toBeNull();
  });

  it("shows when the outlet counts were read, in the outlet's time zone", async () => {
    serve();
    renderWithProviders(<OverviewTiles scope={scope} outlet={outlet} />);
    expect(await screen.findByText(/As of 12:00/)).toBeVisible();
  });

  // A wait that began on an earlier day must say which day, not just a time.
  it("dates the oldest wait when it began before today", async () => {
    // Two days after the wait began, in any timezone, whatever today is.
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date("2026-11-03T03:15:00Z"));
    serve();
    renderWithProviders(<WaitingTile />);
    const note = await within(
      await screen.findByRole("region", { name: "Patients waiting" }),
    ).findByText(/Oldest waiting since/);
    expect(note.textContent).toMatch(/2026/);
  });

  // A country with no tax scheme has no tax gaps: no Setup tile at all.
  it("shows no Setup tile where the outlet has no tax scheme", async () => {
    serve({ overview: { ...full, setup: undefined } });
    renderWithProviders(<OverviewTiles scope={scope} outlet={outlet} />);
    expect(await screen.findByRole("region", { name: "Stock expiring" })).toBeVisible();
    expect(screen.queryByRole("region", { name: "Setup gaps" })).toBeNull();
  });
});
