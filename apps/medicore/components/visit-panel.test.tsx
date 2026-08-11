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
    let created = false;
    const fetchMock = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
      if (url.includes("/iam/me/permissions")) {
        return Promise.resolve(jsonResponse(200, { data: ["medicore.visit.create"] }));
      }
      if (init?.method === "POST") {
        created = true;
        return Promise.resolve(jsonResponse(202, { id: "v-1" }));
      }
      return Promise.resolve(
        jsonResponse(200, {
          data: created
            ? [
                {
                  id: "v-1",
                  patient_name: "Asha Rao",
                  department: "OPD",
                  status: "open",
                  created_at: "2026-08-04T04:00:00Z",
                },
              ]
            : [],
        }),
      );
    });
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
    vi.stubGlobal(
      "fetch",
      vi.fn().mockImplementation((url: string) => {
        if (url.includes("/iam/me/permissions")) {
          return Promise.resolve(jsonResponse(200, { data: ["medicore.visit.create"] }));
        }
        return Promise.resolve(jsonResponse(200, { data: [] }));
      }),
    );
    const { user } = renderWithProviders(<VisitPanel department="OPD" />);
    await user.click(await screen.findByRole("button", { name: "Create visit" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(/required/i);
  });

  it("hides the create-visit form without permission", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockImplementation((url: string) => {
        if (url.includes("/iam/me/permissions")) {
          return Promise.resolve(jsonResponse(200, { data: [] }));
        }
        return Promise.resolve(jsonResponse(200, { data: [] }));
      }),
    );
    renderWithProviders(<VisitPanel department="OPD" />);
    await screen.findByText(/No visits yet/);
    expect(screen.queryByRole("button", { name: "Create visit" })).not.toBeInTheDocument();
  });
});
