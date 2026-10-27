import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { OpdEncounters } from "./opd-encounters";

const hospital = { id: "0199e000-0000-7000-8000-00000000a001", name: "TEST General" };
const patient = {
  id: "0199e000-0000-7000-8000-000000000001",
  hospital_id: hospital.id,
  given_name: "Asha",
  family_name: "Rao",
  mrn: "MRN-1",
};
const encounter = {
  id: "0199e000-0000-7000-8000-0000000000e1",
  hospital_id: hospital.id,
  patient_id: patient.id,
  consult_mode: "teleconsultation",
  visit_type: "first",
  status: "arrived",
  version: 1,
  created_by: "user-doc",
  created_at: "2026-10-27T05:00:00Z",
  closed_at: null,
};

function json(status: number, body: unknown) {
  return Promise.resolve(
    new Response(JSON.stringify(body), {
      status,
      headers: { "Content-Type": "application/json" },
    }),
  );
}
const page = (data: unknown[]) => ({ data, page: { next_cursor: null, has_more: false } });

function server(
  opts: { access?: number; permissions?: string[]; failPut?: number; moveStatus?: number } = {},
) {
  const calls: { url: string; init?: RequestInit }[] = [];
  let failPut = opts.failPut ?? 0;
  const fetchMock = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
    calls.push({ url, init });
    if (url.includes("/iam/me/permissions"))
      return json(200, {
        data: opts.permissions ?? [
          "medicore.visit.create",
          "medicore.visit.read",
          "medicore.visit.update",
        ],
      });
    if (url.includes("/workforce/hospitals")) return json(200, page([hospital]));
    if (url.endsWith(`/medicore/hospitals/${hospital.id}/access`))
      return opts.access === 404
        ? json(404, { error: "not_found", message: "hospital not found" })
        : json(200, { hospital_id: hospital.id, granted: true });
    if (url.includes("/transitions"))
      return opts.moveStatus
        ? json(opts.moveStatus, {
            error: "conflict",
            message: "the encounter changed since it was read",
          })
        : json(200, { ...encounter, status: "in_consultation", version: 2 });
    if (url.includes(`/medicore/hospitals/${hospital.id}/encounters/`) && init?.method === "PUT") {
      if (failPut-- > 0) return json(502, { error: "bad_gateway", message: "upstream timed out" });
      return json(201, encounter);
    }
    if (url.includes(`/medicore/hospitals/${hospital.id}/encounters`))
      return json(200, page([encounter]));
    if (url.includes("/patients/search")) return json(200, page([patient]));
    if (url.includes(`/patients/${patient.id}`)) return json(200, patient);
    return json(404, {});
  });
  vi.stubGlobal("fetch", fetchMock);
  return calls;
}

afterEach(() => vi.unstubAllGlobals());

