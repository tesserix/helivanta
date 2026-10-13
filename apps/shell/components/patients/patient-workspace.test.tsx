import { screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { PatientWorkspace } from "./patient-workspace";

const hospital = "11111111-1111-4111-8111-111111111111";
const patient = {
  id: "22222222-2222-4222-8222-222222222222",
  hospital_id: hospital,
  mrn: "MRN-SYNTHETIC",
  given_name: "Synthetic",
  family_name: "Example",
  dob: "1990-01-01",
  dob_estimated: false,
  sex: "",
  mobile: "",
  address_line: "",
  created_at: "2026-10-13T00:00:00Z",
};
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
beforeEach(() => {
  Element.prototype.scrollIntoView = vi.fn();
});
afterEach(() => vi.unstubAllGlobals());
it("registers a synthetic patient in the selected hospital and displays the saved MRN", async () => {
  let saved = false;
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    let body: unknown = {};
    if (url.includes("permissions")) body = { data: ["patient.read", "patient.register"] };
    if (url.includes("workforce/hospitals"))
      body = page([
        { id: hospital, name: "YES Main Hospital (Synthetic)", access_level: "manager" },
      ]);
    if (url.endsWith("/access")) body = { level: "registrar" };
    if (url.includes("/patients/search")) body = page(saved ? [patient] : []);
    if (url.includes("/patients?") && init?.method === "POST") {
      saved = true;
      body = { patient, possible_matches: [] };
    }
    return new Response(JSON.stringify(body), { status: 200 });
  });
  vi.stubGlobal("fetch", fetcher);
  const { user } = renderWithProviders(<PatientWorkspace />);
  await user.click(await screen.findByRole("button", { name: "Register patient" }));
  expect(screen.getByLabelText("Given name")).toHaveFocus();
  await user.click(screen.getByRole("button", { name: "Cancel registration" }));
  expect(screen.getByRole("button", { name: "Register patient" })).toHaveFocus();
  await user.click(screen.getByRole("button", { name: "Register patient" }));
  expect(screen.getByLabelText("Given name")).toHaveFocus();
  await user.type(screen.getByLabelText("Given name"), "Synthetic");
  await user.type(screen.getByLabelText("Family name"), "Example");
  expect(screen.getByLabelText("Family name")).toHaveFocus();
  await user.click(screen.getByRole("button", { name: "Register patient" }));
  expect(screen.getByLabelText("Given name")).toHaveFocus();
  expect(screen.getByLabelText("Family name")).toHaveValue("Example");
  await user.type(screen.getByLabelText("Date of birth"), "1990-01-01");
  await user.type(screen.getByLabelText("Consent notice version"), "synthetic-v1");
  await user.click(screen.getByLabelText("I have recorded the patient's or guardian's consent"));
  await user.click(screen.getByRole("button", { name: "Save registration" }));
  await waitFor(() =>
    expect(fetcher).toHaveBeenCalledWith(
      `/api/v1/patients?hospital_id=${hospital}`,
      expect.objectContaining({ method: "POST", body: expect.stringContaining('"request_id"') }),
    ),
  );
  expect(await screen.findByText("MRN-SYNTHETIC")).toBeInTheDocument();
});
it("does not expose registration when clinical access is missing", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async (url: string) =>
        new Response(
          JSON.stringify(
            url.includes("workforce/hospitals")
              ? page([
                  { id: hospital, name: "YES Main Hospital (Synthetic)", access_level: "manager" },
                ])
              : url.includes("permissions")
                ? { data: ["patient.read", "patient.register"] }
                : { error: "not_found", message: "patient not found" },
          ),
          { status: url.endsWith("/access") ? 404 : 200 },
        ),
    ),
  );
  renderWithProviders(<PatientWorkspace />);
  expect(await screen.findByRole("alert")).toHaveTextContent("Clinical access is not available");
  expect(screen.queryByRole("button", { name: "Register patient" })).not.toBeInTheDocument();
});
