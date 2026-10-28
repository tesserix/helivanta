import { screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { DispenseList } from "./dispense-list";

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function page(data: unknown[], opts: { nextCursor?: string | null; total?: number } = {}) {
  return {
    data,
    page: {
      next_cursor: opts.nextCursor ?? null,
      has_more: !!opts.nextCursor,
      total: opts.total ?? data.length,
    },
  };
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

type Lists = { waiting?: unknown[]; dispensed?: unknown[] };

// #1295 D8: each tab is its own counted route.
function serve(lists: () => Lists, granted: string[] = ["pharmacy.dispense.fulfil"]) {
  return vi.fn().mockImplementation((url: string, init?: RequestInit) => {
    if (url.includes("/iam/me/permissions"))
      return Promise.resolve(jsonResponse(200, { data: granted }));
    if (init?.method === "POST")
      return Promise.resolve(jsonResponse(200, { id: "d-1", status: "dispensed" }));
    const tab = url.includes("/dispenses/dispensed") ? "dispensed" : "waiting";
    return Promise.resolve(jsonResponse(200, page(lists()[tab] ?? [])));
  });
}

// The page state lives in the URL (#1295 D5); tests set it here.
const nav = vi.hoisted(() => ({ search: "", replace: vi.fn() }));
vi.mock("next/navigation", () => ({
  usePathname: () => "/pharmacy",
  useSearchParams: () => new URLSearchParams(nav.search),
  useRouter: () => ({ replace: nav.replace }),
}));

afterEach(() => {
  vi.unstubAllGlobals();
  nav.search = "";
  nav.replace.mockReset();
});

const listUrls = (fetchMock: ReturnType<typeof vi.fn>) =>
  fetchMock.mock.calls
    .map(([url]) => String(url))
    .filter((url) => url.includes("/pharmacy/dispenses"));

describe("DispenseList", () => {
  it("opens on Waiting, 25 a page from the counted route, with the range", async () => {
    const fetchMock = vi
      .fn()
      .mockImplementation((url: string) =>
        Promise.resolve(
          url.includes("/iam/me/permissions")
            ? jsonResponse(200, { data: [] })
            : jsonResponse(200, page([pendingRow], { nextCursor: "c1", total: 30 })),
        ),
      );
    vi.stubGlobal("fetch", fetchMock);
    renderWithProviders(<DispenseList />);
    expect(await screen.findByText("Asha Rao")).toBeInTheDocument();
    expect(screen.getByText("1–1 of 30")).toBeInTheDocument();
    expect(listUrls(fetchMock)).toEqual(["/api/v1/pharmacy/dispenses/waiting?limit=25"]);
    expect(screen.getByRole("link", { name: "Waiting" })).toHaveAttribute("aria-current", "page");
    const next = screen.getByRole("link", { name: "Next" }).getAttribute("href")!;
    const params = new URL(next, "http://x").searchParams;
    expect(params.get("tab")).toBe("waiting");
    expect(params.get("page")).toBe("2");
    expect(params.get("cursor")).toBe("c1");
  });

  it("reads the Dispensed tab from the URL", async () => {
    nav.search = "tab=dispensed";
    const fetchMock = serve(() => ({ waiting: [pendingRow], dispensed: [dispensedRow] }));
    vi.stubGlobal("fetch", fetchMock);
    renderWithProviders(<DispenseList />);
    expect(await screen.findByText("Meera Nair")).toBeInTheDocument();
    expect(screen.queryByText("Asha Rao")).not.toBeInTheDocument();
    expect(listUrls(fetchMock).every((url) => url.includes("/dispenses/dispensed"))).toBe(true);
    expect(screen.getByRole("link", { name: "Dispensed" })).toHaveAttribute("aria-current", "page");
  });

  // #1295 D5: a reload on page 3 cannot know where page 2 began.
  it("offers First page, not Previous, when opened on a later page", async () => {
    nav.search = "tab=waiting&page=3&cursor=c2";
    const fetchMock = serve(() => ({ waiting: [pendingRow] }));
    vi.stubGlobal("fetch", fetchMock);
    renderWithProviders(<DispenseList />);
    await screen.findByText("Asha Rao");
    expect(listUrls(fetchMock)).toContain("/api/v1/pharmacy/dispenses/waiting?limit=25&cursor=c2");
    expect(screen.queryByRole("link", { name: "Previous" })).toBeNull();
    expect(screen.getByRole("link", { name: "First page" })).toHaveAttribute(
      "href",
      "/pharmacy?tab=waiting",
    );
  });

  // Review Important 2: a page position recorded under one search must
  // never become another list's Previous.
  it("keeps each search's page positions apart", async () => {
    nav.search = "tab=waiting&q=asha&page=2&cursor=cA";
    const fetchMock = serve(() => ({ waiting: [pendingRow] }));
    vi.stubGlobal("fetch", fetchMock);
    // The URL is mocked, so a parent re-render stands in for navigation.
    function Navigate() {
      const [, bump] = useState(0);
      return (
        <>
          <button onClick={() => bump((n) => n + 1)}>navigate</button>
          <DispenseList />
        </>
      );
    }
    const { user } = renderWithProviders(<Navigate />);
    await screen.findByText("Asha Rao");

    // Browser history jumps to page 3 of the unsearched queue.
    nav.search = "tab=waiting&page=3&cursor=c3";
    await user.click(screen.getByRole("button", { name: "navigate" }));
    await screen.findByText("Asha Rao");
    expect(screen.queryByRole("link", { name: "Previous" })).toBeNull();
    expect(screen.getByRole("link", { name: "First page" })).toBeVisible();
  });

  // Dispensing the last row on a later page empties it; fall back, never
  // show an empty page under "of N".
  it("falls back to the first page when a later page is empty", async () => {
    nav.search = "tab=waiting&page=2&cursor=c1";
    vi.stubGlobal(
      "fetch",
      serve(() => ({ waiting: [] })),
    );
    renderWithProviders(<DispenseList />);
    await waitFor(() => expect(nav.replace).toHaveBeenCalledWith("/pharmacy?tab=waiting"));
  });

  // #1295 D9: find the patient at the counter in a long queue.
  it("searches Waiting by patient name through the URL", async () => {
    const fetchMock = serve(() => ({ waiting: [pendingRow] }));
    vi.stubGlobal("fetch", fetchMock);
    const { user } = renderWithProviders(<DispenseList />);
    await screen.findByText("Asha Rao");
    await user.type(screen.getByRole("searchbox", { name: "Search waiting patients" }), "asha");
    await user.click(screen.getByRole("button", { name: "Search" }));
    expect(nav.replace).toHaveBeenCalledWith("/pharmacy?tab=waiting&q=asha");
  });

  it("asks the server for the searched name, and keeps it when paging", async () => {
    nav.search = "tab=waiting&q=asha";
    const fetchMock = vi
      .fn()
      .mockImplementation((url: string) =>
        Promise.resolve(
          url.includes("/iam/me/permissions")
            ? jsonResponse(200, { data: [] })
            : jsonResponse(200, page([pendingRow], { nextCursor: "c1", total: 40 })),
        ),
      );
    vi.stubGlobal("fetch", fetchMock);
    renderWithProviders(<DispenseList />);
    await screen.findByText("Asha Rao");
    expect(listUrls(fetchMock)).toEqual(["/api/v1/pharmacy/dispenses/waiting?q=asha&limit=25"]);
    expect(screen.getByRole("searchbox", { name: "Search waiting patients" })).toHaveValue("asha");
    const next = new URL(
      screen.getByRole("link", { name: "Next" }).getAttribute("href")!,
      "http://x",
    );
    expect(next.searchParams.get("q")).toBe("asha");
    expect(
      new URL(
        screen.getByRole("link", { name: "Dispensed" }).getAttribute("href")!,
        "http://x",
      ).searchParams.get("q"),
    ).toBe("asha");
  });

  // Spec D7: changing tab keeps the filters. The search belongs to Waiting,
  // so the Waiting tab link carries it back.
  it("keeps the Waiting search across a trip to Dispensed", async () => {
    nav.search = "tab=dispensed&q=asha";
    vi.stubGlobal(
      "fetch",
      serve(() => ({ dispensed: [dispensedRow] })),
    );
    renderWithProviders(<DispenseList />);
    await screen.findByText("Meera Nair");
    expect(screen.getByRole("link", { name: "Waiting" })).toHaveAttribute(
      "href",
      "/pharmacy?tab=waiting&q=asha",
    );
  });

  it("offers no search on Dispensed", async () => {
    nav.search = "tab=dispensed";
    vi.stubGlobal(
      "fetch",
      serve(() => ({ dispensed: [dispensedRow] })),
    );
    renderWithProviders(<DispenseList />);
    await screen.findByText("Meera Nair");
    expect(screen.queryByRole("searchbox")).toBeNull();
  });

  // Review Important 3: an emptied page N>=3 steps back to N-1 with the
  // cursor recorded on the way there, not to the first page.
  it("steps back one page, with its recorded cursor, when page 3 empties", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockImplementation((url: string) => {
        if (url.includes("/iam/me/permissions"))
          return Promise.resolve(jsonResponse(200, { data: [] }));
        if (url.includes("cursor=c2")) return Promise.resolve(jsonResponse(200, page([])));
        const next = url.includes("cursor=c1") ? "c2" : "c1";
        return Promise.resolve(
          jsonResponse(200, page([pendingRow], { nextCursor: next, total: 60 })),
        );
      }),
    );
    function Navigate() {
      const [, bump] = useState(0);
      return (
        <>
          <button onClick={() => bump((n) => n + 1)}>navigate</button>
          <DispenseList />
        </>
      );
    }
    const { user } = renderWithProviders(<Navigate />);
    await screen.findByText("Asha Rao");
    for (const search of ["tab=waiting&page=2&cursor=c1", "tab=waiting&page=3&cursor=c2"]) {
      nav.search = search;
      await user.click(screen.getByRole("button", { name: "navigate" }));
      if (search.includes("page=2")) await screen.findByText("Asha Rao");
    }
    await waitFor(() =>
      expect(nav.replace).toHaveBeenCalledWith("/pharmacy?tab=waiting&page=2&cursor=c1"),
    );
  });

  // The real way a page empties: its last row is dispensed.
  it("falls back when the last row on a later page is dispensed", async () => {
    nav.search = "tab=waiting&page=2&cursor=c1";
    let dispensed = false;
    const base = serve(() => ({ waiting: dispensed ? [] : [pendingRow] }));
    vi.stubGlobal(
      "fetch",
      vi.fn((url: string, init?: RequestInit) => {
        if (init?.method === "POST") dispensed = true;
        return base(url, init);
      }),
    );
    const { user } = renderWithProviders(<DispenseList />);
    await user.type(await screen.findByLabelText("Medication"), "Amoxicillin 250mg");
    expect(nav.replace).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "Dispense" }));
    await waitFor(() => expect(nav.replace).toHaveBeenCalledWith("/pharmacy?tab=waiting"));
  });

  it("removes a dispensed row from Waiting with the medication typed in that row", async () => {
    let posted: unknown = null;
    const base = serve(() => (posted ? {} : { waiting: [pendingRow] }));
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (init?.method === "POST") posted = JSON.parse(String(init.body));
      return base(url, init);
    });
    vi.stubGlobal("fetch", fetchMock);

    const { user } = renderWithProviders(<DispenseList />);
    await user.type(await screen.findByLabelText("Medication"), "Amoxicillin 250mg");
    await user.click(screen.getByRole("button", { name: "Dispense" }));
    await waitFor(() => expect(screen.queryByText("Asha Rao")).not.toBeInTheDocument());
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/pharmacy/dispenses/d-1/dispense",
      expect.objectContaining({ method: "POST" }),
    );
    expect(posted).toEqual({ medication: "Amoxicillin 250mg" });
  });

  it("refuses a blank medication without posting", async () => {
    const fetchMock = serve(() => ({ waiting: [pendingRow] }));
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
      serve(() => ({ waiting: [pendingRow, secondRow] })),
    );

    const { user } = renderWithProviders(<DispenseList />);
    const [first, second] = await screen.findAllByLabelText("Medication");
    expect(first).toHaveValue("");
    await user.type(first, "Amoxicillin 250mg");
    expect(second).toHaveValue("");
  });

  it("hides the dispense button without permission", async () => {
    vi.stubGlobal(
      "fetch",
      serve(() => ({ waiting: [pendingRow] }), []),
    );
    renderWithProviders(<DispenseList />);
    await screen.findByText("Asha Rao");
    expect(screen.queryByRole("button", { name: "Dispense" })).not.toBeInTheDocument();
  });

  it("says when nothing is waiting", async () => {
    vi.stubGlobal(
      "fetch",
      serve(() => ({})),
    );
    renderWithProviders(<DispenseList />);
    expect(await screen.findByText("Nothing waiting")).toBeInTheDocument();
    expect(screen.queryByRole("navigation", { name: "Pages" })).toBeNull();
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
    expect(await screen.findByText("Could not load waiting dispenses.")).toBeInTheDocument();
    expect(screen.queryByText("Nothing waiting")).not.toBeInTheDocument();
  });
});
