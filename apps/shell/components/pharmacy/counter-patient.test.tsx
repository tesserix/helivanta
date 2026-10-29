import { screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { CounterPatientLookup } from "./counter-patient";

const scope = {
  tenantId: "11111111-1111-1111-1111-111111111111",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};
const hospital = "22222222-2222-4222-8222-222222222222";
const page = (data: unknown[]) =>
  JSON.stringify({ data, page: { has_more: false, next_cursor: null } });
const link = (status: string, hospital_id = hospital) => ({
  id: "a1",
  pharmacy_id: scope.pharmacyId,
  hospital_id,
  relationship: "owner",
  verification_status: status,
  verification_request_id: "r1",
  verified_at: null,
  created_at: "",
});

afterEach(() => vi.unstubAllGlobals());

function stub(links: unknown[], lookup: () => Response) {
  const fetcher = vi.fn(async (url: string) =>
    url.includes("/patients/pharmacy-lookup") ? lookup() : new Response(page(links)),
  );
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}
const lookups = (fetcher: ReturnType<typeof stub>) =>
  fetcher.mock.calls.filter(([url]) => String(url).includes("/patients/pharmacy-lookup"));

describe("CounterPatientLookup", () => {
  it("finds a patient by MRN at the verified hospital and hands over only a confirmed one", async () => {
    const fetcher = stub(
      [link("verified"), link("requested", "33333333-3333-4333-8333-333333333333")],
      () =>
        new Response(
          JSON.stringify({
            patient_id: "p1",
            name: "TEST Asha Rao",
            age_years: 41,
            age_estimated: true,
            sex: "female",
          }),
        ),
    );
    const onChange = vi.fn();
    const { user } = renderWithProviders(
      <CounterPatientLookup scope={scope} value={null} onChange={onChange} />,
    );
    await user.type(await screen.findByLabelText("MRN"), " mrn-0042{Enter}");
    await waitFor(() => expect(lookups(fetcher)).toHaveLength(1));
    const url = new URL(String(lookups(fetcher)[0][0]), "http://x");
    expect(url.searchParams.get("hospital_id")).toBe(hospital);
    expect(url.searchParams.get("pharmacy_id")).toBe(scope.pharmacyId);
    expect(url.searchParams.get("mrn")).toBe("mrn-0042");
    expect(await screen.findByText("TEST Asha Rao")).toBeInTheDocument();
    expect(screen.getByText("About 41 years · female")).toBeInTheDocument();
    await user.hover(screen.getByRole("button", { name: "This is the patient" }));
    expect(onChange).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "This is the patient" }));
    expect(onChange).toHaveBeenCalledWith({
      patient_id: "p1",
      hospital_id: hospital,
      name: "TEST Asha Rao",
    });
  });

  it("says not found without saying which check failed", async () => {
    stub(
      [link("verified")],
      () =>
        new Response(JSON.stringify({ error: "not_found", message: "patient not found" }), {
          status: 404,
        }),
    );
    const { user } = renderWithProviders(
      <CounterPatientLookup scope={scope} value={null} onChange={vi.fn()} />,
    );
    await user.type(await screen.findByLabelText("MRN"), "MRN-9");
    await user.click(screen.getByRole("button", { name: "Find patient" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "No patient found. Check the MRN; if it is right, ask the hospital for patient lookup access.",
    );
    expect(screen.queryByRole("button", { name: "This is the patient" })).toBeNull();
  });

  it("offers nothing without a verified hospital", async () => {
    const fetcher = stub([link("requested"), link("revoked")], () => new Response("{}"));
    renderWithProviders(<CounterPatientLookup scope={scope} value={null} onChange={vi.fn()} />);
    expect(
      await screen.findByText(/No hospital has verified this pharmacy yet/),
    ).toBeInTheDocument();
    expect(screen.queryByLabelText("MRN")).toBeNull();
    expect(lookups(fetcher)).toHaveLength(0);
  });

  it("asks for an MRN before looking up", async () => {
    const fetcher = stub([link("verified")], () => new Response("{}"));
    const { user } = renderWithProviders(
      <CounterPatientLookup scope={scope} value={null} onChange={vi.fn()} />,
    );
    await user.click(await screen.findByRole("button", { name: "Find patient" }));
    expect(await screen.findByText("Enter the MRN")).toBeInTheDocument();
    expect(lookups(fetcher)).toHaveLength(0);
  });

  it("says when it could not check the hospitals, rather than that none verified", async () => {
    stub([], () => new Response("{}"));
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(JSON.stringify({ error: "unavailable", message: "x" }), { status: 503 }),
      ),
    );
    renderWithProviders(<CounterPatientLookup scope={scope} value={null} onChange={vi.fn()} />);
    expect(
      await screen.findByText("Could not check which hospitals have verified this pharmacy."),
    ).toBeInTheDocument();
    expect(screen.queryByText(/No hospital has verified this pharmacy yet/)).toBeNull();
  });

  it("offers a verified hospital from a later page", async () => {
    const later = "44444444-4444-4444-8444-444444444444";
    const fetcher = vi.fn(async (url: string) =>
      url.includes("cursor=")
        ? new Response(page([link("verified", later)]))
        : new Response(
            JSON.stringify({
              data: [link("requested")],
              page: { has_more: true, next_cursor: "c2" },
            }),
          ),
    );
    vi.stubGlobal("fetch", fetcher);
    const { user } = renderWithProviders(
      <CounterPatientLookup scope={scope} value={null} onChange={vi.fn()} />,
    );
    await user.type(await screen.findByLabelText("MRN"), "MRN-1");
    expect(screen.queryByText(/No hospital has verified/)).toBeNull();
    await user.click(screen.getByRole("button", { name: "Find patient" }));
    await waitFor(() =>
      expect(fetcher.mock.calls.some(([url]) => String(url).includes(`hospital_id=${later}`))).toBe(
        true,
      ),
    );
  });

  it("withdraws a result once the MRN is edited, and sends one lookup at a time", async () => {
    let answer: (r: Response) => void = () => {};
    const fetcher = stub([link("verified")], () => new Response("{}"));
    fetcher.mockImplementation(async (url: string) =>
      url.includes("/patients/pharmacy-lookup")
        ? new Promise<Response>((resolve) => (answer = resolve))
        : new Response(page([link("verified")])),
    );
    const { user } = renderWithProviders(
      <CounterPatientLookup scope={scope} value={null} onChange={vi.fn()} />,
    );
    await user.type(await screen.findByLabelText("MRN"), "MRN-41{Enter}{Enter}");
    await waitFor(() => expect(lookups(fetcher)).toHaveLength(1));
    answer(
      new Response(
        JSON.stringify({
          patient_id: "p1",
          name: "TEST Asha Rao",
          age_years: 41,
          age_estimated: false,
          sex: "",
        }),
      ),
    );
    expect(await screen.findByText("TEST Asha Rao")).toBeInTheDocument();
    expect(lookups(fetcher)).toHaveLength(1);
    await user.type(screen.getByLabelText("MRN"), "2");
    expect(screen.queryByText("TEST Asha Rao")).toBeNull();
    expect(screen.queryByRole("button", { name: "This is the patient" })).toBeNull();
  });
});
