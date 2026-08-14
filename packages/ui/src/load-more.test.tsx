import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { LoadMore } from "./load-more";

describe("LoadMore", () => {
  it("renders nothing when there is nothing more", () => {
    // The absence of the control is a positive statement that the client
    // holds the whole collection — the thing #816's silently-truncated
    // lists could never say. A disabled-but-present button would leave
    // the same ambiguity.
    render(<LoadMore hasMore={false} isLoading={false} onClick={vi.fn()} />);

    expect(screen.queryByRole("button")).toBeNull();
  });

  it("fetches the next page when clicked", async () => {
    const user = userEvent.setup();
    const onClick = vi.fn();
    render(<LoadMore hasMore isLoading={false} onClick={onClick} />);

    await user.click(screen.getByRole("button", { name: "Load more" }));

    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it("disables itself and says so while a page is in flight", async () => {
    // Without this, a clinician on hospital wifi clicks Load more, sees
    // nothing change for a second, and clicks again. The hook drops the
    // duplicate fetch, but the user has no feedback that the first click
    // registered — so the control has to say it is working, and refuse
    // the second click itself rather than relying on the hook to swallow
    // it silently.
    const user = userEvent.setup();
    const onClick = vi.fn();
    render(<LoadMore hasMore isLoading onClick={onClick} />);

    const button = screen.getByRole("button", { name: "Loading…" });
    expect(button).toBeDisabled();

    await user.click(button);
    expect(onClick).not.toHaveBeenCalled();
  });
});
