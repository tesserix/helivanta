import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import { renderWithProviders } from "@hms/api/testing";
import { IdleWarning } from "./idle-warning";

// Harness for the focus-return test below: a real element the clinician
// was focused on (e.g. a form field mid-consultation) BEFORE the warning
// steals focus, so the test can assert focus actually comes back to it —
// not just that the dialog no longer renders. `open` starts false and
// flips on click (rather than being permanently open) so the harness
// controls WHEN the dialog mounts relative to the trigger's own focus,
// mirroring "something was focused, then the warning interrupted it".
function FocusReturnHarness() {
  const [open, setOpen] = useState(false);
  return (
    <>
      <button onClick={() => setOpen(true)}>Before the warning</button>
      {open && <IdleWarning secondsRemaining={120} onStay={() => setOpen(false)} />}
    </>
  );
}

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

  // Minor review finding: nothing pinned that focus is RETURNED once the
  // warning closes. This matters more than the trapping itself for a
  // clinician who was mid-form when the warning interrupted them — losing
  // their place afterward is its own small harm on top of the interruption.
  // `IdleWarning` does not implement this itself; it relies on
  // `@tesserix/web`'s `DialogContent`, which captures
  // `document.activeElement` when it mounts and restores it on unmount
  // (verified by reading `dialog.mjs`: no `DialogTrigger` is used here, so
  // it falls through to `previousFocusRef.current?.focus()`). This test
  // pins that upstream behaviour for THIS component's actual usage
  // (unmounting on Stay) so a `@tesserix/web` upgrade that changes it is
  // caught here, not discovered by a clinician losing their place.
  it("returns focus to the previously focused element once Stay signed in closes the warning", async () => {
    const { user } = renderWithProviders(<FocusReturnHarness />);
    const trigger = screen.getByRole("button", { name: "Before the warning" });

    // Clicking focuses `trigger` and, via its own onClick, mounts
    // IdleWarning — so `trigger` is exactly what DialogContent's mount
    // effect captures as "focused right before this dialog opened".
    await user.click(trigger);
    // The dialog steals focus onto its own first focusable element as
    // soon as it mounts (DialogContent's own effect) — confirms the
    // premise before checking the return.
    expect(document.activeElement).not.toBe(trigger);

    await user.click(screen.getByRole("button", { name: /stay signed in/i }));

    expect(document.activeElement).toBe(trigger);
  });
});
