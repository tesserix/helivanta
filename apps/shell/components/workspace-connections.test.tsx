import { screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { WorkspaceConnections } from "./workspace-connections";

afterEach(() => vi.unstubAllGlobals());

function permissions(data: string[]) {
  const fetcher = vi.fn(async (url: string) =>
    url.endsWith("/iam/me/permissions")
      ? new Response(
          JSON.stringify({
            tenant_id: "11111111-1111-1111-1111-111111111111",
            subject: "TEST-workspace-user",
            data,
          }),
        )
      : new Response(null, { status: 404 }),
  );
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}

it("connects Supply Connect to the permitted PharmaConnect screens", async () => {
  permissions(["supply.directory.read", "pharmacy.catalogue.read", "pharmacy.stock.read"]);
  renderWithProviders(<WorkspaceConnections current="supply" />);
  expect(await screen.findByRole("link", { name: "Product catalogue" })).toHaveAttribute(
    "href",
    "/pharmacy/catalogue",
  );
  expect(screen.getByRole("link", { name: "Stock receiving" })).toHaveAttribute(
    "href",
    "/pharmacy/stock",
  );
  expect(screen.queryByRole("link", { name: "Dispenses" })).not.toBeInTheDocument();
  expect(screen.queryByRole("link", { name: "Expenses & audit" })).not.toBeInTheDocument();
});

it("explains reviewer-only access without requesting pharmacy data", async () => {
  const fetcher = permissions(["supply.directory.read", "supply.license.review.read"]);
  renderWithProviders(<WorkspaceConnections current="supply" />);
  expect(
    await screen.findByText(/This account does not have pharmacy permissions/),
  ).toBeInTheDocument();
  expect(screen.getByRole("heading", { name: "PharmaConnect" })).toBeInTheDocument();
  expect(screen.queryByRole("link")).not.toBeInTheDocument();
  expect(fetcher.mock.calls.every(([url]) => String(url).endsWith("/iam/me/permissions"))).toBe(
    true,
  );
});

it("connects PharmaConnect to the authorised supplier directory", async () => {
  permissions(["supply.directory.read", "pharmacy.catalogue.read"]);
  renderWithProviders(<WorkspaceConnections current="pharmacy" />);
  expect(await screen.findByRole("link", { name: "Organisations & suppliers" })).toHaveAttribute(
    "href",
    "/supply/parties",
  );
});