describe("OpdEncounters", () => {
  it("lists a hospital's encounters by patient name and moves one along", async () => {
    const calls = server();
    renderWithProviders(<OpdEncounters />);
    const list = await screen.findByRole("list", { name: "Encounters" });
    expect(await within(list).findByText(/Asha Rao/)).toBeInTheDocument();
    expect(within(list).getByText(/Teleconsultation · First visit/)).toBeInTheDocument();

    await userEvent.click(within(list).getByRole("button", { name: "Start consultation" }));
    await waitFor(() => expect(calls.some((c) => c.url.includes("/transitions"))).toBe(true));
    const move = calls.find((c) => c.url.includes("/transitions"))!;
    expect(JSON.parse(String(move.init?.body))).toEqual({
      to: "in_consultation",
      expected_version: 1,
    });
  });

  it("asks for a reason before cancelling", async () => {
    const calls = server();
    renderWithProviders(<OpdEncounters />);
    const list = await screen.findByRole("list", { name: "Encounters" });
    await userEvent.click(await within(list).findByRole("button", { name: "Cancel" }));
    await userEvent.click(within(list).getByRole("button", { name: "Confirm" }));
    expect(await within(list).findByText("Give a reason")).toBeInTheDocument();
    expect(calls.some((c) => c.url.includes("/transitions"))).toBe(false);

    await userEvent.type(within(list).getByLabelText(/Reason/), "Patient asked to rebook");
    await userEvent.click(within(list).getByRole("button", { name: "Confirm" }));
    await waitFor(() => expect(calls.some((c) => c.url.includes("/transitions"))).toBe(true));
    const move = calls.find((c) => c.url.includes("/transitions"))!;
    expect(JSON.parse(String(move.init?.body))).toEqual({
      to: "cancelled",
      expected_version: 1,
      reason: "Patient asked to rebook",
    });
  });

  it("opens an encounter for a patient found at this hospital, under a UUIDv7", async () => {
    const calls = server();
    renderWithProviders(<OpdEncounters />);
    await userEvent.type(await screen.findByLabelText("Find a patient registered here"), "Asha");
    await userEvent.click(screen.getByRole("button", { name: "Search" }));
    await waitFor(() => expect(calls.some((c) => c.url.includes("/patients/search"))).toBe(true));
    await userEvent.click(await screen.findByRole("combobox", { name: "Patient" }));
    await userEvent.click(await screen.findByRole("option", { name: "Asha Rao · MRN-1" }));
    await userEvent.click(screen.getByRole("button", { name: "Open encounter" }));

    await waitFor(() => expect(calls.some((c) => c.init?.method === "PUT")).toBe(true));
    const put = calls.find((c) => c.init?.method === "PUT")!;
    expect(put.url).toMatch(
      /\/medicore\/hospitals\/[^/]+\/encounters\/[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
    );
    expect(JSON.parse(String(put.init?.body))).toEqual({
      patient_id: patient.id,
      consult_mode: "in_person",
      visit_type: "first",
    });
  });

  // Review finding 1: a retry after a lost response must replay the same
  // encounter id, or the patient gets two encounters.
  it("retries an open under the same encounter id", async () => {
    const calls = server({ failPut: 1 });
    renderWithProviders(<OpdEncounters />);
    await userEvent.type(await screen.findByLabelText("Find a patient registered here"), "Asha");
    await userEvent.click(screen.getByRole("button", { name: "Search" }));
    await userEvent.click(await screen.findByRole("combobox", { name: "Patient" }));
    await userEvent.click(await screen.findByRole("option", { name: "Asha Rao · MRN-1" }));
    await userEvent.click(screen.getByRole("button", { name: "Open encounter" }));
    await waitFor(() => expect(calls.filter((c) => c.init?.method === "PUT")).toHaveLength(1));
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Open encounter" })).toBeEnabled(),
    );
    await userEvent.click(screen.getByRole("button", { name: "Open encounter" }));
    await waitFor(() => expect(calls.filter((c) => c.init?.method === "PUT")).toHaveLength(2));
    const [first, second] = calls.filter((c) => c.init?.method === "PUT");
    expect(second.url).toBe(first.url);
  });

  // Review finding 6: a refused move (a colleague moved it first) reloads the
  // list, so the row shows the current status instead of failing forever.
  it("reloads the encounters after a refused move", async () => {
    const calls = server({ moveStatus: 409 });
    renderWithProviders(<OpdEncounters />);
    const list = await screen.findByRole("list", { name: "Encounters" });
    await within(list).findByText(/Asha Rao/);
    const lists = () =>
      calls.filter((c) => /\/encounters(\?|$)/.test(c.url) && c.init?.method !== "PUT").length;
    const before = lists();
    await userEvent.click(within(list).getByRole("button", { name: "Start consultation" }));
    await waitFor(() => expect(lists()).toBeGreaterThan(before));
  });

  // Review finding 5: a patient's name is read once per page visit, not again
  // each time the rows remount (each read is an audited disclosure).
  it("does not re-read a patient when the list is filtered", async () => {
    const calls = server();
    renderWithProviders(<OpdEncounters />);
    await within(await screen.findByRole("list", { name: "Encounters" })).findByText(/Asha Rao/);
    await new Promise((resolve) => setTimeout(resolve, 6_000)); // past the default freshness
    await userEvent.click(screen.getByRole("combobox", { name: "Status" }));
    await userEvent.click(await screen.findByRole("option", { name: "Arrived" }));
    await waitFor(() => expect(calls.some((c) => c.url.includes("status=arrived"))).toBe(true));
    await within(await screen.findByRole("list", { name: "Encounters" })).findByText(/Asha Rao/);
    expect(calls.filter((c) => c.url.includes(`/patients/${patient.id}`))).toHaveLength(1);
  }, 20_000);

  it("says plainly when the account has no MediCore access at the hospital", async () => {
    server({ access: 404 });
    renderWithProviders(<OpdEncounters />);
    expect(
      await screen.findByRole("heading", { name: `No MediCore access at ${hospital.name}` }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("list", { name: "Encounters" })).toBeNull();
  });

  it("hides the open form and status actions without the permissions", async () => {
    server({ permissions: ["medicore.visit.read"] });
    renderWithProviders(<OpdEncounters />);
    const list = await screen.findByRole("list", { name: "Encounters" });
    expect(await within(list).findByText(/Asha Rao/)).toBeInTheDocument();
    expect(within(list).queryByRole("button")).toBeNull();
    expect(screen.queryByRole("heading", { name: "Open an encounter" })).toBeNull();
  });
});
