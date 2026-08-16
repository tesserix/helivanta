/**
 * Framework-free idle-session tracker (design spec D2/D4/D7,
 * `docs/superpowers/specs/2026-08-16-idle-timeout-design.md`).
 *
 * Deliberately has no React in it: the mount (`hms-shell.tsx`) is a thin
 * `useEffect` wrapper, and every rule below is testable with fake timers
 * and a couple of `window.dispatchEvent` calls, no DOM-rendering harness
 * required.
 *
 * **Why this lives in `@hms/ui`, not `apps/shell`.** Every HMS app
 * (`apps/medicore`, `apps/pharmacy`, `apps/lab`) renders `HmsShell`. A
 * clinician working inside `/medicore` for twenty minutes is ACTIVE — if
 * only the shell tracked interaction they would be signed out
 * mid-consultation, failing the feature's own acceptance criterion while
 * looking implemented. `zitadel-session.ts`'s doc comment records the
 * exact same mistake already made once for sign-out: shell-only tracking
 * silently did the wrong thing on the majority path (working inside a
 * zone, not the dashboard). This module exists so every app gets the
 * SAME tracker by rendering `HmsShell`, not a shell-only copy.
 */

// Debounce window for the activity signal (D4): an active clinician
// produces one cheap request a minute, not one per keystroke.
export const ACTIVITY_DEBOUNCE_MS = 60_000;

// How far ahead of idle_deadline the warning modal (task 6) fires (D5).
export const WARNING_LEAD_MS = 120_000;

// Every HMS app shares this origin, so a single named channel is enough
// for every tab and every zone app to agree on one deadline (D4: "Tabs
// share one timer").
const BROADCAST_CHANNEL_NAME = "hms.session";

// pointerdown, keydown and a real scroll only — deliberately NOT
// mousemove. D4/D7: "Mouse movement alone does not count: a cleaner's
// sleeve on a desk is not a clinician." Any of these three requires an
// actual human doing something at the keyboard, mouse button, touch
// surface, or scroll wheel; a mouse merely resting on or drifting across
// a desk generates mousemove events with no one there.
const QUALIFYING_EVENTS = ["pointerdown", "keydown", "scroll"] as const;

interface DeadlineMessage {
  type: "deadline";
  deadline: string; // ISO 8601, see noteDeadline
}

export interface IdleTrackerHandlers {
  /** A debounced, genuine interaction was observed — go tell the server. */
  onActivity: () => void;
  /** The last known deadline is WARNING_LEAD_MS away. */
  onWarn: () => void;
  /** The last known deadline has passed. */
  onExpire: () => void;
}

export interface IdleTracker {
  /** Attach listeners and open the broadcast channel. Idempotent. */
  start: () => void;
  /** Detach everything this tracker holds. Idempotent. */
  stop: () => void;
  /**
   * Record the server's current idle_deadline (from the activity
   * endpoint's response body, or from another tab's broadcast of the
   * same) and (re)schedule onWarn/onExpire from it.
   *
   * An earlier-or-equal deadline than the one already held is ignored —
   * the server only ever moves idle_deadline forward (D3), so an
   * out-of-order broadcast or a stale response must never rewind the
   * schedule and resurrect a warning that was already cancelled.
   */
  noteDeadline: (deadline: Date) => void;
}

