import { screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { DispenseList } from "./dispense-list";

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function page(data: unknown[], opts: { hasMore?: boolean; nextCursor?: string | null } = {}) {
  return { data, page: { next_cursor: opts.nextCursor ?? null, has_more: opts.hasMore ?? false } };
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
  it("dispenses a pending row with the medication typed in that row", async () => {
    let posted: unknown = null;
    const fetchMock = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
      if (url.includes("/iam/me/permissions")) {
        return Promise.resolve(jsonResponse(200, { data: ["pharmacy.dispense.fulfil"] }));
      }
      if (init?.method === "POST") {
        posted = JSON.parse(String(init.body));
        return Promise.resolve(jsonResponse(200, { id: "d-1", status: "dispensed" }));
      }
      return Promise.resolve(
        jsonResponse(
          200,
          page([
            posted
              ? {
                  ...pendingRow,
                  status: "dispensed",
                  medication: "Amoxicillin 250mg",
                  dispensed_at: "2026-08-04T04:01:00Z",
                }
              : pendingRow,
          ]),
        ),
      );
    });
    vi.stubGlobal("fetch", fetchMock);

    const { user } = renderWithProviders(<DispenseList />);
    await user.type(await screen.findByLabelText("Medication"), "Amoxicillin 250mg");
    await user.click(screen.getByRole("button", { name: "Dispense" }));
    await waitFor(() => expect(screen.getByText("Amoxicillin 250mg")).toBeInTheDocument());
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/pharmacy/dispenses/d-1/dispense",
      expect.objectContaining({ method: "POST" }),
    );
    expect(posted).toEqual({ medication: "Amoxicillin 250mg" });
  });

  it("refuses a blank medication without posting", async () => {
    const fetchMock = vi.fn().mockImplementation((url: string) => {
      if (url.includes("/iam/me/permissions")) {
        return Promise.resolve(jsonResponse(200, { data: ["pharmacy.dispense.fulfil"] }));
      }
      return Promise.resolve(jsonResponse(200, page([pendingRow])));
    });
    vi.stubGlobal("fetch", fetchMock);

    const { user } = renderWithProviders(<DispenseList />);
    await user.type(await screen.findByLabelText("Medication"), "   ");
    await user.click(screen.getByRole("button", { name: "Dispense" }));
    expect(await screen.findByText("Medication is required")).toBeInTheDocument();
    expect(fetchMock.mock.calls.some(([, init]) => init?.method === "POST")).toBe(false);
  });

  it("keeps each row's medication to that row", async () => {
    const secondRow = { ...pendingRow, id: "d-2", visit_id: "v-2", patient_name: "Rohan Iyer" };
    vi.stubGlobal(
      "fetch",
      vi.fn().mockImplementation((url: string) => {
        if (url.includes("/iam/me/permissions")) {
          return Promise.resolve(jsonResponse(200, { data: ["pharmacy.dispense.fulfil"] }));
        }
        return Promise.resolve(jsonResponse(200, page([pendingRow, secondRow])));
      }),
    );

    const { user } = renderWithProviders(<DispenseList />);
    const [first, second] = await screen.findAllByLabelText("Medication");
    expect(first).toHaveValue("");
    await user.type(first, "Amoxicillin 250mg");
    expect(second).toHaveValue("");
  });

  it("shows the empty state", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse(200, page([]))));
    renderWithProviders(<DispenseList />);
    expect(await screen.findByText(/No dispense tasks yet/)).toBeInTheDocument();
  });

  it("hides the dispense button without permission", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockImplementation((url: string) => {
        if (url.includes("/iam/me/permissions")) {
          return Promise.resolve(jsonResponse(200, { data: [] }));
        }
        return Promise.resolve(jsonResponse(200, page([pendingRow])));
      }),
    );
    renderWithProviders(<DispenseList />);
    await screen.findByText("Asha Rao");
    expect(screen.queryByRole("button", { name: "Dispense" })).not.toBeInTheDocument();
  });

  it("loads the next page on click, then hides Load more once the list is complete", async () => {
    const secondRow = { ...pendingRow, id: "d-2", patient_name: "Rohan Iyer" };
    const fetchMock = vi.fn().mockImplementation((url: string) => {
      if (url.includes("/iam/me/permissions")) {
        return Promise.resolve(jsonResponse(200, { data: [] }));
      }
      if (url.includes("cursor=")) {
        return Promise.resolve(jsonResponse(200, page([secondRow])));
      }
      return Promise.resolve(
        jsonResponse(200, page([pendingRow], { hasMore: true, nextCursor: "c1" })),
      );
    });
    vi.stubGlobal("fetch", fetchMock);

    const { user } = renderWithProviders(<DispenseList />);
    await screen.findByText("Asha Rao");
    expect(screen.queryByText("Rohan Iyer")).not.toBeInTheDocument();

    const loadMore = screen.getByRole("button", { name: "Load more" });
    await user.click(loadMore);

    await waitFor(() => expect(screen.getByText("Rohan Iyer")).toBeInTheDocument());
    expect(screen.queryByRole("button", { name: "Load more" })).not.toBeInTheDocument();
  });
});
