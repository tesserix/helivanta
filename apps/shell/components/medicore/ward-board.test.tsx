import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { chooseOption } from "../../test/select";
import { WardBoard } from "./ward-board";

const hospital = { id: "0199e000-0000-7000-8000-00000000a001", name: "TEST General" };
const patient = {
  id: "0199e000-0000-7000-8000-000000000001",
  hospital_id: hospital.id,
  given_name: "Asha",
  family_name: "Rao",
  mrn: "MRN-1",
};
const bed = (over: Record<string, unknown>) => ({
  id: "0199e000-0000-7000-8000-0000000000b1",
  hospital_id: hospital.id,
  ward_id: "w1",
  ward_code: "W-A",
  room_id: "r1",
  room_code: "R-1",
  code: "A-1",
  bed_class: "general",
  state: "available",
  version: 1,
  created_at: "2026-10-29T00:00:00Z",
  occupancy_id: null,
  admission_id: null,
  patient_id: null,
  occupied_since: null,
  ...over,
});
const occupied = bed({
  id: "0199e000-0000-7000-8000-0000000000b2",
  code: "A-2",
  state: "occupied",
  version: 2,
  admission_id: "0199e000-0000-7000-8000-0000000000a9",
  patient_id: patient.id,
});
const free = bed({ id: "0199e000-0000-7000-8000-0000000000b3", code: "A-3", version: 4 });
const cleaning = bed({
  id: "0199e000-0000-7000-8000-0000000000b4",
  code: "A-4",
  state: "cleaning",
  version: 3,
});

function json(status: number, body: unknown) {
  return Promise.resolve(
    new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }),
  );
}
const page = (data: unknown[]) => ({ data, page: { next_cursor: null, has_more: false } });

function server(
  permissions = ["medicore.visit.read", "medicore.admission.manage"],
  opts: { beds?: () => unknown[]; put?: () => Promise<Response>; account?: number } = {},
) {
  const calls: { url: string; init?: RequestInit }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn().mockImplementation((url: string, init?: RequestInit) => {
      calls.push({ url, init });
      if (url.includes("/iam/me/permissions"))
        return json(200, { subject: "user-admin", data: permissions });
      if (url.endsWith(`/billing/hospitals/${hospital.id}/access`))
        return json(200, { granted: true });
      if (url.includes("/billing/") && url.endsWith("/account") && opts.account)
        return json(opts.account, { error: "x", message: "x" });
      if (url.includes("/billing/") && url.endsWith("/account"))
        return json(200, {
          admission_id: occupied.admission_id,
          status: "open",
          totals: [
            { currency: "INR", charged_minor: 2500, reversed_minor: 1000, outstanding_minor: 1500 },
          ],
          entry_count: 2,
        });
      if (url.includes("/workforce/hospitals")) return json(200, page([hospital]));
      if (url.endsWith(`/medicore/hospitals/${hospital.id}/access`))
        return json(200, { granted: true });
      if (init?.method === "PUT") return opts.put?.() ?? json(200, {});
      if (url.includes("/beds?"))
        return json(200, page(opts.beds?.() ?? [occupied, free, cleaning]));
      if (url.includes("/patients/search")) return json(200, page([patient]));
      if (url.includes(`/patients/${patient.id}`)) return json(200, patient);
      return json(200, page([]));
    }),
  );
  return calls;
}
const puts = (calls: ReturnType<typeof server>) => calls.filter((c) => c.init?.method === "PUT");
const body = (c: { init?: RequestInit }) => JSON.parse(String(c.init?.body));

afterEach(() => vi.unstubAllGlobals());

