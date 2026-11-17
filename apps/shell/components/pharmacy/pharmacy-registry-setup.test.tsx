import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import type { PharmacyOrganization } from "@helivanta/api";
import { refusedDefault } from "@helivanta/ui";
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

it("withholds hospital choices when the owner response repeats a hospital identity", async () => {
  const base = registry();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) =>
      url.includes("/workforce/hospitals")
        ? Response.json(
            page([
              { id: hospital, name: "Synthetic main hospital", access_level: "manager" },
              { id: hospital, name: "Conflicting hospital name", access_level: "manager" },
            ]),
          )
        : base(url, init),
    ),
  );
  renderWithProviders(<PharmacyRegistrySetup tenantId={tenant} pharmacy={pharmacy} />);
  expect(await screen.findByRole("alert")).toHaveTextContent("Could not load your hospitals.");
  expect(screen.queryByLabelText("Owning hospital")).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Declare hospital affiliation" })).toBeDisabled();
});

it("continues an empty hospital page without claiming that the owner must register another hospital", async () => {
  const base = registry();
  const fetcher = vi.fn(async (url: string, init?: RequestInit) =>
    url.includes("/workforce/hospitals") && !url.includes("cursor=")
      ? Response.json({ data: [], page: { has_more: true, next_cursor: "next-hospitals" } })
      : base(url, init),
  );
  vi.stubGlobal("fetch", fetcher);
  const { user } = renderWithProviders(
    <PharmacyRegistrySetup tenantId={tenant} pharmacy={pharmacy} />,
  );
  const more = await screen.findByRole("button", { name: "Load more hospitals" });
  expect(screen.queryByText(/Register your hospital in Staff/)).not.toBeInTheDocument();
  await user.click(more);
  await chooseOption(user, screen.getByLabelText("Owning hospital"), "Synthetic main hospital");
  expect(screen.getByRole("button", { name: "Declare hospital affiliation" })).toBeEnabled();
  const requests = fetcher.mock.calls.filter(([url]) => url.includes("/workforce/hospitals"));
  expect(requests).toHaveLength(2);
  expect(requests[1][0]).toContain("cursor=next-hospitals");
  expect(new Headers(requests[1][1]!.headers).get("X-Helivanta-Expected-Tenant")).toBe(tenant);
  expect(new Headers(requests[1][1]!.headers).get("X-Helivanta-Expected-Subject")).toBe(
    "synthetic-owner",
  );
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

it.each([403, 503])(
  "withholds the selected hospital and refuses form submission after lookup failure (%s)",
  async (status) => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const base = registry();
    let refused = false;
    const fetcher = vi.fn(async (url: string, init?: RequestInit) =>
      refused && url.includes("/workforce/hospitals")
        ? Response.json({ error: "hospital_unavailable" }, { status })
        : base(url, init),
    );
    vi.stubGlobal("fetch", fetcher);
    const { user } = renderWithProviders(
      <QueryClientProvider client={client}>
        <PharmacyRegistrySetup tenantId={tenant} pharmacy={pharmacy} />
      </QueryClientProvider>,
    );
    await chooseOption(
      user,
      await screen.findByLabelText("Owning hospital"),
      "Synthetic main hospital",
    );
    const submit = screen.getByRole("button", { name: "Declare hospital affiliation" });
    const form = submit.closest("form")!;
    refused = true;
    await act(async () => {
      await client.invalidateQueries({
        queryKey: ["pharmacy", tenant, "synthetic-owner", pharmacy.id, "ownership-hospitals"],
      });
    });
    expect(await screen.findByRole("alert")).toHaveTextContent(
      status === 403 ? refusedDefault : "Could not load your hospitals.",
    );
    expect(screen.queryByLabelText("Owning hospital")).not.toBeInTheDocument();
    expect(submit).toBeDisabled();
    await act(async () => {
      fireEvent.submit(form);
    });
    expect(fetcher.mock.calls.filter(([, init]) => init?.method === "PUT")).toHaveLength(0);
  },
);

it("shows an accessible hospital name on its pending affiliation row", async () => {
  const base = registry();
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    if (url.includes("/hospital-affiliations") && (init?.method ?? "GET") === "GET")
      return Response.json(
        page([
          {
            id: "0199e000-0000-7000-8000-000000000030",
            pharmacy_id: pharmacy.id,
            hospital_id: hospital,
            hospital_name: null,
            relationship: "owner",
            verification_status: "requested",
            verification_request_id: "0199e000-0000-7000-8000-0000000000aa",
            verified_at: null,
            created_at: "2026-11-15T00:00:00Z",
          },
        ]),
      );
    return base(url, init);
  });
  vi.stubGlobal("fetch", fetcher);
  renderWithProviders(<PharmacyRegistrySetup tenantId={tenant} pharmacy={pharmacy} />);
  const row = (await screen.findByText(/Waiting for the hospital to verify/)).closest("li")!;
  await waitFor(() => expect(within(row).getByText("Synthetic main hospital")).toBeInTheDocument());
  expect(within(row).queryByText(hospital)).not.toBeInTheDocument();
  expect(
    fetcher.mock.calls.filter(([, init]) => init?.method && init.method !== "GET"),
  ).toHaveLength(0);
});

it.each([403, 503])(
  "withholds a pending affiliation's directory name after lookup refusal (%s)",
  async (status) => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const base = registry();
    let refused = false;
    const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
      if (url.includes("/workforce/hospitals") && refused)
        return Response.json({ error: "hospital_unavailable" }, { status });
      if (url.includes("/hospital-affiliations") && (init?.method ?? "GET") === "GET")
        return Response.json(
          page([
            {
              id: "0199e000-0000-7000-8000-000000000030",
              pharmacy_id: pharmacy.id,
              hospital_id: hospital,
              hospital_name: null,
              relationship: "owner",
              verification_status: "requested",
              verification_request_id: "0199e000-0000-7000-8000-0000000000aa",
              verified_at: null,
              created_at: "2026-11-15T00:00:00Z",
            },
          ]),
        );
      return base(url, init);
    });
    vi.stubGlobal("fetch", fetcher);
    renderWithProviders(
      <QueryClientProvider client={client}>
        <PharmacyRegistrySetup tenantId={tenant} pharmacy={pharmacy} />
      </QueryClientProvider>,
    );
    const row = (await screen.findByText(/Waiting for the hospital to verify/)).closest("li")!;
    await waitFor(() =>
      expect(within(row).getByText("Synthetic main hospital")).toBeInTheDocument(),
    );
    refused = true;
    await act(() =>
      client.invalidateQueries({
        queryKey: ["pharmacy", tenant, "synthetic-owner", pharmacy.id, "ownership-hospitals"],
      }),
    );
    await waitFor(() =>
      expect(within(row).getByText("Hospital name unavailable")).toBeInTheDocument(),
    );
    expect(within(row).queryByText("Synthetic main hospital")).not.toBeInTheDocument();
    expect(within(row).queryByText(hospital)).not.toBeInTheDocument();
  },
);
