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
  it("dispenses a pending row", async () => {
    let dispensed = false;
    const fetchMock = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
      if (url.includes("/iam/me/permissions")) {
        return Promise.resolve(jsonResponse(200, { data: ["pharmacy.dispense.fulfil"] }));
      }
      if (init?.method === "POST") {
        dispensed = true;
        return Promise.resolve(jsonResponse(200, { id: "d-1", status: "dispensed" }));
      }
      return Promise.resolve(
        jsonResponse(
          200,
          page([
            dispensed
              ? {
                  ...pendingRow,
                  status: "dispensed",
                  medication: "Paracetamol 500mg",
                  dispensed_at: "2026-08-04T04:01:00Z",
                }
              : pendingRow,
          ]),
        ),
      );
    });
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
