import { screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { MedicationsPanel } from "./medications-panel";

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function page(data: unknown[], opts: { hasMore?: boolean; nextCursor?: string | null } = {}) {
  return { data, page: { next_cursor: opts.nextCursor ?? null, has_more: opts.hasMore ?? false } };
}

afterEach(() => vi.unstubAllGlobals());

describe("MedicationsPanel", () => {
  it("lists medications and adds one with the form", async () => {
    let added = false;
    const fetchMock = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
      if (init?.method === "POST") {
        added = true;
        return Promise.resolve(jsonResponse(202, { id: "m-1" }));
      }
      return Promise.resolve(
        jsonResponse(
          200,
          page(
            added
              ? [{ id: "m-1", name: "Paracetamol", strength: "500mg", created_at: "2026-08-04T04:00:00Z" }]
              : [],
          ),
        ),
      );
    });
    vi.stubGlobal("fetch", fetchMock);

    const { user } = renderWithProviders(<MedicationsPanel />);
    expect(await screen.findByText(/No medications yet/)).toBeInTheDocument();

    await user.type(screen.getByLabelText("Name"), "Paracetamol");
    await user.type(screen.getByLabelText("Strength"), "500mg");
    await user.click(screen.getByRole("button", { name: "Add medication" }));

    await waitFor(() => expect(screen.getByText("Paracetamol")).toBeInTheDocument());
  });

  it("shows an inline error when the name is empty", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse(200, page([]))));
    const { user } = renderWithProviders(<MedicationsPanel />);
    await user.click(await screen.findByRole("button", { name: "Add medication" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(/required/i);
  });

  it("loads the next page on click, then hides Load more once the list is complete", async () => {
    const first = { id: "m-1", name: "Paracetamol", strength: "500mg", created_at: "2026-08-04T04:00:00Z" };
    const second = { id: "m-2", name: "Ibuprofen", strength: "200mg", created_at: "2026-08-04T03:00:00Z" };
    const fetchMock = vi.fn().mockImplementation((url: string) => {
      if (url.includes("cursor=")) {
        return Promise.resolve(jsonResponse(200, page([second])));
      }
      return Promise.resolve(jsonResponse(200, page([first], { hasMore: true, nextCursor: "c1" })));
    });
    vi.stubGlobal("fetch", fetchMock);

    const { user } = renderWithProviders(<MedicationsPanel />);
    await screen.findByText("Paracetamol");
    expect(screen.queryByText("Ibuprofen")).not.toBeInTheDocument();

    const loadMore = screen.getByRole("button", { name: "Load more" });
    await user.click(loadMore);

    await waitFor(() => expect(screen.getByText("Ibuprofen")).toBeInTheDocument());
    expect(screen.queryByRole("button", { name: "Load more" })).not.toBeInTheDocument();
  });
});
