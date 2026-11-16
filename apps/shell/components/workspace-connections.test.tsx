import { screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { releaseSnapshotForTest, renderWithProviders } from "@helivanta/api/testing";
import { WorkspaceConnections } from "./workspace-connections";

afterEach(() => vi.unstubAllGlobals());

function permissions(data: string[], released?: string[]) {
  const scope = {
    tenantId: "11111111-1111-1111-1111-111111111111",
    subject: "TEST-workspace-user",
  };
  const fetcher = vi.fn(async (url: string) =>
    url.endsWith("/workspace/release-capabilities")
      ? Response.json(releaseSnapshotForTest(scope, released))
      : url.endsWith("/iam/me/permissions")
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
  expect(screen.getByRole("link", { name: "Stock on hand" })).toHaveAttribute(
    "href",
    "/pharmacy/stock",
  );
  expect(screen.queryByRole("link", { name: "Past dispenses" })).not.toBeInTheDocument();
  expect(screen.queryByRole("link", { name: "Supplier bills" })).not.toBeInTheDocument();
});

it("explains reviewer-only access without requesting pharmacy data", async () => {
  const fetcher = permissions(["supply.directory.read", "supply.license.review.read"]);
  renderWithProviders(<WorkspaceConnections current="supply" />);
  expect(
    await screen.findByText(/This account does not have pharmacy permissions/),
  ).toBeInTheDocument();
  expect(screen.getByRole("heading", { name: "PharmaConnect" })).toBeInTheDocument();
  expect(screen.queryByRole("link")).not.toBeInTheDocument();
  expect(
    fetcher.mock.calls.every(([url]) =>
      ["/iam/me/permissions", "/workspace/release-capabilities"].some((path) =>
        String(url).endsWith(path),
      ),
    ),
  ).toBe(true);
});

it("connects PharmaConnect to the authorised supplier directory", async () => {
  permissions(["supply.directory.read", "pharmacy.catalogue.read"]);
  renderWithProviders(<WorkspaceConnections current="pharmacy" />);
  expect(await screen.findByRole("link", { name: "Organisations & suppliers" })).toHaveAttribute(
    "href",
    "/supply/parties",
  );
});

it("does not advertise unreleased supplier work even to a permission-eligible owner", async () => {
  const fetcher = permissions(["supply.directory.read", "pharmacy.catalogue.read"], []);
  renderWithProviders(<WorkspaceConnections current="pharmacy" />);
  await waitFor(() =>
    expect(
      fetcher.mock.calls.some(([url]) => url.endsWith("/workspace/release-capabilities")),
    ).toBe(true),
  );
  expect(screen.queryByRole("heading")).not.toBeInTheDocument();
  expect(screen.queryByRole("link")).not.toBeInTheDocument();
});
