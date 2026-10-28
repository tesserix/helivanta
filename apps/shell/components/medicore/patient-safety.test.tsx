import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { PatientSafety } from "./patient-safety";

const hospital = {
  id: "0199e000-0000-7000-8000-00000000a001",
  name: "TEST General",
  access_level: "reader" as const,
};
const patientId = "0199e000-0000-7000-8000-000000000001";
const encounterId = "0199e000-0000-7000-8000-0000000000e1";
const base = `/medicore/hospitals/${hospital.id}/patients/${patientId}`;

const unknown = {
  status: "unknown",
  no_known_allergy: null,
  entries: [],
  needs_coding: false,
  recording_enabled: true,
};
const entry = {
  id: "0199e000-0000-7000-8000-0000000000a1",
  version: 1,
  status: "active",
  substance_kind: "drug_molecule",
  molecule_id: "0199e000-0000-7000-8000-0000000000m1",
  substance_label: "Amoxicillin",
  non_drug_category: null,
  reaction: "urticaria",
  reaction_note: null,
  severity: "life_threatening",
  source: "patient_reported",
  onset_date: null,
  recorded_by: "user-doc",
  recorded_at: "2026-10-27T05:00:00Z",
  needs_coding: false,
};

function json(status: number, body: unknown) {
  return Promise.resolve(
    new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }),
  );
}

function server(
  opts: {
    allergies?: unknown;
    weight?: { status: number; body: unknown };
    permissions?: string[];
  } = {},
) {
  const calls: { url: string; init?: RequestInit }[] = [];
  let allergies = opts.allergies ?? unknown;
  vi.stubGlobal(
    "fetch",
    vi.fn().mockImplementation((url: string, init?: RequestInit) => {
      calls.push({ url, init });
      if (url.includes("/iam/me/permissions"))
        return json(200, {
          data: opts.permissions ?? [
            "medicore.allergy.read",
            "medicore.allergy.record",
            "medicore.observation.read",
            "medicore.observation.record",
          ],
        });
      if (url.includes("/withdrawal")) {
        allergies = unknown;
        return json(200, { id: "x", withdrawn: true });
      }
      if (url.includes(`${base}/no-known-allergy/`)) {
        allergies = {
          ...unknown,
          status: "no_known_allergy",
          no_known_allergy: {
            id: "x",
            recorded_by: "user-nurse",
            recorded_at: "2026-10-27T06:00:00Z",
          },
        };
        return json(201, {
          id: "x",
          recorded_by: "user-nurse",
          recorded_at: "2026-10-27T06:00:00Z",
        });
      }
      if (url.endsWith(`${base}/allergies`)) return json(200, allergies);
      if (url.includes(`${base}/weights/`) && init?.method === "PUT")
        return json(201, { id: "w", weight_kg: "13.100" });
      if (url.endsWith(`${base}/weights/latest`))
        return json(
          opts.weight?.status ?? 404,
          opts.weight?.body ?? { error: "not_found", message: "weight not found" },
        );
      if (url.includes("/drugmaster/search"))
        return json(200, {
          data: [
            {
              formulation_id: "f",
              label: "Amoxicillin 500 mg capsule",
              ingredients: [{ molecule_id: entry.molecule_id, inn_name: "Amoxicillin" }],
            },
          ],
        });
      if (url.includes(`${base}/allergies/`) && init?.method === "PUT") return json(201, entry);
      return json(404, {});
    }),
  );
  return calls;
}

afterEach(() => vi.unstubAllGlobals());

const render = () =>
  renderWithProviders(
    <PatientSafety hospital={hospital} patientId={patientId} encounterId={encounterId} />,
  );

