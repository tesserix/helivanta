import { screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { chooseOption } from "../../test/select";
import { BillingDesk } from "./billing-desk";

const hospital = {
  id: "0199e000-0000-7000-8000-00000000a001",
  name: "TEST General",
  access_level: "manager" as const,
};
const gone = {
  admission_id: "0199e000-0000-7000-8000-0000000000a9",
  hospital_id: hospital.id,
  patient_id: "0199e000-0000-7000-8000-000000000001",
  status: "closed",
  discharge_ordered: false,
  opened_at: "2026-10-29T00:00:00Z",
  closed_at: "2026-10-30T08:00:00Z",
  created_at: "2026-10-29T00:00:00Z",
  balance_minor: 0,
  deposit_held_minor: 250000,
  refundable_minor: 250000,
  has_final_bill: true,
};
const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
const page = (data: unknown[]) => json(200, { data, page: { next_cursor: null, has_more: false } });

afterEach(() => vi.unstubAllGlobals());

function server(opts: { granted?: boolean } = {}) {
  const fetcher = vi.fn(async (url: string) => {
    if (url.includes("/iam/me/permissions"))
      return json(200, { subject: "user-desk", data: ["billing.account.read"] });
    if (url.endsWith(`/billing/hospitals/${hospital.id}/access`))
      return opts.granted === false
        ? json(404, { error: "not_found", message: "hospital not found" })
        : json(200, { granted: true });
    if (url.includes(`/billing/hospitals/${hospital.id}/accounts`))
      return page(url.includes("view=to_refund") ? [gone] : []);
    if (url.includes("/patients/"))
      return json(200, { given_name: "Asha", family_name: "Rao", mrn: "MRN-1" });
    if (url.endsWith("/account"))
      return json(200, { status: "closed", discharge_ordered: false, totals: [], sources: [] });
    return page([]);
  });
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}
const urls = (f: ReturnType<typeof server>) => f.mock.calls.map(([url]) => String(url));

describe("BillingDesk", () => {
  it("lists accounts to collect first, and switches to those to refund", async () => {
    const fetcher = server();
    const { user } = renderWithProviders(<BillingDesk hospital={hospital} />);
    expect(await screen.findByText("No accounts here.")).toBeInTheDocument();
    expect(urls(fetcher).some((u) => u.endsWith("/accounts?view=to_collect"))).toBe(true);
    await chooseOption(user, screen.getByLabelText("Show"), "To refund");
    expect(await screen.findByText(/to refund INR 2,500.00/)).toBeInTheDocument();
    expect(screen.getByText("Released")).toBeInTheDocument();
    expect(await screen.findByText(/Asha Rao/)).toBeInTheDocument();
  });

  it("opens a released patient's account, which the ward board no longer shows", async () => {
    const fetcher = server();
    const { user } = renderWithProviders(<BillingDesk hospital={hospital} />);
    await chooseOption(user, await screen.findByLabelText("Show"), "To refund");
    await user.click(await screen.findByRole("button", { name: "Open" }));
    await waitFor(() =>
      expect(urls(fetcher).some((u) => u.endsWith(`/admissions/${gone.admission_id}/bills`))).toBe(
        true,
      ),
    );
  });

  it("names each row's Open button by its patient", async () => {
    server();
    const { user } = renderWithProviders(<BillingDesk hospital={hospital} />);
    await chooseOption(user, await screen.findByLabelText("Show"), "To refund");
    await screen.findByText(/Asha Rao/);
    expect(screen.getByRole("button", { name: "Open" })).toHaveAccessibleDescription(/Asha Rao/);
  });

  it("says so without a billing grant, and lists nothing", async () => {
    const fetcher = server({ granted: false });
    renderWithProviders(<BillingDesk hospital={hospital} />);
    expect(await screen.findByText(`No billing access at ${hospital.name}`)).toBeInTheDocument();
    expect(urls(fetcher).some((u) => u.includes("/accounts"))).toBe(false);
  });
});
