import { screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { InterimBills } from "./interim-bills";

const hospital = {
  id: "0199e000-0000-7000-8000-00000000a001",
  name: "TEST General",
  access_level: "manager" as const,
};
const admission = "0199e000-0000-7000-8000-00000000b001";
const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
const page = (data: unknown[]) => json(200, { data, page: { next_cursor: null, has_more: false } });
const bill = (number: number) => ({
  id: `0199e000-0000-7000-8000-0000000000${number}0`,
  admission_id: admission,
  hospital_id: hospital.id,
  patient_id: "0199e000-0000-7000-8000-00000000c001",
  number,
  issued_by: "user-clerk",
  issued_at: "2026-10-30T06:00:00Z",
  unpriced_bed_days: 2,
  line_count: 2,
  new_count: 1,
  totals: [
    { currency: "INR", charged_minor: 250000, reversed_minor: 100000, outstanding_minor: 150000 },
  ],
  sources: [{ source_module: "pharmacy", currency: "INR", outstanding_minor: 150000 }],
});
const lines = [
  {
    id: "0199e000-0000-7000-8000-0000000000e1",
    kind: "reversal",
    source_module: "pharmacy",
    source_document: "TS/2627/C1",
    amount_minor: 100000,
    currency: "INR",
    occurred_at: "2026-10-30T05:00:00Z",
    recorded_at: "2026-10-30T05:00:00Z",
    new: true,
  },
  {
    id: "0199e000-0000-7000-8000-0000000000e2",
    kind: "charge",
    source_module: "pharmacy",
    source_document: "TS/2627/7",
    amount_minor: 250000,
    currency: "INR",
    occurred_at: "2026-10-29T05:00:00Z",
    recorded_at: "2026-10-29T05:00:00Z",
    new: false,
  },
];

afterEach(() => vi.unstubAllGlobals());

function server(opts: { permissions?: string[]; issue?: Response } = {}) {
  const permissions = opts.permissions ?? ["billing.account.read", "billing.bill.issue"];
  const issued = [bill(1)];
  const fetcher = vi.fn(async (url: string, init: RequestInit = {}) => {
    if (url.includes("/iam/me/permissions"))
      return json(200, { subject: "user-clerk", data: permissions });
    if (init.method === "POST") {
      if (opts.issue) return opts.issue;
      issued.unshift(bill(2));
      return json(201, bill(2));
    }
    if (url.includes("/interim-bills/") && url.includes("/lines")) return page(lines);
    if (url.match(/\/interim-bills\/\d+/)) return json(200, bill(Number(url.split("/").pop())));
    if (url.endsWith("/interim-bills")) return page(issued);
    return page([]);
  });
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}
const posts = (f: ReturnType<typeof server>) =>
  f.mock.calls.filter(([, init]) => init?.method === "POST") as [string, RequestInit][];

describe("InterimBills", () => {
  it("shows a bill as issued: not a tax invoice, unpriced days, new entries", async () => {
    server({ permissions: ["billing.account.read"] });
    const { user } = renderWithProviders(
      <InterimBills hospital={hospital} admissionId={admission} />,
    );
    await user.click(await screen.findByRole("button", { name: "Interim bill 1" }));
    const shown = await screen.findByRole("region", { name: "Interim bill 1" });
    expect(shown).toHaveTextContent("Interim bill 1 — not a tax invoice");
    expect(shown).toHaveTextContent("Owed INR 1,500.00");
    expect(shown).toHaveTextContent("2 bed days not priced, not included");
    expect(await screen.findByText(/TS\/2627\/C1 · reversed/)).toHaveTextContent("New");
    expect(screen.getByText(/TS\/2627\/7/)).not.toHaveTextContent("New");
    expect(screen.queryByRole("button", { name: "Issue interim bill" })).not.toBeInTheDocument();
  });

  it("issues the next bill and opens it", async () => {
    const fetcher = server();
    const { user } = renderWithProviders(
      <InterimBills hospital={hospital} admissionId={admission} />,
    );
    await user.click(await screen.findByRole("button", { name: "Issue interim bill" }));
    await waitFor(() => expect(posts(fetcher)).toHaveLength(1));
    expect(posts(fetcher)[0][0]).toMatch(
      `/billing/hospitals/${hospital.id}/admissions/${admission}/interim-bills`,
    );
    expect(await screen.findByRole("region", { name: "Interim bill 2" })).toBeInTheDocument();
  });

  it("says why nothing was issued", async () => {
    server({
      issue: json(409, {
        error: "nothing_new",
        message: "nothing has been charged or reversed since the last bill",
      }),
    });
    const { user } = renderWithProviders(
      <InterimBills hospital={hospital} admissionId={admission} />,
    );
    await user.click(await screen.findByRole("button", { name: "Issue interim bill" }));
    expect(
      await screen.findByText("nothing has been charged or reversed since the last bill"),
    ).toBeInTheDocument();
  });
});
