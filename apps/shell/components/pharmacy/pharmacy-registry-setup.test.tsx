import { screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import type { PharmacyOrganization } from "@helivanta/api";
import { chooseOption } from "../../test/select";
import { PharmacyRegistrySetup } from "./pharmacy-registry-setup";

const tenant = "11111111-1111-4111-8111-111111111111";
const hospital = "0199e000-0000-7000-8000-000000000020";
const pharmacy: PharmacyOrganization = {
  id: "0199e000-0000-7000-8000-000000000010",
  code: "SYN-PHARMACY",
  name: "Synthetic hospital pharmacy",
  ownership_type: "hospital_owned",
  onboarding_status: "draft",
  created_at: "2026-11-15T00:00:00Z",
};
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });

function registry() {
  return vi.fn(async (url: string, init?: RequestInit) => {
    if (url.endsWith("/iam/me/permissions"))
      return Response.json({
        tenant_id: tenant,
        subject: "synthetic-owner",
        data: ["pharmacy.access.manage", "workforce.hospital.manage"],
      });
    if (url.includes("/workforce/hospitals"))
      return Response.json(
        page([{ id: hospital, name: "Synthetic main hospital", access_level: "manager" }]),
      );
    if (url.includes("/hospital-affiliations") && init?.method === "PUT")
      return Response.json({ id: url.split("/").at(-1), ...JSON.parse(init.body as string) });
    if (url.includes("/hospital-affiliations")) return Response.json(page([]));
    return Response.json({ error: "unexpected_request" }, { status: 400 });
  });
}
afterEach(() => {
  vi.unstubAllGlobals();
  localStorage.clear();
});

it("declares hospital ownership by choosing a named accessible hospital without granting access", async () => {
  const fetcher = registry();
  vi.stubGlobal("fetch", fetcher);
  const { user } = renderWithProviders(
    <PharmacyRegistrySetup tenantId={tenant} pharmacy={pharmacy} />,
  );
  await chooseOption(
    user,
    await screen.findByLabelText("Owning hospital"),
    "Synthetic main hospital",
  );
  await user.click(screen.getByRole("button", { name: "Declare hospital affiliation" }));
  await waitFor(() => {
    const writes = fetcher.mock.calls.filter(([, init]) => init?.method === "PUT");
    expect(writes).toHaveLength(1);
    expect(writes[0][0]).toContain(`/pharmacy/pharmacies/${pharmacy.id}/hospital-affiliations/`);
    expect(JSON.parse(writes[0][1]!.body as string)).toEqual({
      hospital_id: hospital,
      relationship: "owner",
    });
    expect(new Headers(writes[0][1]!.headers).get("X-Helivanta-Expected-Tenant")).toBe(tenant);
  });
  expect(screen.queryByLabelText("Hospital reference ID")).not.toBeInTheDocument();
  expect(screen.getByText(/grants nothing until that hospital verifies it/)).toBeInTheDocument();
});
