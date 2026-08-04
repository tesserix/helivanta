import { screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@hms/api/testing";
import { VisitPanel } from "./visit-panel";

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

afterEach(() => vi.unstubAllGlobals());

describe("VisitPanel", () => {
  it("lists visits and creates one with the form", async () => {
    const fetchMock = vi
      .fn()
      // initial list
      .mockResolvedValueOnce(jsonResponse(200, { data: [] }))
      // create
      .mockResolvedValueOnce(jsonResponse(202, { id: "v-1" }))
      // refetch after invalidation
      .mockResolvedValue(
        jsonResponse(200, {
          data: [
            {
              id: "v-1",
              patient_name: "Asha Rao",
              department: "OPD",
              status: "open",
              created_at: "2026-08-04T04:00:00Z",
            },
          ],
        }),
      );
    vi.stubGlobal("fetch", fetchMock);

    const { user } = renderWithProviders(<VisitPanel department="OPD" />);
    expect(await screen.findByText(/No visits yet/)).toBeInTheDocument();

    await user.type(screen.getByLabelText("Patient name"), "Asha Rao");
    await user.click(screen.getByRole("button", { name: "Create visit" }));

    await waitFor(() => expect(screen.getByText("Asha Rao")).toBeInTheDocument());
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/medicore/visits",
      expect.objectContaining({ method: "POST" }),
    );
  });

  it("shows an inline error when the name is empty", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse(200, { data: [] })));
    const { user } = renderWithProviders(<VisitPanel department="OPD" />);
    await user.click(await screen.findByRole("button", { name: "Create visit" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(/required/i);
  });
});
