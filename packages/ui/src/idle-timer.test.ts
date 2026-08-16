import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ACTIVITY_DEBOUNCE_MS, WARNING_LEAD_MS, createIdleTracker } from "./idle-timer";

describe("createIdleTracker", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  // Brief task-5-brief.md, Step 1 — verbatim (a trailing t.stop() is
  // appended to each for test hygiene — see the "leaked listeners" review
  // finding — without touching the given assertions themselves).
  it("calls onActivity at most once per debounce window under a burst", () => {
    vi.useFakeTimers();
    const onActivity = vi.fn();
    const t = createIdleTracker({ onActivity, onWarn: vi.fn(), onExpire: vi.fn() });
    t.start();
    for (let i = 0; i < 50; i++) window.dispatchEvent(new Event("keydown"));
    expect(onActivity).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(ACTIVITY_DEBOUNCE_MS + 1);
    window.dispatchEvent(new Event("keydown"));
    expect(onActivity).toHaveBeenCalledTimes(2);
    t.stop();
  });

  it("does not treat mouse movement alone as activity", () => {
    const onActivity = vi.fn();
    const t = createIdleTracker({ onActivity, onWarn: vi.fn(), onExpire: vi.fn() });
    t.start();
    window.dispatchEvent(new Event("mousemove"));
    expect(onActivity).not.toHaveBeenCalled();
    t.stop();
  });

  it("warns at the lead time before the deadline and expires at it", () => {
    vi.useFakeTimers();
    const onWarn = vi.fn(),
      onExpire = vi.fn();
    const t = createIdleTracker({ onActivity: vi.fn(), onWarn, onExpire });
    t.start();
    t.noteDeadline(new Date(Date.now() + 5 * 60_000));
    vi.advanceTimersByTime(3 * 60_000 - 1);
    expect(onWarn).not.toHaveBeenCalled();
    vi.advanceTimersByTime(2);
    expect(onWarn).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(2 * 60_000);
    expect(onExpire).toHaveBeenCalledTimes(1);
    t.stop();
  });

  it("a later deadline from another tab cancels a pending warning", () => {
    vi.useFakeTimers();
    const onWarn = vi.fn();
    const t = createIdleTracker({ onActivity: vi.fn(), onWarn, onExpire: vi.fn() });
    t.start();
    t.noteDeadline(new Date(Date.now() + 3 * 60_000));
    t.noteDeadline(new Date(Date.now() + 15 * 60_000)); // another tab was active
    vi.advanceTimersByTime(2 * 60_000);
    expect(onWarn).not.toHaveBeenCalled();
    t.stop();
  });

  // Additional coverage beyond the brief's four required tests.

  // CRITICAL review finding: scroll events do not bubble, and HmsShell's
  // real scroll container is an inner div, not `window` — a listener
  // added without `capture: true` never observes a real scroll on the
  // page, so a clinician scrolling a long chart with no other input
  // would silently be signed out. Dispatching on `window` (as the burst
  // test above does) cannot catch this, because a listener on window
  // sees a window-dispatched event regardless of bubbling; the event
  // must originate on a DIFFERENT node, as it does in the real DOM tree.
  it("observes a non-bubbling scroll event dispatched on an inner scroll container", () => {
    const onActivity = vi.fn();
    const t = createIdleTracker({ onActivity, onWarn: vi.fn(), onExpire: vi.fn() });
    const scrollContainer = document.createElement("div");
    document.body.appendChild(scrollContainer);
    t.start();
    // `scroll` events do not bubble by default (WHATWG spec) — this
    // mirrors real content scrolling inside HmsShell's
    // `overflow-y-auto` panel, never `window` itself.
    scrollContainer.dispatchEvent(new Event("scroll", { bubbles: false }));
    expect(onActivity).toHaveBeenCalledTimes(1);
    t.stop();
    document.body.removeChild(scrollContainer);
  });

  it("also treats pointerdown and scroll as qualifying activity", () => {
    vi.useFakeTimers();
    const onActivity = vi.fn();
    const t = createIdleTracker({ onActivity, onWarn: vi.fn(), onExpire: vi.fn() });
    t.start();
    window.dispatchEvent(new Event("pointerdown"));
    expect(onActivity).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(ACTIVITY_DEBOUNCE_MS + 1);
    window.dispatchEvent(new Event("scroll"));
    expect(onActivity).toHaveBeenCalledTimes(2);
  });

  it("ignores an earlier or equal deadline than the one already held", () => {
    vi.useFakeTimers();
    const onWarn = vi.fn();
    const t = createIdleTracker({ onActivity: vi.fn(), onWarn, onExpire: vi.fn() });
    t.start();
    const later = new Date(Date.now() + 10 * 60_000);
    t.noteDeadline(later);
    // An equal deadline: no-op, must not reset the already-scheduled warn.
    t.noteDeadline(new Date(later.getTime()));
    // An earlier deadline (e.g. a stale/out-of-order broadcast): ignored.
    t.noteDeadline(new Date(Date.now() + 1 * 60_000));
    // The warn should still be scheduled off the ORIGINAL (10m) deadline,
    // i.e. at 8 minutes — not the 1-minute deadline the stale note tried
    // to install, which would have fired already by now.
    vi.advanceTimersByTime(8 * 60_000 - 1);
    expect(onWarn).not.toHaveBeenCalled();
    vi.advanceTimersByTime(2);
    expect(onWarn).toHaveBeenCalledTimes(1);
  });

  it("stops listening and cancels pending timers once stopped", () => {
    vi.useFakeTimers();
    const onActivity = vi.fn();
    const onWarn = vi.fn();
    const onExpire = vi.fn();
    const t = createIdleTracker({ onActivity, onWarn, onExpire });
    t.start();
    t.noteDeadline(new Date(Date.now() + 5 * 60_000));
    t.stop();

    window.dispatchEvent(new Event("keydown"));
    expect(onActivity).not.toHaveBeenCalled();

    vi.advanceTimersByTime(10 * 60_000);
    expect(onWarn).not.toHaveBeenCalled();
    expect(onExpire).not.toHaveBeenCalled();
  });

  // Important review finding: an activity response can resolve after the
  // owning component unmounted and called stop() (hms-shell.tsx awaits
  // the POST, then calls tracker.noteDeadline in a .then()). Inert while
  // onExpire is a stub, but task 7 makes onExpire destructive (D6
  // teardown) — at that point this guard is the only thing standing
  // between a torn-down mount and a stray expiry firing against it.
  it("ignores a noteDeadline call made after stop()", () => {
    vi.useFakeTimers();
    const onWarn = vi.fn();
    const onExpire = vi.fn();
    const t = createIdleTracker({ onActivity: vi.fn(), onWarn, onExpire });
    t.start();
    t.stop();
    t.noteDeadline(new Date(Date.now() + 5 * 60_000));
    vi.advanceTimersByTime(10 * 60_000);
    expect(onWarn).not.toHaveBeenCalled();
    expect(onExpire).not.toHaveBeenCalled();
  });

  it("start is idempotent — calling it twice does not double-fire onActivity", () => {
    vi.useFakeTimers();
    const onActivity = vi.fn();
    const t = createIdleTracker({ onActivity, onWarn: vi.fn(), onExpire: vi.fn() });
    t.start();
    t.start();
    window.dispatchEvent(new Event("keydown"));
    expect(onActivity).toHaveBeenCalledTimes(1);
    t.stop();
  });

  describe("cross-tab broadcast", () => {
    let originalBroadcastChannel: typeof BroadcastChannel | undefined;

    beforeEach(() => {
      originalBroadcastChannel = globalThis.BroadcastChannel;
    });

    afterEach(() => {
      globalThis.BroadcastChannel = originalBroadcastChannel as typeof BroadcastChannel;
    });

    it("shares a noted deadline with another tab over BroadcastChannel", async () => {
      // Real timers deliberately: BroadcastChannel delivery is a real
      // task-queue hop, not a fake-timer-controllable setTimeout, so
      // fake timers would never let this message arrive. A deadline
      // just past the warning lead time keeps the real wait short.
      const onWarnA = vi.fn();
      const onWarnB = vi.fn();
      const tabA = createIdleTracker({ onActivity: vi.fn(), onWarn: onWarnA, onExpire: vi.fn() });
      const tabB = createIdleTracker({ onActivity: vi.fn(), onWarn: onWarnB, onExpire: vi.fn() });
      tabA.start();
      tabB.start();

      // Tab A learns of a fresh deadline (e.g. its own activity call
      // resolved) — tab B, which never called noteDeadline itself, must
      // pick up the same schedule purely from the broadcast.
      tabA.noteDeadline(new Date(Date.now() + WARNING_LEAD_MS + 20));

      await new Promise((resolve) => setTimeout(resolve, 200));
      expect(onWarnA).toHaveBeenCalledTimes(1);
      expect(onWarnB).toHaveBeenCalledTimes(1);

      tabA.stop();
      tabB.stop();
    });

    // Amplification-loop risk, previously unasserted: if a received
    // broadcast counted as activity itself, N tabs receiving one
    // broadcast could each fire their OWN onActivity/POST in response,
    // multiplying one real interaction into N requests — the opposite of
    // D4's "only one activity call is made for all of them".
    it("does not call onActivity in the receiver just because it received a broadcast deadline", async () => {
      const onActivityA = vi.fn();
      const onActivityB = vi.fn();
      const tabA = createIdleTracker({
        onActivity: onActivityA,
        onWarn: vi.fn(),
        onExpire: vi.fn(),
      });
      const tabB = createIdleTracker({
        onActivity: onActivityB,
        onWarn: vi.fn(),
        onExpire: vi.fn(),
      });
      tabA.start();
      tabB.start();

      tabA.noteDeadline(new Date(Date.now() + 5 * 60_000));
      await new Promise((resolve) => setTimeout(resolve, 100));

      expect(onActivityA).not.toHaveBeenCalled();
      expect(onActivityB).not.toHaveBeenCalled();

      tabA.stop();
      tabB.stop();
    });

    it("falls back to a per-tab-only timer when BroadcastChannel is unavailable", () => {
      // @ts-expect-error -- simulating an environment without it
      delete globalThis.BroadcastChannel;
      vi.useFakeTimers();
      const onWarn = vi.fn();
      const t = createIdleTracker({ onActivity: vi.fn(), onWarn, onExpire: vi.fn() });
      // Must not throw during start() just because the channel is gone.
      expect(() => t.start()).not.toThrow();
      t.noteDeadline(new Date(Date.now() + 5 * 60_000));
      vi.advanceTimersByTime(3 * 60_000 + 1);
      expect(onWarn).toHaveBeenCalledTimes(1);
      t.stop();
    });
  });

  it("ignores a deadline that is not a valid date", () => {
    vi.useFakeTimers();
    const onWarn = vi.fn();
    const onExpire = vi.fn();
    const t = createIdleTracker({ onActivity: vi.fn(), onWarn, onExpire });
    t.start();
    t.noteDeadline(new Date(Number.NaN));
    vi.advanceTimersByTime(60 * 60_000);
    expect(onWarn).not.toHaveBeenCalled();
    expect(onExpire).not.toHaveBeenCalled();
    t.stop();
  });
});
