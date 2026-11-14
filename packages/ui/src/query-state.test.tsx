import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { CalendarPlus } from "lucide-react";
import { describe, expect, it, vi } from "vitest";
import { ApiError } from "@helivanta/api";
import { QueryState } from "./query-state";

type Visit = { id: string; name: string };

function renderState(query: {
  items: Visit[];
  isPending: boolean;
  isError: boolean;
  error?: unknown;
  retry?: () => void;
}) {
  return render(
    <QueryState
      query={{ retry: () => {}, error: null, ...query }}
      label="visits"
      empty={{ icon: CalendarPlus, title: "No visits yet", hint: "Create the first one above." }}
    >
      {(items) => (
        <ul>
          {items.map((v) => (
            <li key={v.id}>{v.name}</li>
          ))}
        </ul>
      )}
    </QueryState>,
  );
}

const visit = { id: "v-1", name: "Asha Rao" };

// #1276: a list is empty only after a successful answer with no rows. Before
// the first answer, or after a failed one, items is [] too, and showing the
// empty state then reads an outage as an empty queue.
describe("QueryState", () => {
  it("shows a loading status, not the empty state, while pending", () => {
    renderState({ items: [], isPending: true, isError: false });
    expect(screen.getByRole("status", { name: "Loading visits…" })).toBeInTheDocument();
    expect(screen.queryByText("No visits yet")).not.toBeInTheDocument();
  });

  it("shows a failure, not the empty state, when the first load fails", async () => {
    const retry = vi.fn();
    renderState({ items: [], isPending: false, isError: true, retry });
    expect(screen.getByRole("alert")).toHaveTextContent("Could not load visits.");
    expect(screen.queryByText("No visits yet")).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(retry).toHaveBeenCalledOnce();
  });

  it("keeps the loaded rows under a warning when a later load fails", () => {
    renderState({
      items: [visit],
      isPending: false,
      isError: true,
      error: new ApiError("internal", "boom", 500),
    });
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Could not load all visits. The list may be out of date.",
    );
    expect(screen.getByText("Asha Rao")).toBeInTheDocument();
  });

  it("shows the empty state after a successful empty answer", () => {
    renderState({ items: [], isPending: false, isError: false });
    expect(screen.getByText("No visits yet")).toBeInTheDocument();
    expect(screen.getByText("Create the first one above.")).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("renders the rows when there are some", () => {
    renderState({ items: [visit], isPending: false, isError: false });
    expect(screen.getByText("Asha Rao")).toBeInTheDocument();
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
  it("shows a refused first load as a refusal with no retry", () => {
    renderState({
      items: [],
      isPending: false,
      isError: true,
      error: new ApiError("forbidden", "no", 403),
    });
    expect(screen.getByRole("alert")).toHaveTextContent(
      "This wasn't found, or you don't have access to it.",
    );
    expect(screen.queryByRole("button", { name: "Try again" })).toBeNull();
    expect(screen.queryByText("No visits yet")).not.toBeInTheDocument();
  });

  // #1591: rows the server now refuses must not stay on screen as if current.
  it.each([
    [404, "This wasn't found, or you don't have access to it."],
    [401, "Your session has ended. Sign in again to continue."],
  ])("hides the rows when a refresh gets a %i", (status, sentence) => {
    renderState({
      items: [visit],
      isPending: false,
      isError: true,
      error: new ApiError("refused", "no", status),
    });
    expect(screen.getByRole("alert")).toHaveTextContent(sentence);
    expect(screen.queryByText("Asha Rao")).toBeNull();
    expect(screen.queryByText(/may be out of date/)).toBeNull();
    expect(screen.queryByRole("button", { name: "Try again" })).toBeNull();
  });
});
