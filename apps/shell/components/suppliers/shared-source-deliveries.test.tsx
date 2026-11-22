import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { SharedSourceDeliveries } from "./shared-source-deliveries";
const id = (n: number) => `0199e000-0000-7000-8000-${String(n).padStart(12, "0")}`;
const scope = {
  tenantId: id(1),
  subject: "TEST-receiver",
  hospitalId: id(2),
  grantId: id(3),
  relationshipId: id(4),
  termsSha256: "a".repeat(64),
  source: {
    source_tenant_id: id(5),
    recipient_tenant_id: id(1),
    hospital_id: id(2),
    source_product: "pharmacy" as const,
    source_organization_id: id(6),
    source_location_id: id(7),
  },
};
const source = {
  scope: scope.source,
  grant_id: scope.grantId,
  relationship_id: scope.relationshipId,
  terms_sha256: scope.termsSha256,
  source_organization_name: "TEST Independent Pharmacy",
  source_location_name: "TEST Nagpur Branch",
};
afterEach(() => vi.unstubAllGlobals());
it("conceals cached names and delivery evidence when the current source check fails", async () => {
  let refused = false;
  const request = vi.fn(async (url: string) => {
    if (url.includes("/shipment-source"))
      return refused
        ? Response.json(
            {
              error: "partner_shipment_unavailable",
              message: "TEST current authority unavailable",
            },
            { status: 503 },
          )
        : Response.json(source);
    return Response.json({ data: [], page: { has_more: false, next_cursor: null } });
  });
  vi.stubGlobal("fetch", request);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const view = render(
    <QueryClientProvider client={client}>
      <SharedSourceDeliveries scope={scope} />
    </QueryClientProvider>,
  );
  try {
    expect(await screen.findByText("TEST Independent Pharmacy")).toBeVisible();
    expect(
      await screen.findByText("No published dispatches are currently shared by this source."),
    ).toBeVisible();
    expect(view.container.textContent).not.toContain(scope.grantId);
    refused = true;
    await client.invalidateQueries({ queryKey: ["supplier-sharing", "source", scope] });
    await waitFor(() =>
      expect(screen.queryByText("TEST Independent Pharmacy")).not.toBeInTheDocument(),
    );
    expect(
      screen.queryByText("No published dispatches are currently shared by this source."),
    ).not.toBeInTheDocument();
    expect(
      screen.getByText(
        "This shared source could not be verified. Previously loaded delivery evidence is hidden.",
      ),
    ).toBeVisible();
  } finally {
    view.unmount();
    client.clear();
  }
});
