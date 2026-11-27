import { beforeEach, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { readPrescriptionFulfilmentReview, type PrescriptionReviewContext } from "@helivanta/api";
import { PrescriptionFulfilmentReview } from "./prescription-fulfilment-review";
vi.mock("@helivanta/api", async (original) => ({
  ...(await original<typeof import("@helivanta/api")>()),
  readPrescriptionFulfilmentReview: vi.fn(),
}));
const id = "0199e000-0000-7000-8000-000000000001";
const scope = {
  tenantId: "11111111-1111-7111-8111-111111111111",
  pharmacyId: "0199e000-0000-7000-8000-000000000004",
  outletId: "0199e000-0000-7000-8000-000000000002",
  subject: "synthetic-preparer",
};
const source: PrescriptionReviewContext = {
  owner_tenant_id: scope.tenantId,
  pharmacy_id: scope.pharmacyId,
  reader_subject: scope.subject,
  source_schema_version: 1,
  source_kind: "recorded_prescription",
  source_authenticity: "not_evaluated",
  source_state_hash: "a".repeat(64),
  clinical_authority: "not_evaluated",
  clinical_context: {
    allergies: "unavailable",
    active_medications: "unavailable",
    patient_age: "unavailable",
    patient_weight: "unavailable",
    clinical_flags: "unavailable",
  },
  record: {
    id,
    outlet_id: scope.outletId,
    prescriber_name: "Synthetic Prescriber",
    prescriber_registration: "QA/123",
    prescriber_council: "Synthetic council",
    prescribed_on: "2026-11-25",
    patient_name: "Synthetic Patient",
    patient_address: null,
    prescriber_address: null,
    recorded_by: "synthetic-recorder",
    created_at: "2026-11-26T00:00:00Z",
    lines: [
      {
        line_number: 1,
        prescribed_as: "Synthetic medicine",
        product_id: id,
        quantity_units: 20,
        dispensings_allowed: 1,
        units_remaining: 15,
        dispensings_remaining: 1,
      },
    ],
  },
};
beforeEach(() => vi.clearAllMocks());
function show() {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <PrescriptionFulfilmentReview scope={scope} source={source} />
    </QueryClientProvider>,
  );
}
it("requires explicit units and never offers approval or dispatch", async () => {
  const user = userEvent.setup();
  show();
  expect(screen.getByLabelText("Proposed units for Synthetic medicine")).toHaveValue(null);
  await user.click(screen.getByRole("button", { name: "Check proposed quantities" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Choose");
  expect(readPrescriptionFulfilmentReview).not.toHaveBeenCalled();
  expect(
    screen.queryByRole("button", { name: /approve|dispense|dispatch/i }),
  ).not.toBeInTheDocument();
});
it("shows a source-bound proposal then invalidates it when quantities change", async () => {
  const user = userEvent.setup();
  vi.mocked(readPrescriptionFulfilmentReview).mockResolvedValue({
    source,
    lines: [{ line_number: 1, quantity_units: 5, product_id: id }],
    fulfilment_state_hash: "b".repeat(64),
    state: "proposed",
    stock_reservation: "not_reserved",
    clinical_authority: "not_evaluated",
  });
  show();
  await user.type(screen.getByLabelText("Proposed units for Synthetic medicine"), "5");
  await user.click(screen.getByRole("button", { name: "Check proposed quantities" }));
  await waitFor(() =>
    expect(screen.getByRole("status")).toHaveTextContent("Proposed quantities checked"),
  );
  expect(readPrescriptionFulfilmentReview).toHaveBeenCalledWith(scope, id, {
    source_state_hash: source.source_state_hash,
    lines: [{ line_number: 1, quantity_units: 5 }],
  });
  expect(screen.getByText(/stock has not been reserved/i)).toBeInTheDocument();
  await user.type(screen.getByLabelText("Proposed units for Synthetic medicine"), "1");
  expect(screen.queryByText(/Proposed quantities checked/)).not.toBeInTheDocument();
});

it("discards the clinical projection cache after the review closes", async () => {
  const client = new QueryClient();
  const user = userEvent.setup();
  vi.mocked(readPrescriptionFulfilmentReview).mockResolvedValue({
    source,
    lines: [{ line_number: 1, quantity_units: 5, product_id: id }],
    fulfilment_state_hash: "b".repeat(64),
    state: "proposed",
    stock_reservation: "not_reserved",
    clinical_authority: "not_evaluated",
  });
  const mounted = render(
    <QueryClientProvider client={client}>
      <PrescriptionFulfilmentReview scope={scope} source={source} />
    </QueryClientProvider>,
  );
  await user.type(screen.getByLabelText("Proposed units for Synthetic medicine"), "5");
  await user.click(screen.getByRole("button", { name: "Check proposed quantities" }));
  await screen.findByRole("status");
  mounted.unmount();
  await waitFor(() => expect(client.getMutationCache().getAll()).toHaveLength(0));
});
