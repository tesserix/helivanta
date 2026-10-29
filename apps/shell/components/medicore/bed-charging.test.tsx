import { act, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { chooseOption } from "../../test/select";
import { BedChargingSetup } from "./bed-charging";

const hospital = {
  id: "0199e000-0000-7000-8000-00000000a001",
  name: "TEST General",
  access_level: "manager" as const,
};
const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
const page = (data: unknown[]) => json(200, { data, page: { next_cursor: null, has_more: false } });
const rate = {
  id: "0199e000-0000-7000-8000-0000000000d1",
  hospital_id: hospital.id,
  bed_class: "icu",
  effective_from: "2026-01-01",
  daily_rate_minor: 600000,
  currency: "INR",
  created_by: "user-admin",
  created_at: "2026-10-30T06:00:00Z",
};

afterEach(() => vi.unstubAllGlobals());

function server(opts: { permissions?: string[]; timezone?: string | null } = {}) {
  const permissions = opts.permissions ?? ["billing.account.read", "billing.rate.manage"];
  const fetcher = vi.fn(async (url: string, init: RequestInit = {}) => {
    if (url.includes("/iam/me/permissions"))
      return json(200, { subject: "user-admin", data: permissions });
    if (url.endsWith(`/billing/hospitals/${hospital.id}/access`))
      return json(200, { granted: true });
    if (init.method === "PUT") return json(200, {});
    if (url.endsWith("/bed-charging"))
      return opts.timezone
        ? json(200, { hospital_id: hospital.id, timezone: opts.timezone })
        : json(404, { error: "not_configured", message: "bed charging is not set up" });
    if (url.includes("/bed-rates")) return page([rate]);
    return page([]);
  });
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}
const puts = (f: ReturnType<typeof server>) =>
  f.mock.calls.filter(([, init]) => init?.method === "PUT") as [string, RequestInit][];

describe("BedChargingSetup", () => {
  it("sets the hospital's timezone once, offering Asia/Kolkata", async () => {
    const fetcher = server({ timezone: null });
    const { user } = renderWithProviders(<BedChargingSetup hospital={hospital} />);
    expect(await screen.findByLabelText("Timezone")).toHaveValue("Asia/Kolkata");
    expect(screen.getByText(/it cannot be changed/)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Set timezone" }));
    await waitFor(() => expect(puts(fetcher)).toHaveLength(1));
    expect(puts(fetcher)[0][0]).toMatch(`/billing/hospitals/${hospital.id}/bed-charging`);
    expect(JSON.parse(String(puts(fetcher)[0][1].body))).toEqual({ timezone: "Asia/Kolkata" });
  });

  it("lists rates and sets one for a class from a date, refusing an empty one inline", async () => {
    const fetcher = server({ timezone: "Asia/Kolkata" });
    const { user } = renderWithProviders(<BedChargingSetup hospital={hospital} />);
    expect(await screen.findByText("Midnights are counted in Asia/Kolkata.")).toBeInTheDocument();
    expect(await screen.findByText(/ICU · INR 6,000.00 a day from 2026-01-01/)).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Set rate" }));
    expect(await screen.findByText("Choose the date the rate starts")).toBeInTheDocument();
    expect(puts(fetcher)).toHaveLength(0);

    await chooseOption(user, screen.getByLabelText("Bed class"), "Private");
    await user.clear(screen.getByLabelText("Daily rate (INR)"));
    await user.type(screen.getByLabelText("Daily rate (INR)"), "3000.50");
    await user.type(screen.getByLabelText("From"), "2026-11-01");
    await user.click(screen.getByRole("button", { name: "Set rate" }));
    await waitFor(() => expect(puts(fetcher)).toHaveLength(1));
    expect(puts(fetcher)[0][0]).toMatch(
      `/billing/hospitals/${hospital.id}/bed-rates/private/2026-11-01`,
    );
    expect(JSON.parse(String(puts(fetcher)[0][1].body))).toEqual({
      daily_rate_minor: 300050,
      currency: "INR",
    });
  });

  it("is not shown to a reader who cannot set rates", async () => {
    const fetcher = server({ permissions: ["billing.account.read"], timezone: "Asia/Kolkata" });
    renderWithProviders(<BedChargingSetup hospital={hospital} />);
    // Only once the billing grant has answered could the panel appear.
    await waitFor(() =>
      expect(fetcher.mock.calls.some(([url]) => String(url).endsWith("/access"))).toBe(true),
    );
    await act(() => new Promise((resolve) => setTimeout(resolve, 100)));
    expect(screen.queryByText(/Bed charges at/)).toBeNull();
    expect(fetcher.mock.calls.some(([url]) => String(url).includes("/bed-charging"))).toBe(false);
  });
});
