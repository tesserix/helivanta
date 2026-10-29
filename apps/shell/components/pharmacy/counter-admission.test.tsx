import { screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { chooseOption } from "../../test/select";
import { CounterAdmissionLookup } from "./counter-admission";

const scope = {
  tenantId: "11111111-1111-1111-1111-111111111111",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};
const hospital = "22222222-2222-4222-8222-222222222222";
const page = (data: unknown[]) =>
  JSON.stringify({ data, page: { has_more: false, next_cursor: null } });
const link = {
  id: "a1",
  pharmacy_id: scope.pharmacyId,
  hospital_id: hospital,
  relationship: "owner",
  verification_status: "verified",
  verification_request_id: "r1",
  verified_at: "",
  created_at: "",
};
const stay = {
  occupancy_id: "0199e000-0000-7000-8000-0000000000c1",
  admission_id: "0199e000-0000-7000-8000-0000000000c2",
  patient_id: "0199e000-0000-7000-8000-0000000000c3",
  hospital_id: hospital,
  ward_code: "W-A",
  room_code: "R-1",
  bed_code: "A-1",
  started_at: "",
};

afterEach(() => vi.unstubAllGlobals());

function stub(
  occupant: () => Response,
  lookup: () => Response = () =>
    new Response(
      JSON.stringify({
        patient_id: stay.patient_id,
        name: "TEST Asha Rao",
        age_years: 41,
        age_estimated: false,
        sex: "female",
      }),
    ),
) {
  const fetcher = vi.fn(async (url: string) => {
    if (url.includes("/admission-occupant")) return occupant();
    if (url.includes("/patients/pharmacy-lookup")) return lookup();
    return new Response(page([link]));
  });
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}

describe("CounterAdmissionLookup", () => {
  it("resolves a bed, confirms the patient by id, and hands over the stay", async () => {
    const fetcher = stub(() => new Response(JSON.stringify(stay)));
    const onChange = vi.fn();
    const { user } = renderWithProviders(
      <CounterAdmissionLookup scope={scope} value={null} onChange={onChange} />,
    );
    await user.type(await screen.findByLabelText("Code"), "a-1{Enter}");
    expect(await screen.findByText("TEST Asha Rao")).toBeInTheDocument();
    expect(screen.getByText(/41 years · female · Ward W-A, room R-1, bed A-1/)).toBeInTheDocument();
    const urls = fetcher.mock.calls.map(([u]) => new URL(String(u), "http://x"));
    const occupant = urls.find((u) => u.pathname.endsWith("/admission-occupant"))!;
    expect(occupant.searchParams.get("bed_code")).toBe("A-1");
    expect(occupant.searchParams.get("hospital_id")).toBe(hospital);
    const lookup = urls.find((u) => u.pathname.endsWith("/patients/pharmacy-lookup"))!;
    expect(lookup.searchParams.get("patient_id")).toBe(stay.patient_id);
    expect(lookup.searchParams.get("mrn")).toBeNull();
    await user.hover(screen.getByRole("button", { name: "Bill this admission" }));
    expect(onChange).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "Bill this admission" }));
    expect(onChange).toHaveBeenCalledWith({
      occupancy_id: stay.occupancy_id,
      admission_id: stay.admission_id,
      hospital_id: hospital,
      bed_code: "A-1",
      name: "TEST Asha Rao",
    });
  });

  it.each([
    [
      { error: "ambiguous", message: "x" },
      409,
      "More than one bed in that room is occupied. Give the bed.",
    ],
    [{ error: "no_occupant", message: "x" }, 404, "No one is in that bed or room now."],
  ])("explains a refused bed or room", async (body, status, text) => {
    stub(() => new Response(JSON.stringify(body), { status }));
    const { user } = renderWithProviders(
      <CounterAdmissionLookup scope={scope} value={null} onChange={vi.fn()} />,
    );
    await chooseOption(user, await screen.findByRole("combobox", { name: "By" }), "Room");
    await user.type(screen.getByLabelText("Code"), "R-1");
    await user.click(screen.getByRole("button", { name: "Find admission" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(text);
    expect(screen.queryByRole("button", { name: "Bill this admission" })).toBeNull();
  });

  it("says to ask for lookup access when the patient cannot be confirmed", async () => {
    stub(
      () => new Response(JSON.stringify(stay)),
      () =>
        new Response(JSON.stringify({ error: "not_found", message: "patient not found" }), {
          status: 404,
        }),
    );
    const { user } = renderWithProviders(
      <CounterAdmissionLookup scope={scope} value={null} onChange={vi.fn()} />,
    );
    await user.type(await screen.findByLabelText("Code"), "A-1");
    await user.click(screen.getByRole("button", { name: "Find admission" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Ask the hospital for patient lookup access.",
    );
    await waitFor(() =>
      expect(screen.queryByRole("button", { name: "Bill this admission" })).toBeNull(),
    );
  });
});
