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
const notFound = () =>
  Response.json({ error: "not_found", message: "pharmacy not found" }, { status: 404 });
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
    "registers an unregistered %s pharmacy with an empty body and shows the retained registration",
    async (ownership) => {
      const fetcher = vi
        .fn()
        .mockResolvedValueOnce(notFound())
        .mockResolvedValueOnce(Response.json({ ...employer, ownership_type: ownership }))
        .mockResolvedValue(Response.json({ ...employer, ownership_type: ownership }));
      vi.stubGlobal("fetch", fetcher);
      const { user } = renderWithProviders(
        <PharmacyEmployer tenantId={tenant} pharmacy={organization(ownership)} canManage />,
      );
      expect(await screen.findByText(/not yet registered as an employer/)).toBeInTheDocument();
      await user.click(screen.getByRole("button", { name: "Register as employer" }));
      expect(
        await screen.findByText(/Registered as an employer on 5 Nov 2026/),
      ).toBeInTheDocument();
      const [url, init] = fetcher.mock.calls[1] as unknown as [string, RequestInit];
      expect(url).toBe(`/api/v1/workforce/pharmacies/${id}`);
      expect(init).toMatchObject({ method: "PUT", body: "{}", cache: "no-store" });
      expect(screen.queryByRole("button", { name: "Register as employer" })).toBeNull();
    },
  );

  it("shows the refusal and keeps the pharmacy unregistered when registration fails", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValueOnce(notFound())
        .mockResolvedValueOnce(
          Response.json(
            { error: "authz_unavailable", message: "authorization is temporarily unavailable" },
            { status: 503 },
          ),
        ),
    );
    const { user } = renderWithProviders(
      <PharmacyEmployer tenantId={tenant} pharmacy={organization("independent")} canManage />,
    );
    await user.click(await screen.findByRole("button", { name: "Register as employer" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "authorization is temporarily unavailable",
    );
    expect(screen.getByRole("button", { name: "Register as employer" })).toBeEnabled();
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
