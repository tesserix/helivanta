import { describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import { renderWithProviders } from "@hms/api/testing";
import { IdleWarning } from "./idle-warning";

describe("IdleWarning", () => {
  // Brief task-6-brief.md, Step 1 — verbatim.
  it("shows a live countdown and a Stay signed in button", async () => {
    renderWithProviders(<IdleWarning secondsRemaining={120} onStay={vi.fn()} />);
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /stay signed in/i })).toBeInTheDocument();
    expect(screen.getByText(/2:00|120/)).toBeInTheDocument();
  });

  it("calls onStay when the button is pressed", async () => {
    const onStay = vi.fn();
    const { user } = renderWithProviders(<IdleWarning secondsRemaining={120} onStay={onStay} />);
    await user.click(screen.getByRole("button", { name: /stay signed in/i }));
    expect(onStay).toHaveBeenCalledTimes(1);
  });

  // Proves the countdown is a straight mm:ss rendering of whatever the
  // caller passes, not a formula this component reinvents — e.g. a naive
  // `Math.floor(seconds / 60)` off-by-one would print "1:30" as "1:29" or
  // similar. A component that hardcoded "2:00" would still pass the two
  // tests above; this uses a value that only matches if the formatting
  // logic is actually exercised.
  it("formats a non-round remaining time as minutes:seconds", () => {
    renderWithProviders(<IdleWarning secondsRemaining={90} onStay={vi.fn()} />);
    expect(screen.getByText(/1:30/)).toBeInTheDocument();
  });

  // Wording must attribute the ending session to inactivity, not to the
  // user having done something wrong, and must not claim they have
  // already been signed out (spec D5 / D6 distinguish "about to end" from
  // "signed out").
  it("attributes the ending session to inactivity, not the user, and does not claim they are already signed out", () => {
    renderWithProviders(<IdleWarning secondsRemaining={120} onStay={vi.fn()} />);
    const dialog = screen.getByRole("dialog");
    expect(dialog).toHaveTextContent(/inactivity/i);
    expect(dialog).not.toHaveTextContent(/signed out/i);
  });

  // Item 6 in the brief: dismissing the warning — not just clicking the
  // Stay button — must call onStay too, because hiding the modal without
  // extending the session would leave the clinician believing they are
  // safe while the deadline keeps counting down underneath them. This
  // exercises the REAL dismiss path (Escape, handled by
  // @tesserix/web's DialogContent) rather than calling a prop directly,
  // so a version that only wired the button (and left onOpenChange
  // wired to nothing) would fail here.
  it("calls onStay when the dialog is dismissed via Escape, not just via the button", async () => {
    const onStay = vi.fn();
    const { user } = renderWithProviders(<IdleWarning secondsRemaining={120} onStay={onStay} />);
    screen.getByRole("dialog").focus();
    await user.keyboard("{Escape}");
    expect(onStay).toHaveBeenCalledTimes(1);
  });
});
