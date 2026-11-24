import { screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { LowStockOverview } from "./low-stock-overview";

vi.mock("next/navigation", () => ({ useRouter: () => ({ push: vi.fn() }) }));
const scope = {
  tenantId: "0199e000-0000-7000-8000-0000000000aa",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};
const summary = {
  outlet_id: scope.outletId,
  low_stock: 172,
  observed_at: "2026-11-24T10:00:00Z",
  local_date: "2026-11-24",
  time_zone: "Asia/Kolkata",
};
const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status });
afterEach(() => vi.unstubAllGlobals());

it("links the owner's exact count to its scoped warning list without fetching a partial list", async () => {
  const fetch = vi.fn().mockResolvedValue(json(summary));
  vi.stubGlobal("fetch", fetch);
  renderWithProviders(<LowStockOverview scope={scope} />);
  expect(await screen.findByRole("link", { name: /Low-stock products172/ })).toHaveAttribute(
    "href",
    `/pharmacy/low-stock?pharmacy=${scope.pharmacyId}&outlet=${scope.outletId}`,
  );
  expect(screen.getByText(/Asia\/Kolkata/)).toBeVisible();
  expect(
    fetch.mock.calls.every(([url]) => String(url).endsWith("/stock/low-products/overview")),
  ).toBe(true);
});

it("hides a previously loaded count after a temporary outage and restores it only after a successful retry", async () => {
  let allowed = true;
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => (allowed ? json(summary) : json({ error: "unavailable" }, 503))),
  );
  const { user } = renderWithProviders(<LowStockOverview scope={scope} />);
  await screen.findByRole("link", { name: /Low-stock products172/ });
  allowed = false;
  await user.click(screen.getByRole("button", { name: "Refresh low-stock overview" }));
  expect(await screen.findByText("The low-stock count could not be loaded.")).toBeVisible();
  expect(screen.queryByRole("link", { name: /Low-stock products/ })).not.toBeInTheDocument();
  expect(screen.queryByText("172")).not.toBeInTheDocument();
  allowed = true;
  await user.click(screen.getByRole("button", { name: "Try again" }));
  await waitFor(() =>
    expect(screen.getByRole("link", { name: /Low-stock products172/ })).toBeVisible(),
  );
});

it("shows an observed zero only when the owner returns a successful exact zero", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(json({ ...summary, low_stock: 0 })));
  renderWithProviders(<LowStockOverview scope={scope} />);
  expect(await screen.findByRole("link", { name: /Low-stock products0/ })).toBeVisible();
});

it("removes the old count and retry action when outlet authority is revoked", async () => {
  let allowed = true;
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => (allowed ? json(summary) : json({ error: "not_found" }, 404))),
  );
  const { user } = renderWithProviders(<LowStockOverview scope={scope} />);
  await screen.findByRole("link", { name: /Low-stock products172/ });
  allowed = false;
  await user.click(screen.getByRole("button", { name: "Refresh low-stock overview" }));
  expect(
    await screen.findByText("This wasn't found, or you don't have access to it."),
  ).toBeVisible();
  expect(screen.queryByRole("link", { name: /Low-stock products/ })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
});
