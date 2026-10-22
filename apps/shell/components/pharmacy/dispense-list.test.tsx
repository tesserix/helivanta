import { screen, waitFor, within } from "@testing-library/react";
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
const dispensedRow = {
  ...pendingRow,
  id: "d-9",
  patient_name: "Meera Nair",
  medication: "Paracetamol 500mg",
  status: "dispensed",
  dispensed_at: "2026-08-04T03:00:00Z",
};

type Lists = { pending?: unknown[]; dispensed?: unknown[] };

// The queue is split on the server (#1258 D4): each list asks for its own
// status, so the mock answers by the status query parameter.
function serve(lists: () => Lists, granted: string[] = ["pharmacy.dispense.fulfil"]) {
  return vi.fn().mockImplementation((url: string, init?: RequestInit) => {
    if (url.includes("/iam/me/permissions"))
      return Promise.resolve(jsonResponse(200, { data: granted }));
    if (init?.method === "POST")
      return Promise.resolve(jsonResponse(200, { id: "d-1", status: "dispensed" }));
    const status = new URL(url, "http://x").searchParams.get("status") as keyof Lists;
    return Promise.resolve(jsonResponse(200, page(lists()[status] ?? [])));
  });
}

const waiting = () => screen.getByRole("region", { name: "Waiting" });
const done = () => screen.getByRole("region", { name: "Dispensed" });

afterEach(() => vi.unstubAllGlobals());

describe("DispenseList", () => {
  it("asks the server for each status rather than splitting one page", async () => {
    const fetchMock = serve(() => ({ pending: [pendingRow], dispensed: [dispensedRow] }));
    vi.stubGlobal("fetch", fetchMock);
    renderWithProviders(<DispenseList />);
    expect(await within(waiting()).findByText("Asha Rao")).toBeInTheDocument();
    expect(await within(done()).findByText("Meera Nair")).toBeInTheDocument();
    const lists = fetchMock.mock.calls
      .map(([url]) => String(url))
      .filter((url) => url.includes("/pharmacy/dispenses"));
    expect(lists.some((url) => url.includes("status=pending"))).toBe(true);
    expect(lists.some((url) => url.includes("status=dispensed"))).toBe(true);
    expect(lists.every((url) => url.includes("status="))).toBe(true);
  });

  it("moves a row from Waiting to Dispensed with the medication typed in that row", async () => {
    let posted: unknown = null;
    const base = serve(() =>
      posted
        ? {
            dispensed: [
              {
                ...pendingRow,
                status: "dispensed",
                medication: "Amoxicillin 250mg",
                dispensed_at: "2026-08-04T04:01:00Z",
              },
            ],
          }
        : { pending: [pendingRow] },
    );
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (init?.method === "POST") posted = JSON.parse(String(init.body));
      return base(url, init);
    });
    vi.stubGlobal("fetch", fetchMock);

    const { user } = renderWithProviders(<DispenseList />);
    await user.type(await screen.findByLabelText("Medication"), "Amoxicillin 250mg");
    await user.click(screen.getByRole("button", { name: "Dispense" }));
    await waitFor(() => expect(within(done()).getByText("Amoxicillin 250mg")).toBeInTheDocument());
    expect(within(waiting()).queryByText("Asha Rao")).not.toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/pharmacy/dispenses/d-1/dispense",
      expect.objectContaining({ method: "POST" }),
    );
    expect(posted).toEqual({ medication: "Amoxicillin 250mg" });
  });

  it("refuses a blank medication without posting", async () => {
    const fetchMock = serve(() => ({ pending: [pendingRow] }));
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
      serve(() => ({ pending: [pendingRow, secondRow] })),
    );

    const { user } = renderWithProviders(<DispenseList />);
    const [first, second] = await screen.findAllByLabelText("Medication");
    expect(first).toHaveValue("");
    await user.type(first, "Amoxicillin 250mg");
    expect(second).toHaveValue("");
  });

  it("says when nothing is waiting", async () => {
    vi.stubGlobal(
      "fetch",
      serve(() => ({})),
    );
    renderWithProviders(<DispenseList />);
    expect(await within(waiting()).findByText("Nothing waiting")).toBeInTheDocument();
    expect(await within(done()).findByText("Nothing dispensed yet")).toBeInTheDocument();
  });

  it("hides the dispense button without permission", async () => {
    vi.stubGlobal(
      "fetch",
      serve(() => ({ pending: [pendingRow] }), []),
    );
    renderWithProviders(<DispenseList />);
    await screen.findByText("Asha Rao");
    expect(screen.queryByRole("button", { name: "Dispense" })).not.toBeInTheDocument();
  });

  it("loads the next page on click, then hides Load more once the list is complete", async () => {
    const secondRow = { ...pendingRow, id: "d-2", patient_name: "Rohan Iyer" };
    const fetchMock = vi.fn().mockImplementation((url: string) => {
      if (url.includes("/iam/me/permissions"))
        return Promise.resolve(jsonResponse(200, { data: [] }));
      if (url.includes("status=dispensed")) return Promise.resolve(jsonResponse(200, page([])));
      if (url.includes("cursor=")) return Promise.resolve(jsonResponse(200, page([secondRow])));
      return Promise.resolve(
        jsonResponse(200, page([pendingRow], { hasMore: true, nextCursor: "c1" })),
      );
    });
    vi.stubGlobal("fetch", fetchMock);

    const { user } = renderWithProviders(<DispenseList />);
    await screen.findByText("Asha Rao");
    expect(screen.queryByText("Rohan Iyer")).not.toBeInTheDocument();

    await user.click(within(waiting()).getByRole("button", { name: "Load more" }));

    await waitFor(() => expect(screen.getByText("Rohan Iyer")).toBeInTheDocument());
    expect(screen.queryByRole("button", { name: "Load more" })).not.toBeInTheDocument();
  });

  // #1276: a failed load is an outage, never an empty list.
  it("shows a failure, not the empty state, when the list cannot load", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockImplementation((url: string) =>
          Promise.resolve(
            url.includes("/iam/me/permissions")
              ? jsonResponse(200, { data: [] })
              : jsonResponse(503, { error: "unavailable" }),
          ),
        ),
    );
    renderWithProviders(<DispenseList />);
    expect(
      await within(waiting()).findByText("Could not load waiting dispenses."),
    ).toBeInTheDocument();
    expect(screen.queryByText("Nothing waiting")).not.toBeInTheDocument();
  });
});
