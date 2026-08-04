import { screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@hms/api/testing";
import { OrderList } from "./order-list";

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

const pendingOrder = {
  id: "o-1",
  visit_id: "v-1",
  patient_name: "Asha Rao",
  test_name: "CBC",
  status: "pending",
  result_value: null,
  resulted_at: null,
  created_at: "2026-08-04T04:00:00Z",
};

afterEach(() => vi.unstubAllGlobals());

describe("OrderList", () => {
  it("saves a result for a pending order", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(200, { data: [pendingOrder] }))
      .mockResolvedValueOnce(jsonResponse(200, { id: "o-1", status: "completed" }))
      .mockResolvedValue(
        jsonResponse(200, {
          data: [{ ...pendingOrder, status: "completed", result_value: "WBC 6.1", resulted_at: "2026-08-04T04:01:00Z" }],
        }),
      );
    vi.stubGlobal("fetch", fetchMock);

    const { user } = renderWithProviders(<OrderList />);
    await user.type(await screen.findByLabelText("Result for Asha Rao"), "WBC 6.1");
    await user.click(screen.getByRole("button", { name: "Save result" }));
    await waitFor(() => expect(screen.getByText("Result: WBC 6.1")).toBeInTheDocument());
  });
});
