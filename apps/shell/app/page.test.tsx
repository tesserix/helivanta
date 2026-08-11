import { screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@hms/api/testing";
import Dashboard from "./page";

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function mockPermissions(perms: string[]) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockImplementation((url: string) => {
      if (url.includes("/iam/me/permissions")) {
        return Promise.resolve(jsonResponse(200, { data: perms }));
      }
      return Promise.resolve(jsonResponse(200, { data: [] }));
    }),
  );
}

afterEach(() => vi.unstubAllGlobals());

// The dashboard must show only doors that will open (zones.ts's own
// contract) — the same visibleZones(can) filter the sidebar already
// applies. Regression coverage for a gap found in Task 14 review: the
// dashboard used to render one card per zone unfiltered, so a pharmacist
// saw MediCore/Lab cards leading nowhere useful.
describe("Dashboard", () => {
  it("shows a card for every zone page the user holds permission for", async () => {
    mockPermissions([
      "medicore.visit.read",
      "pharmacy.dispense.read",
      "pharmacy.medication.read",
      "lab.order.read",
    ]);
    renderWithProviders(<Dashboard />);

    for (const title of ["OPD", "IPD", "Dispenses", "Medications", "Lab"]) {
      expect(await screen.findByText(title)).toBeInTheDocument();
    }
  });

  it("hides cards for zones the user lacks permission for (pharmacist)", async () => {
    mockPermissions(["pharmacy.dispense.read", "pharmacy.medication.read"]);
    renderWithProviders(<Dashboard />);

    expect(await screen.findByText("Dispenses")).toBeInTheDocument();
    expect(screen.getByText("Medications")).toBeInTheDocument();
    expect(screen.queryByText("OPD")).not.toBeInTheDocument();
    expect(screen.queryByText("IPD")).not.toBeInTheDocument();
    expect(screen.queryByText("Lab")).not.toBeInTheDocument();
  });

  it("tints each department's icon chip by the zone's hue", async () => {
    mockPermissions([
      "medicore.visit.read",
      "pharmacy.dispense.read",
      "pharmacy.medication.read",
      "lab.order.read",
    ]);
    renderWithProviders(<Dashboard />);

    const opdCard = (await screen.findByText("OPD")).closest("a");
    const labCard = (await screen.findByText("Lab")).closest("a");

    expect(opdCard?.querySelector("span")).toHaveClass("bg-(--hms-rose-tint)", "text-(--hms-rose)");
    expect(labCard?.querySelector("span")).toHaveClass(
      "bg-(--hms-violet-tint)",
      "text-(--hms-violet)",
    );
  });

  it("shows no zone cards while permissions are still loading", () => {
    // A fetch that never resolves keeps usePermissions().isLoading true —
    // can() denies while loading, so cards must not flash before the
    // real permission set arrives.
    vi.stubGlobal(
      "fetch",
      vi.fn(() => new Promise<Response>(() => {})),
    );
    renderWithProviders(<Dashboard />);

    expect(screen.queryByText("OPD")).not.toBeInTheDocument();
    expect(screen.queryByText("Dispenses")).not.toBeInTheDocument();
    expect(screen.queryByText("Lab")).not.toBeInTheDocument();
  });
});