export function createIdleTracker(handlers: IdleTrackerHandlers): IdleTracker {
  let started = false;
  let lastActivityAt = -Infinity;
  let deadline: Date | undefined;
  let warnTimer: ReturnType<typeof setTimeout> | undefined;
  let expireTimer: ReturnType<typeof setTimeout> | undefined;
  let channel: BroadcastChannel | undefined;

  function clearScheduled(): void {
    if (warnTimer !== undefined) clearTimeout(warnTimer);
    if (expireTimer !== undefined) clearTimeout(expireTimer);
    warnTimer = undefined;
    expireTimer = undefined;
  }

  function scheduleFromDeadline(): void {
    clearScheduled();
    if (!deadline) return;
    const now = Date.now();
    const warnInMs = Math.max(0, deadline.getTime() - WARNING_LEAD_MS - now);
    const expireInMs = Math.max(0, deadline.getTime() - now);
    warnTimer = setTimeout(() => handlers.onWarn(), warnInMs);
    expireTimer = setTimeout(() => handlers.onExpire(), expireInMs);
  }

  function noteDeadline(newDeadline: Date, fromBroadcast = false): void {
    // A stop()ped tracker must schedule nothing: an activity response can
    // resolve after unmount (hms-shell.tsx awaits the POST, then calls
    // this), and with nothing left to clearTimeout() on the next start(),
    // a stray onExpire would eventually fire against a torn-down mount.
    if (!started) return;
    if (Number.isNaN(newDeadline.getTime())) return;
    if (deadline && newDeadline.getTime() <= deadline.getTime()) return;
    deadline = newDeadline;
    scheduleFromDeadline();
    // Re-broadcast our own writes so every other tab reschedules from
    // the same deadline (D4). Messages this tab receives are re-noted
    // (see onmessage below) but not re-broadcast, so tabs don't relay
    // the same deadline back and forth forever.
    if (!fromBroadcast) {
      channel?.postMessage({
        type: "deadline",
        deadline: newDeadline.toISOString(),
      } satisfies DeadlineMessage);
    }
  }

  function handleQualifyingEvent(): void {
    const now = Date.now();
    if (now - lastActivityAt < ACTIVITY_DEBOUNCE_MS) return;
    lastActivityAt = now;
    handlers.onActivity();
  }

  function start(): void {
    if (started) return;
    started = true;
    for (const event of QUALIFYING_EVENTS) {
      // capture: true is load-bearing for "scroll", not stylistic:
      // scroll events do NOT bubble, and HmsShell's actual scroll
      // container is an inner `overflow-y-auto` div
      // (hms-shell.tsx), not `window` — a bubble-phase listener on
      // window would never observe it. Capture-phase listeners see
      // every event on its way DOWN to the target regardless of
      // whether it bubbles back up, so this is the only phase that
      // works for all three qualifying events uniformly. The same
      // `{ capture: true }` shape must be passed to removeEventListener
      // in stop() below — capture is the one option addEventListener/
      // removeEventListener use to identify which listener to remove;
      // a mismatch (e.g. omitting it there) leaves this listener
      // permanently attached.
      window.addEventListener(event, handleQualifyingEvent, { passive: true, capture: true });
    }
    // BroadcastChannel is unavailable in a handful of older browsers and
    // some restricted embeds. Fall back to a per-tab-only timer rather
    // than throwing — "more requests, same behaviour, never a wrong
    // sign-out" (D4).
    if (typeof BroadcastChannel !== "undefined") {
      try {
        channel = new BroadcastChannel(BROADCAST_CHANNEL_NAME);
        channel.onmessage = (event: MessageEvent<unknown>) => {
          const data = event.data as Partial<DeadlineMessage> | undefined;
          if (data?.type === "deadline" && typeof data.deadline === "string") {
            // Another tab observed real interaction (that's the only way
            // it could have a fresher deadline to report). Count that
            // towards THIS tab's debounce window too — D4's "only one
            // activity call is made for all of them" would otherwise be
            // undone by two simultaneously-used tabs each running their
            // own independent debounce clock and both firing their own
            // request. This never calls onActivity itself, only widens
            // the window before this tab's own next qualifying event
            // would fire it — see the dedicated test for why that
            // distinction matters.
            lastActivityAt = Date.now();
            noteDeadline(new Date(data.deadline), true);
          }
        };
      } catch {
        channel = undefined;
      }
    }
  }

  function stop(): void {
    if (!started) return;
    started = false;
    for (const event of QUALIFYING_EVENTS) {
      window.removeEventListener(event, handleQualifyingEvent, { capture: true });
    }
    clearScheduled();
    channel?.close();
    channel = undefined;
  }

  return { start, stop, noteDeadline: (d: Date) => noteDeadline(d, false) };
}
