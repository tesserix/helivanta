import { screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import type { Bed } from "@helivanta/api";
import { chooseOption } from "../../test/select";
import { DischargeOrder } from "./discharge-order";

const hospital = {
  id: "0199e000-0000-7000-8000-00000000a001",
  name: "TEST General",
  access_level: "manager" as const,
};
const admission = "0199e000-0000-7000-8000-0000000000a9";
const order = "0199e000-0000-7000-8000-0000000000d1";
const bed = (over: Partial<Bed> = {}): Bed => ({
  id: "0199e000-0000-7000-8000-0000000000b2",
  hospital_id: hospital.id,
  ward_id: "w1",
  ward_code: "W-A",
  room_id: "r1",
  room_code: "R-1",
  code: "A-2",
  bed_class: "general",
  state: "occupied",
  version: 2,
  created_at: "2026-10-29T00:00:00Z",
  occupancy_id: "0199e000-0000-7000-8000-0000000000c1",
  admission_id: admission,
  patient_id: "0199e000-0000-7000-8000-000000000001",
  occupied_since: "2026-10-29T00:00:00Z",
  discharge_order_id: null,
  discharge_disposition: null,
  discharge_ordered_at: null,
  ...over,
});
const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

afterEach(() => vi.unstubAllGlobals());

function server(permissions: string[]) {
  const fetcher = vi.fn(async (url: string, init: RequestInit = {}) => {
    if (url.includes("/iam/me/permissions"))
      return json(200, { subject: "user-doc", data: permissions });
    if (init.method === "PUT") return json(201, {});
    return json(404, { error: "not_found", message: "not found" });
  });
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}
const puts = (f: ReturnType<typeof server>) =>
  f.mock.calls.filter(([, init]) => init?.method === "PUT") as [string, RequestInit][];

describe("DischargeOrder", () => {
  it("lets a doctor order discharge with a disposition and note", async () => {
    const fetcher = server(["medicore.visit.read", "medicore.discharge.order"]);
    const { user } = renderWithProviders(<DischargeOrder hospital={hospital} bed={bed()} />);
    await user.click(await screen.findByRole("button", { name: "Order discharge" }));
    await chooseOption(user, screen.getByLabelText("Discharge to"), "Transfer out");
    await user.type(screen.getByLabelText("Note (optional)"), "  To district hospital  ");
    await user.click(screen.getByRole("button", { name: "Order discharge" }));
    await waitFor(() => expect(puts(fetcher)).toHaveLength(1));
    const [url, init] = puts(fetcher)[0];
    expect(url).toMatch(
      new RegExp(
        `/medicore/hospitals/${hospital.id}/admissions/${admission}/discharge-orders/[0-9a-f-]{36}$`,
      ),
    );
    expect(JSON.parse(String(init.body))).toEqual({
      disposition: "transfer_out",
      note: "To district hospital",
    });
  });

  it("sends no note when none is given, home by default", async () => {
    const fetcher = server(["medicore.visit.read", "medicore.discharge.order"]);
    const { user } = renderWithProviders(<DischargeOrder hospital={hospital} bed={bed()} />);
    await user.click(await screen.findByRole("button", { name: "Order discharge" }));
    await user.click(screen.getByRole("button", { name: "Order discharge" }));
    await waitFor(() => expect(puts(fetcher)).toHaveLength(1));
    expect(JSON.parse(String(puts(fetcher)[0][1].body))).toEqual({ disposition: "home" });
  });

  it("shows the order to the ward and withdraws it for a doctor, with a reason", async () => {
    const fetcher = server(["medicore.visit.read", "medicore.discharge.order"]);
    const ordered = bed({
      discharge_order_id: order,
      discharge_disposition: "home",
      discharge_ordered_at: "2026-10-30T04:00:00Z",
    });
    const { user } = renderWithProviders(<DischargeOrder hospital={hospital} bed={ordered} />);
    expect(await screen.findByText(/Discharge ordered · Home/)).toBeInTheDocument();
    await user.click(await screen.findByRole("button", { name: "Withdraw discharge" }));
    await user.click(screen.getByRole("button", { name: "Withdraw discharge" }));
    expect(await screen.findByText("Give a reason")).toBeInTheDocument();
    expect(puts(fetcher)).toHaveLength(0);
    await user.type(screen.getByLabelText("Why discharge is withdrawn"), "Fever overnight");
    await user.click(screen.getByRole("button", { name: "Withdraw discharge" }));
    await waitFor(() => expect(puts(fetcher)).toHaveLength(1));
    expect(puts(fetcher)[0][0]).toMatch(`/discharge-orders/${order}/withdrawals/`);
    expect(JSON.parse(String(puts(fetcher)[0][1].body))).toEqual({ reason: "Fever overnight" });
  });

  it("offers no discharge action to the ward, only the order", async () => {
    server(["medicore.visit.read", "medicore.admission.manage"]);
    renderWithProviders(
      <DischargeOrder
        hospital={hospital}
        bed={bed({ discharge_order_id: order, discharge_disposition: "home" })}
      />,
    );
    expect(await screen.findByText(/Discharge ordered · Home/)).toBeInTheDocument();
    await new Promise((r) => setTimeout(r, 50));
    expect(screen.queryByRole("button", { name: /discharge/i })).not.toBeInTheDocument();
  });
});
