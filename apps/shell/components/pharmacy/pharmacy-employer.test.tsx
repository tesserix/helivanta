import { screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import type { PharmacyOrganization } from "@helivanta/api";
import { PharmacyEmployer } from "./pharmacy-employer";

const tenant = "11111111-1111-4111-8111-111111111111";
const id = "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa";
const employer = {
  pharmacy_id: id,
  name: "Synthetic Chain Pharmacy",
  ownership_type: "chain",
  created_at: "2026-11-05T12:00:00Z",
};
function organization(ownership: PharmacyOrganization["ownership_type"]): PharmacyOrganization {
  return {
    id,
    code: "SYN",
    name: "Synthetic Chain Pharmacy",
    ownership_type: ownership,
    onboarding_status: "draft",
    created_at: "2026-11-01T00:00:00Z",
  };
}
const subject = "user synthetic/registrant";
const grant = { pharmacy_id: id, subject, level: "manager" };
const notFound = () =>
  Response.json({ error: "not_found", message: "pharmacy not found" }, { status: 404 });
// Routes by request, so the registration and the self-grant can each be answered.
function workforce({
  registered = false,
  register = () => Response.json(employer),
  access = () => Response.json(grant),
}: {
  registered?: boolean;
  register?: () => Response;
  access?: () => Response;
} = {}) {
  let stored = registered;
  return vi.fn(async (url: string, init?: RequestInit) => {
    if (url.endsWith("/iam/me/permissions"))
      return Response.json({ tenant_id: tenant, subject, data: [] });
    if (url.includes("/access/")) return access();
    if (init?.method === "PUT") {
      const response = register();
      if (response.ok) stored = true;
      return response;
    }
    return stored ? Response.json(employer) : notFound();
  });
}
afterEach(() => vi.unstubAllGlobals());

describe("PharmacyEmployer", () => {
  it("says a hospital-owned pharmacy's staff belong to the owning hospital, without asking Workforce", () => {
    const fetcher = vi.fn();
    vi.stubGlobal("fetch", fetcher);
    renderWithProviders(
      <PharmacyEmployer tenantId={tenant} pharmacy={organization("hospital_owned")} canManage />,
    );
    expect(screen.getByText(/employed by the hospital that owns it/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Register as employer" })).toBeNull();
    expect(fetcher).not.toHaveBeenCalled();
  });

  it.each(["independent", "chain"] as const)(
    "registers an unregistered %s pharmacy and makes the registrant its Staff manager",
    async (ownership) => {
      const fetcher = workforce({
        register: () => Response.json({ ...employer, ownership_type: ownership }),
      });
      vi.stubGlobal("fetch", fetcher);
      const { user } = renderWithProviders(
        <PharmacyEmployer tenantId={tenant} pharmacy={organization(ownership)} canManage />,
      );
      expect(await screen.findByText(/not yet registered as an employer/)).toBeInTheDocument();
      await user.click(screen.getByRole("button", { name: "Register as employer" }));
      expect(
        await screen.findByText(/Registered as an employer on 5 Nov 2026/),
      ).toBeInTheDocument();
      const writes = fetcher.mock.calls.filter(([, init]) => init?.method === "PUT");
      expect(writes.map(([url]) => url)).toEqual([
        `/api/v1/workforce/pharmacies/${id}`,
        `/api/v1/workforce/pharmacies/${id}/access/${encodeURIComponent(subject)}`,
      ]);
      expect(writes[0]?.[1]).toMatchObject({ body: "{}", cache: "no-store" });
      expect(writes[1]?.[1]).toMatchObject({ body: '{"level":"manager"}', cache: "no-store" });
      expect(screen.queryByRole("button", { name: "Register as employer" })).toBeNull();
    },
  );

  it("shows the refusal, grants nothing and keeps the pharmacy unregistered when registration fails", async () => {
    const fetcher = workforce({
      register: () =>
        Response.json(
          { error: "authz_unavailable", message: "authorization is temporarily unavailable" },
          { status: 503 },
        ),
    });
    vi.stubGlobal("fetch", fetcher);
    const { user } = renderWithProviders(
      <PharmacyEmployer tenantId={tenant} pharmacy={organization("independent")} canManage />,
    );
    await user.click(await screen.findByRole("button", { name: "Register as employer" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "authorization is temporarily unavailable",
    );
    expect(screen.getByRole("button", { name: "Register as employer" })).toBeEnabled();
    expect(fetcher.mock.calls.some(([url]) => url.includes("/access/"))).toBe(false);
  });

  it("keeps a retry for the manager grant when it fails after registration", async () => {
    let granted = false;
    const fetcher = workforce({
      access: () =>
        granted
          ? Response.json(grant)
          : Response.json(
              { error: "authz_unavailable", message: "authorization is temporarily unavailable" },
              { status: 503 },
            ),
    });
    vi.stubGlobal("fetch", fetcher);
    const { user } = renderWithProviders(
      <PharmacyEmployer tenantId={tenant} pharmacy={organization("chain")} canManage />,
    );
    await user.click(await screen.findByRole("button", { name: "Register as employer" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "authorization is temporarily unavailable",
    );
    await waitFor(() =>
      expect(screen.getByText(/Registered as an employer on 5 Nov 2026/)).toBeInTheDocument(),
    );
    granted = true;
    await user.click(screen.getByRole("button", { name: "Retry manager access" }));
    await waitFor(() => expect(screen.queryByRole("alert")).toBeNull());
    expect(screen.queryByRole("button", { name: "Retry manager access" })).toBeNull();
    expect(fetcher.mock.calls.filter(([url]) => url.includes("/access/"))).toHaveLength(2);
  });

  it("refuses a grant answered for another account", async () => {
    vi.stubGlobal(
      "fetch",
      workforce({ access: () => Response.json({ ...grant, subject: "user-other" }) }),
    );
    const { user } = renderWithProviders(
      <PharmacyEmployer tenantId={tenant} pharmacy={organization("chain")} canManage />,
    );
    await user.click(await screen.findByRole("button", { name: "Register as employer" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "The Staff access grant could not be verified.",
    );
  });

  it("does not report an unreadable registration as unregistered", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          Response.json({ error: "internal", message: "Something went wrong." }, { status: 500 }),
        ),
    );
    renderWithProviders(
      <PharmacyEmployer tenantId={tenant} pharmacy={organization("chain")} canManage />,
    );
    expect(await screen.findByText("Could not load employer registration.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Register as employer" })).toBeNull();
  });

  it("offers no registration and makes no request without hospital management access", async () => {
    const fetcher = vi.fn();
    vi.stubGlobal("fetch", fetcher);
    renderWithProviders(
      <PharmacyEmployer tenantId={tenant} pharmacy={organization("chain")} canManage={false} />,
    );
    expect(
      screen.getByText(/managed by people with hospital management access/),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Register as employer" })).toBeNull();
    await waitFor(() => expect(fetcher).not.toHaveBeenCalled());
  });
});