describe("WardBoard", () => {
  it("shows each bed's state and its occupant by name", async () => {
    server();
    renderWithProviders(<WardBoard />);
    const room = await screen.findByRole("list", { name: "Room R-1" });
    expect(await within(room).findByText(/Asha Rao/)).toBeInTheDocument();
    expect(within(room).getByText("Occupied")).toBeInTheDocument();
    expect(within(room).getByText("Cleaning")).toBeInTheDocument();
  });

  it("admits a found patient to an available bed with its expected version", async () => {
    const calls = server();
    const { user } = renderWithProviders(<WardBoard />);
    await user.type(await screen.findByLabelText("Find a patient registered here"), "Asha");
    await user.click(screen.getByRole("button", { name: "Search" }));
    await chooseOption(user, await screen.findByRole("combobox", { name: "Patient" }), /Asha Rao/);
    const bedPicker = screen.getByRole("combobox", { name: "Bed" });
    await user.click(bedPicker);
    expect(screen.queryByRole("option", { name: /A-2/ })).toBeNull();
    expect(screen.queryByRole("option", { name: /A-4/ })).toBeNull();
    await user.click(screen.getByRole("option", { name: /A-3/ }));
    await user.click(screen.getByRole("button", { name: "Admit" }));
    await waitFor(() => expect(puts(calls)).toHaveLength(1));
    expect(puts(calls)[0].url).toMatch(/\/admissions\/[0-9a-f-]{36}$/);
    expect(body(puts(calls)[0])).toEqual({
      patient_id: patient.id,
      bed_id: free.id,
      intent: "emergency",
      expected_bed_version: 4,
    });
  });

  it("releases only after a confirmation, and asks for a note when the reason is other", async () => {
    const calls = server();
    const { user } = renderWithProviders(<WardBoard />);
    await user.click(await screen.findByRole("button", { name: "Release" }));
    await chooseOption(
      user,
      screen.getByRole("combobox", { name: "Why the patient left the bed" }),
      "Other",
    );
    const form = screen
      .getByRole("combobox", { name: "Why the patient left the bed" })
      .closest("form")!;
    await user.click(within(form).getByRole("button", { name: "Release" }));
    expect(await screen.findByText("Say what happened")).toBeInTheDocument();
    await user.type(screen.getByLabelText("What happened"), "TEST left with family");
    await user.click(within(form).getByRole("button", { name: "Release" }));
    const dialog = await screen.findByRole("dialog");
    expect(puts(calls)).toHaveLength(0);
    await user.click(within(dialog).getByRole("button", { name: "Release" }));
    await waitFor(() => expect(puts(calls)).toHaveLength(1));
    expect(puts(calls)[0].url).toContain(`/admissions/${occupied.admission_id}/release`);
    expect(body(puts(calls)[0])).toEqual({ reason: "other", note: "TEST left with family" });
  });

  it("transfers to another available bed and marks a cleaned bed ready", async () => {
    const calls = server();
    const { user } = renderWithProviders(<WardBoard />);
    await user.click(await screen.findByRole("button", { name: "Transfer" }));
    const picker = screen.getByRole("combobox", { name: "Move to bed" });
    await chooseOption(user, picker, /A-3/);
    await user.click(within(picker.closest("form")!).getByRole("button", { name: "Transfer" }));
    await waitFor(() => expect(puts(calls)).toHaveLength(1));
    expect(puts(calls)[0].url).toMatch(
      new RegExp(`/admissions/${occupied.admission_id}/transfers/[0-9a-f-]{36}$`),
    );
    expect(body(puts(calls)[0])).toEqual({ to_bed_id: free.id, expected_bed_version: 4 });

    await user.click(screen.getByRole("button", { name: "Mark ready" }));
    await waitFor(() => expect(puts(calls)).toHaveLength(2));
    expect(puts(calls)[1].url).toContain(`/beds/${cleaning.id}/transitions/`);
    expect(body(puts(calls)[1])).toEqual({ to: "available", expected_version: 3 });
  });

  it("offers no admission actions to a reader", async () => {
    server(["medicore.visit.read"]);
    renderWithProviders(<WardBoard />);
    await screen.findByRole("list", { name: "Room R-1" });
    expect(screen.queryByRole("heading", { name: "Admit a patient" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Release" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Mark ready" })).toBeNull();
  });

  it("sends the trimmed note, and nothing for a blank optional one", async () => {
    const calls = server();
    const { user } = renderWithProviders(<WardBoard />);
    await user.click(await screen.findByRole("button", { name: "Release" }));
    await user.type(screen.getByLabelText("Note (optional)"), "   ");
    const form = screen
      .getByRole("combobox", { name: "Why the patient left the bed" })
      .closest("form")!;
    await user.click(within(form).getByRole("button", { name: "Release" }));
    await user.click(
      within(await screen.findByRole("dialog")).getByRole("button", { name: "Release" }),
    );
    await waitFor(() => expect(puts(calls)).toHaveLength(1));
    expect(body(puts(calls)[0])).toEqual({ reason: "discharged" });
  });

  it("starts a new admission after a refusal, and refuses a bed taken while the form was open", async () => {
    let beds = [occupied, free, cleaning];
    let put = 0;
    const calls = server(undefined, {
      beds: () => beds,
      put: () => {
        put++;
        // First someone moved A-3 on; then someone took it.
        beds = put === 1 ? [occupied, { ...free, version: 5 }, cleaning] : [occupied, cleaning];
        return json(409, {
          error: "stale_bed",
          message: "the bed changed; reload it and try again",
        });
      },
    });
    const { user } = renderWithProviders(<WardBoard />);
    await user.type(await screen.findByLabelText("Find a patient registered here"), "Asha");
    await user.click(screen.getByRole("button", { name: "Search" }));
    await chooseOption(user, await screen.findByRole("combobox", { name: "Patient" }), /Asha Rao/);
    await chooseOption(user, screen.getByRole("combobox", { name: "Bed" }), /A-3/);
    await user.click(screen.getByRole("button", { name: "Admit" }));
    await waitFor(() => expect(puts(calls)).toHaveLength(1));
    await waitFor(() =>
      expect(calls.filter((c) => c.url.includes("/beds?")).length).toBeGreaterThan(1),
    );
    await user.click(screen.getByRole("button", { name: "Admit" }));
    await waitFor(() => expect(puts(calls)).toHaveLength(2));
    expect(puts(calls)[1].url).not.toBe(puts(calls)[0].url);
    expect(body(puts(calls)[1]).expected_bed_version).toBe(5);

    await waitFor(() => expect(screen.getByText("No bed is available now.")).toBeInTheDocument());
    await user.click(screen.getByRole("button", { name: "Admit" }));
    expect(await screen.findByText("That bed was just taken. Choose another.")).toBeInTheDocument();
    expect(puts(calls)).toHaveLength(2);
  });

  it("shows what an occupied bed's admission owes, for a billing reader", async () => {
    server(["medicore.visit.read", "billing.account.read"]);
    renderWithProviders(<WardBoard />);
    expect(await screen.findByText(/Pharmacy charges owed/)).toHaveTextContent(
      "Pharmacy charges owed INR 15.00",
    );
  });

  it("reads no account without billing access", async () => {
    const calls = server(["medicore.visit.read"]);
    renderWithProviders(<WardBoard />);
    const room = await screen.findByRole("list", { name: "Room R-1" });
    await within(room).findByText(/Asha Rao/);
    expect(screen.queryByText(/Pharmacy charges owed/)).toBeNull();
    expect(calls.some((c) => c.url.includes("/billing/"))).toBe(false);
  });

  it.each([
    [404, "No pharmacy charges posted yet"],
    [503, "Could not load the account"],
  ])("shows an account answering %i honestly, never as zero", async (status, text) => {
    server(["medicore.visit.read", "billing.account.read"], { account: status });
    renderWithProviders(<WardBoard />);
    expect(await screen.findByText(text)).toBeInTheDocument();
    expect(screen.queryByText("No pharmacy charges owed")).toBeNull();
  });
});