describe("PatientSafety", () => {
  it("shows an unrecorded patient as unknown, never as no allergies", async () => {
    server();
    render();
    expect(await screen.findByText("Allergies unknown: not yet asked")).toBeInTheDocument();
    expect(screen.queryByText(/No known allergies,/)).not.toBeInTheDocument();
    expect(await screen.findByText("No weight recorded.")).toBeInTheDocument();
  });

  it("records no known allergies under one id and shows who recorded it", async () => {
    const calls = server();
    render();
    await userEvent.click(await screen.findByRole("button", { name: "No known allergies" }));
    expect(calls.some((c) => c.url.includes("/no-known-allergy/"))).toBe(false);
    await userEvent.click(screen.getByRole("button", { name: "Confirm no known allergies" }));
    expect(
      await screen.findByText(/No known allergies, recorded by user-nurse/),
    ).toBeInTheDocument();
    const put = calls.find((c) => c.url.includes("/no-known-allergy/"));
    expect(JSON.parse(String(put?.init?.body))).toEqual({ encounter_id: encounterId });

    await userEvent.click(screen.getByRole("button", { name: "Withdraw" }));
    await userEvent.click(screen.getByRole("button", { name: "Confirm withdrawal" }));
    expect(await screen.findByText("Give a reason")).toBeInTheDocument();
    await userEvent.type(screen.getByLabelText("Reason for withdrawing"), "wrong patient");
    await userEvent.click(screen.getByRole("button", { name: "Confirm withdrawal" }));
    expect(await screen.findByText("Allergies unknown: not yet asked")).toBeInTheDocument();
    const post = calls.find((c) => c.url.includes("/withdrawal"));
    expect(JSON.parse(String(post?.init?.body))).toEqual({ reason: "wrong patient" });
  });

  it("lists active allergies with their severity and offers no 'no known allergies'", async () => {
    server({ allergies: { ...unknown, status: "allergies_recorded", entries: [entry] } });
    render();
    expect(await screen.findByText("Amoxicillin")).toBeInTheDocument();
    expect(screen.getByText("Life-threatening")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "No known allergies" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Correct Amoxicillin allergy" })).toBeInTheDocument();
  });

  it("warns when a drug allergy is free text", async () => {
    server({
      allergies: {
        ...unknown,
        status: "allergies_recorded",
        needs_coding: true,
        entries: [
          {
            ...entry,
            substance_kind: "drug_unlisted",
            substance_label: "Brand X",
            needs_coding: true,
          },
        ],
      },
    });
    render();
    expect(await screen.findByRole("alert")).toHaveTextContent(/Prescribing checks stay blocked/);
  });

  it("explains that recording is off until the lists are approved", async () => {
    server({ allergies: { ...unknown, recording_enabled: false } });
    render();
    expect(await screen.findByText(/opens once a clinician approves/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Record allergy" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "No known allergies" })).toBeInTheDocument();
  });

  it("records a coded drug allergy picked from the drug catalogue", async () => {
    const calls = server();
    render();
    await userEvent.click(await screen.findByRole("button", { name: "Record allergy" }));
    await userEvent.type(screen.getByLabelText("Find the drug"), "amox");
    await userEvent.click(screen.getByRole("button", { name: "Search drugs" }));
    await waitFor(() =>
      expect(calls.some((c) => c.url.includes("/drugmaster/search?q=amox"))).toBe(true),
    );
    await userEvent.click(screen.getByRole("combobox", { name: "Drug" }));
    await userEvent.click(await screen.findByRole("option", { name: "Amoxicillin" }));
    await userEvent.click(screen.getByRole("button", { name: "Save allergy" }));
    await waitFor(() =>
      expect(
        calls.some((c) => c.url.includes(`${base}/allergies/`) && c.init?.method === "PUT"),
      ).toBe(true),
    );
    const put = calls.find((c) => c.url.includes(`${base}/allergies/`) && c.init?.method === "PUT");
    expect(JSON.parse(String(put?.init?.body))).toMatchObject({
      substance_kind: "drug_molecule",
      molecule_id: entry.molecule_id,
      encounter_id: encounterId,
    });
  });

  it("refuses a malformed weight inline and records a valid one", async () => {
    const calls = server({
      weight: {
        status: 200,
        body: { id: "w0", weight_kg: "12.500", method: "measured", age_days: 3 },
      },
    });
    render();
    expect(await screen.findByText("12.500 kg (measured), 3 days ago")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Record weight" }));
    await userEvent.type(screen.getByRole("textbox", { name: "Weight" }), "13.12345");
    await userEvent.click(screen.getByRole("button", { name: "Save weight" }));
    expect(await screen.findByText("A number, at most three decimals")).toBeInTheDocument();
    await userEvent.clear(screen.getByRole("textbox", { name: "Weight" }));
    await userEvent.type(screen.getByRole("textbox", { name: "Weight" }), "13.1");
    await userEvent.click(screen.getByRole("button", { name: "Save weight" }));
    await waitFor(() => expect(calls.some((c) => c.init?.method === "PUT")).toBe(true));
    const put = calls.find((c) => c.init?.method === "PUT");
    const body = JSON.parse(String(put?.init?.body));
    expect(body).toMatchObject({ value: "13.1", unit: "kg", method: "measured" });
    expect(Math.abs(Date.parse(body.measured_at) - Date.now())).toBeLessThan(5 * 60_000);
  });

  it("hides what the caller may not read", async () => {
    server({ permissions: ["medicore.observation.read"] });
    render();
    expect(await screen.findByText("No weight recorded.")).toBeInTheDocument();
    expect(screen.queryByText(/Allergies/)).not.toBeInTheDocument();
  });
});

describe("PatientSafety edge cases", () => {
  it("does not call a missing grant an empty weight history", async () => {
    server({ weight: { status: 404, body: { error: "not_found", message: "patient not found" } } });
    render();
    expect(await screen.findByText("Could not load weight.")).toBeInTheDocument();
    expect(screen.queryByText("No weight recorded.")).not.toBeInTheDocument();
  });

  it("sends a past measurement time as entered and refuses a future one", async () => {
    const calls = server();
    render();
    await userEvent.click(await screen.findByRole("button", { name: "Record weight" }));
    const at = screen.getByLabelText("Measured at");
    await userEvent.clear(at);
    await userEvent.type(at, "2099-01-01T10:00");
    await userEvent.type(screen.getByRole("textbox", { name: "Weight" }), "13");
    await userEvent.click(screen.getByRole("button", { name: "Save weight" }));
    expect(await screen.findByText("Cannot be in the future")).toBeInTheDocument();
    await userEvent.clear(at);
    await userEvent.type(at, "2026-10-20T09:30");
    await userEvent.click(screen.getByRole("button", { name: "Save weight" }));
    await waitFor(() => expect(calls.some((c) => c.init?.method === "PUT")).toBe(true));
    const body = JSON.parse(String(calls.find((c) => c.init?.method === "PUT")?.init?.body));
    expect(body.measured_at).toBe(
      new Date("2026-10-20T09:30").toISOString().replace(/\.\d{3}Z$/, "Z"),
    );
  });

  it("retries the same weight under the same id", async () => {
    let fail = 1;
    const calls = server();
    type Fetch = (url: string, init?: RequestInit) => Promise<Response>;
    const fetchMock = vi.mocked(globalThis.fetch as unknown as Fetch);
    const inner = fetchMock.getMockImplementation() as Fetch;
    fetchMock.mockImplementation((url: string, init?: RequestInit) =>
      init?.method === "PUT" && fail-- > 0
        ? json(502, { error: "bad_gateway", message: "timed out" })
        : inner(url, init),
    );
    render();
    await userEvent.click(await screen.findByRole("button", { name: "Record weight" }));
    await userEvent.type(screen.getByRole("textbox", { name: "Weight" }), "13");
    await userEvent.click(screen.getByRole("button", { name: "Save weight" }));
    const puts = () =>
      fetchMock.mock.calls.filter(
        ([, init]) => (init as RequestInit | undefined)?.method === "PUT",
      );
    await waitFor(() => expect(puts()).toHaveLength(1));
    await userEvent.click(screen.getByRole("button", { name: "Save weight" }));
    await waitFor(() => expect(puts()).toHaveLength(2));
    const [first, second] = puts();
    expect(second[0]).toBe(first[0]);
    expect(calls.filter((c) => c.init?.method === "PUT")).toHaveLength(1);
  });
});
