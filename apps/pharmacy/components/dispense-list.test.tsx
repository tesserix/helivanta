import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@hms/api/testing";
import { DispenseList } from "./dispense-list";

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

const pendingRow = {
  id: "d-1",
  visit_id: "v-1",
  patient_name: "Asha Rao",
  medication: "",
  status: "pending",
  dispensed_at: null,
  created_at: "2026-08-04T04:00:00Z",
};

afterEach(() => vi.unstubAllGlobals());

describe("DispenseList", () => {
  it("dispenses a pending row", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(200, { data: [pendingRow] }))
      .mockResolvedValueOnce(jsonResponse(200, { id: "d-1", status: "dispensed" }))
      .mockResolvedValue(
        jsonResponse(200, {
          data: [{ ...pendingRow, status: "dispensed", medication: "Paracetamol 500mg", dispensed_at: "2026-08-04T04:01:00Z" }],
        }),
      );
    vi.stubGlobal("fetch", fetchMock);

    const { user } = renderWithProviders(<DispenseList />);
    await user.click(await screen.findByRole("button", { name: "Dispense" }));
    const lists = screen.getAllByRole("list");
    const dispensesList = lists[0];
    await waitFor(() => expect(within(dispensesList).getAllByText("Dispensed")).toHaveLength(1));
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/pharmacy/dispenses/d-1/dispense",
      expect.objectContaining({ method: "POST" }),
    );
  });

  it("shows the empty state", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse(200, { data: [] })));
    renderWithProviders(<DispenseList />);
    expect(await screen.findByText(/No dispense tasks yet/)).toBeInTheDocument();
  });
});
