import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import { renderWithProviders } from "@helivanta/api/testing";
import { PERMISSIONS_CACHE_KEY, RENEW_AT_KEY, storeRenewAt } from "@helivanta/api";
import { HmsShell } from "./hms-shell";
import { WARNING_LEAD_MS, type IdleTracker } from "./idle-timer";
import { IDLE_ENDED_MARK, SIGNED_OUT_MARK } from "./zitadel-session";

const endZitadelSession = vi.hoisted(() => vi.fn());
// Real SIGNED_OUT_MARK/IDLE_ENDED_MARK/SessionEndMark from the actual
// module, only endZitadelSession replaced — review finding (Minor): a
// hoisted literal re-declaration here would keep passing even if the
// real exported constant's VALUE changed while login/page.tsx (a
// different module, reading the real export) moved with it, silently
// decoupling this test from what ships.
vi.mock("./zitadel-session", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./zitadel-session")>();
  return { ...actual, endZitadelSession };
});

// Wraps the real createIdleTracker so tests can observe what HmsShell
// actually passes to noteDeadline — the shell test previously used a
// fetch stub shaped `{ data: [] }`, under which `new Date(undefined)` is
// silently discarded and the response-body-to-noteDeadline wiring was
// never exercised at all.
const noteDeadlineSpy = vi.hoisted(() => vi.fn());
// Every REAL tracker createIdleTracker() produces, in creation order — lets
// the idle-warning tests drive `noteDeadline` directly the same way a
// `BroadcastChannel` message from another tab would (idle-timer.ts's own
// onmessage handler calls exactly this method), without re-testing
// BroadcastChannel delivery itself (already covered in idle-timer.test.ts).
const createdTrackers = vi.hoisted(() => [] as IdleTracker[]);
vi.mock("./idle-timer", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./idle-timer")>();
  return {
    ...actual,
    createIdleTracker: (handlers: Parameters<typeof actual.createIdleTracker>[0]) => {
      const tracker = actual.createIdleTracker(handlers);
      createdTrackers.push(tracker);
      return {
        ...tracker,
        noteDeadline: (d: Date) => {
          noteDeadlineSpy(d);
          tracker.noteDeadline(d);
        },
      };
    },
  };
});

function seedCache(permissions: string[]) {
  window.localStorage.setItem(
    PERMISSIONS_CACHE_KEY,
    JSON.stringify({
      subject: "doc",
      tenantId: "tenant-a",
      permissions,
      storedAt: Date.now(),
    }),
  );
}

describe("HmsShell", () => {
  beforeEach(() => {
    window.localStorage.clear();
    window.sessionStorage.clear();
    vi.restoreAllMocks();
    endZitadelSession.mockReset();
    endZitadelSession.mockResolvedValue(true);
    noteDeadlineSpy.mockReset();
    createdTrackers.length = 0;
    // Deliberately different from the seeded cache: the two prove
    // different things. If the stub matched the cache, `Lab` would render
    // whether or not the cache was ever read, since the network response
    // would paint the same zone anyway.
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({
          data: ["pharmacy.dispense.read"],
          subject: "doc",
          tenant_id: "tenant-a",
        }),
      }),
    );
    // Sign-out ends in a hard navigation to /login (packages/ui/src/hms-shell.tsx),
    // which jsdom does not implement; stub it so that step is observable
    // instead of throwing "Not implemented: navigation".
    vi.stubGlobal("location", { ...window.location, href: "" });
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  // Proves the cache is actually read on first paint (not just present in
  // storage): the cache and the network stub grant different permissions,
  // and the zone visible immediately is the cached one, not the network
  // one — usePermissions paints from cache before the fetch resolves.
  it("paints the cached zone before the network response arrives", async () => {
    seedCache(["lab.order.read"]);
    renderWithProviders(<HmsShell active="/">content</HmsShell>);

    expect(screen.getByLabelText("Lab")).toBeInTheDocument();
  });

  // A shared hospital terminal must not show the next user the previous
  // user's nav, so signing out drops the cached set.
  it("clears the cached permissions when signing out", async () => {
    seedCache(["lab.order.read"]);
    const { user } = renderWithProviders(<HmsShell active="/">content</HmsShell>);

    await user.click(screen.getByLabelText("Sign out"));

    expect(window.localStorage.getItem(PERMISSIONS_CACHE_KEY)).toBeNull();
  });

  // Regresses #781: EVERY app — every zone, not only the shell — must
  // revoke the Helivanta session server-side AND end Zitadel's own SSO session
  // by default, with no `onSignOut` override needed any more. That used
  // to be shell-only; found broken live via the e2e suite (signing out
  // from a zone page left Zitadel's SSO cookie alive, so the next login —
  // even for a different identity — completed silently with no
  // credential prompt) and fixed by moving the Zitadel-ending step into
  // this package's own default (zitadel-session.ts).
  it("POSTs to /logout AND ends the Zitadel session by default when no onSignOut is supplied", async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 200 });
    vi.stubGlobal("fetch", fetchMock);
    const { user } = renderWithProviders(<HmsShell active="/">content</HmsShell>);

    await user.click(screen.getByLabelText("Sign out"));

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith("/logout", { method: "POST" }));
    // A deliberate sign-out asks for SIGNED_OUT_MARK specifically — never
    // IDLE_ENDED_MARK, and never with no mark (review finding: the two
    // teardown paths must each ask for their own mark, or the wrong one
    // can end up set).
    await waitFor(() => expect(endZitadelSession).toHaveBeenCalledWith(SIGNED_OUT_MARK));
    expect(endZitadelSession).toHaveBeenCalledTimes(1);
  });

  // #781's rule is that a signed-out session must not be reconstructable
  // from anything the browser kept, and #916 Task 4 gave the browser one
  // more thing to keep: the server's renewal schedule
  // (packages/api's renew-schedule.ts). It is not a credential and holds no
  // PHI, so leaving it was never a vulnerability — it is cleared so the
  // property holds with no exceptions, and asserted HERE because the unit
  // test on clearRenewAt() proves only that the function works, not that
  // sign-out ever calls it.
  it("drops the stored renewal schedule on sign-out", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: true, status: 200 }));
    storeRenewAt(new Date(Date.now() + 60_000).toISOString());
    expect(window.sessionStorage.getItem(RENEW_AT_KEY)).not.toBeNull();

    const { user } = renderWithProviders(<HmsShell active="/">content</HmsShell>);
    await user.click(screen.getByLabelText("Sign out"));

    await waitFor(() => expect(window.sessionStorage.getItem(RENEW_AT_KEY)).toBeNull());
  });

  // endZitadelSession() itself owns the same-origin fallback when it
  // can't reach Zitadel (no config, or the RP-initiated logout failed) —
  // see zitadel-session.ts. HmsShell must not ALSO navigate in that case,
  // or it would race whatever endZitadelSession() already started.
  it("does not navigate itself even when ending the Zitadel session falls back", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: true, status: 200 }));
    endZitadelSession.mockResolvedValue(false);
    const { user } = renderWithProviders(<HmsShell active="/">content</HmsShell>);

    await user.click(screen.getByLabelText("Sign out"));

    await waitFor(() => expect(endZitadelSession).toHaveBeenCalledTimes(1));
    expect(window.location.href).toBe("");
  });

  // A caller that DOES supply onSignOut (an extension point this package
  // still offers, even though shell no longer needs it — see the prop's
  // doc comment) must have it run instead of the built-in default, and
  // HmsShell must NOT also navigate itself: a redirect onSignOut starts
  // would be raced and cancelled by a same-origin `/login` navigation
  // right after (see handleSignOut's doc comment).
  it("runs the supplied onSignOut instead of the default POST, and never navigates itself", async () => {
    // renderWithProviders mounts usePermissions too, which fetches
    // /api/v1/iam/me/permissions on its own regardless of sign-out; the
    // assertion below only cares whether /logout specifically was hit.
    const fetchMock = vi
      .fn()
      .mockResolvedValue({ ok: true, status: 200, json: async () => ({ data: [] }) });
    vi.stubGlobal("fetch", fetchMock);
    let resolveSignOut: () => void = () => {};
    const onSignOut = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          resolveSignOut = resolve;
        }),
    );
    const { user } = renderWithProviders(
      <HmsShell active="/" onSignOut={onSignOut}>
        content
      </HmsShell>,
    );

    await user.click(screen.getByLabelText("Sign out"));

    expect(onSignOut).toHaveBeenCalledTimes(1);
    expect(window.location.href).toBe("");
    expect(fetchMock).not.toHaveBeenCalledWith("/logout", { method: "POST" });

    resolveSignOut();
    await waitFor(() => expect(onSignOut).toHaveResolved());
    // Still no navigation from HmsShell itself, even after onSignOut
    // resolves — ownership of navigation stayed with onSignOut the whole
    // time.
    expect(window.location.href).toBe("");
  });

  // #848 D4/D7: every app that renders HmsShell tracks interaction and
  // reports it to the activity endpoint, not only the shell's dashboard.
  describe("idle activity tracking", () => {
    it("POSTs to the activity endpoint on a qualifying interaction", async () => {
      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({ data: [] }),
      });
      vi.stubGlobal("fetch", fetchMock);
      renderWithProviders(<HmsShell active="/">content</HmsShell>);

      window.dispatchEvent(new Event("keydown"));

      await waitFor(() =>
        expect(fetchMock).toHaveBeenCalledWith(
          "/api/v1/auth/session/activity",
          expect.objectContaining({ method: "POST" }),
        ),
      );
    });

    // The single most important guarantee here (spec "Errors and failure
    // handling"): a transient failure on this background ping must never
    // end the session. Only the server-side idle_deadline may do that.
    it("does not sign the user out when the activity call fails", async () => {
      const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => {
        const url = typeof input === "string" ? input : input.toString();
        if (url.includes("/auth/session/activity")) {
          return Promise.reject(new Error("network down"));
        }
        return Promise.resolve({ ok: true, status: 200, json: async () => ({ data: [] }) });
      });
      vi.stubGlobal("fetch", fetchMock);
      renderWithProviders(<HmsShell active="/">content</HmsShell>);

      window.dispatchEvent(new Event("keydown"));

      await waitFor(() =>
        expect(fetchMock).toHaveBeenCalledWith(
          "/api/v1/auth/session/activity",
          expect.objectContaining({ method: "POST" }),
        ),
      );
      // Give the rejected promise's .catch() a turn, then assert nothing
      // resembling sign-out/teardown happened.
      await new Promise((resolve) => setTimeout(resolve, 0));
      expect(fetchMock).not.toHaveBeenCalledWith("/logout", { method: "POST" });
      expect(endZitadelSession).not.toHaveBeenCalled();
      expect(window.location.href).toBe("");
    });

    // Previously uncovered: the shell test's default fetch stub returns
    // `{ data: [] }`, under which `new Date(undefined)` is silently
    // discarded and this path is never actually exercised.
    it("passes the activity response's idle_deadline through to the tracker's noteDeadline", async () => {
      const returnedDeadline = "2026-08-16T12:34:56.000Z";
      const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => {
        const url = typeof input === "string" ? input : input.toString();
        if (url.includes("/auth/session/activity")) {
          return Promise.resolve({
            ok: true,
            status: 200,
            json: async () => ({ idle_deadline: returnedDeadline }),
          });
        }
        return Promise.resolve({ ok: true, status: 200, json: async () => ({ data: [] }) });
      });
      vi.stubGlobal("fetch", fetchMock);
      renderWithProviders(<HmsShell active="/">content</HmsShell>);

      window.dispatchEvent(new Event("keydown"));

      await waitFor(() => expect(noteDeadlineSpy).toHaveBeenCalledWith(new Date(returnedDeadline)));
    });
  });

  // #848 task 6 (D5): the warning modal itself, and the gap task 5
  // deliberately left open — see hms-shell.tsx's comment on the mount
  // effect for why seeding on mount is treated as a legitimate
  // interaction rather than a workaround.
  describe("idle warning modal", () => {
    function stubActivityFetch(deadlines: () => Date) {
      const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => {
        const url = typeof input === "string" ? input : input.toString();
        if (url.includes("/auth/session/activity")) {
          return Promise.resolve({
            ok: true,
            status: 200,
            json: async () => ({ idle_deadline: deadlines().toISOString() }),
          });
        }
        return Promise.resolve({ ok: true, status: 200, json: async () => ({ data: [] }) });
      });
      vi.stubGlobal("fetch", fetchMock);
      return fetchMock;
    }

    // Closes the gap recorded in hms-shell.tsx: without this, a terminal
    // that is loaded and never touched gets no client-side schedule at
    // all — no warning, ever — since noteDeadline only ever learns a
    // deadline from a successful activity response.
    it("reports activity on mount, before any interaction, so an untouched terminal still gets a schedule", async () => {
      const fetchMock = stubActivityFetch(() => new Date(Date.now() + 15 * 60_000));
      renderWithProviders(<HmsShell active="/">content</HmsShell>);

      await waitFor(() =>
        expect(fetchMock).toHaveBeenCalledWith(
          "/api/v1/auth/session/activity",
          expect.objectContaining({ method: "POST" }),
        ),
      );
      // No dispatchEvent anywhere above — this call can only have come
      // from the mount effect itself.
    });

    it("shows the warning dialog with a live countdown once the deadline reaches the lead time", async () => {
      vi.useFakeTimers();
      const deadline = new Date(Date.now() + WARNING_LEAD_MS + 5_000);
      stubActivityFetch(() => deadline);
      renderWithProviders(<HmsShell active="/">content</HmsShell>);

      // Let the mount-seeded activity call resolve and its
      // .then(noteDeadline) run, which schedules onWarn ~5s out.
      await act(async () => {
        await vi.advanceTimersByTimeAsync(0);
      });
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();

      await act(async () => {
        await vi.advanceTimersByTimeAsync(5_000);
      });

      const dialog = screen.getByRole("dialog");
      expect(dialog).toBeInTheDocument();
      expect(screen.getByRole("button", { name: /stay signed in/i })).toBeInTheDocument();
      // onWarn fires WARNING_LEAD_MS before the deadline (that's the
      // whole point of the lead time), so the countdown reads ~2:00 the
      // instant it appears.
      expect(dialog).toHaveTextContent(/2:00|1:59/);

      // The countdown is a display DERIVED from the deadline on every
      // tick, not a locally-decremented counter (spec's error-handling
      // rule) — ticking fake time forward must move the displayed value
      // down by roughly the same amount, proving it is actually reading
      // `deadline - Date.now()` each second rather than a value computed
      // once when the modal opened.
      await act(async () => {
        await vi.advanceTimersByTimeAsync(90_000);
      });
      expect(dialog).toHaveTextContent(/0:3\d/);
    });

    it("extends the session and hides the modal when Stay signed in is pressed", async () => {
      vi.useFakeTimers();
      const deadline = new Date(Date.now() + WARNING_LEAD_MS + 1_000);
      const fetchMock = stubActivityFetch(() => deadline);
      renderWithProviders(<HmsShell active="/">content</HmsShell>);

      await act(async () => {
        await vi.advanceTimersByTimeAsync(1_000);
      });
      expect(screen.getByRole("dialog")).toBeInTheDocument();
      const callsBeforeStay = fetchMock.mock.calls.filter((c) =>
        String(c[0]).includes("/auth/session/activity"),
      ).length;

      await act(async () => {
        fireEvent.click(screen.getByRole("button", { name: /stay signed in/i }));
        await vi.advanceTimersByTimeAsync(0);
      });

      // Dismissing via the button must call the SAME activity endpoint
      // that a qualifying interaction would — not merely hide the modal
      // (item 6: hiding without extending leaves the clinician believing
      // they are safe while the deadline keeps counting down).
      const callsAfterStay = fetchMock.mock.calls.filter((c) =>
        String(c[0]).includes("/auth/session/activity"),
      ).length;
      expect(callsAfterStay).toBeGreaterThan(callsBeforeStay);
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    });

    // D5: "Another tab may have extended the session, in which case the
    // modal must not appear at all." Drives the REAL tracker's
    // `noteDeadline` directly — exactly the call idle-timer.ts's own
    // BroadcastChannel `onmessage` handler makes on a message from
    // another tab (already unit-tested in idle-timer.test.ts) — so this
    // proves HmsShell's onWarn wiring actually respects that
    // cancellation end to end, rather than assuming it because the
    // lower-level module does.
    it("does not show the warning if the deadline was already extended before the lead time was reached", async () => {
      vi.useFakeTimers();
      const shortDeadline = new Date(Date.now() + WARNING_LEAD_MS + 5_000);
      stubActivityFetch(() => shortDeadline);
      renderWithProviders(<HmsShell active="/">content</HmsShell>);

      await act(async () => {
        await vi.advanceTimersByTimeAsync(0);
      });
      expect(createdTrackers).toHaveLength(1);

      // Simulate another tab's activity extending the session well past
      // the original deadline — the same call idle-timer.ts's
      // BroadcastChannel handler makes on receipt of another tab's
      // message.
      const extensionAt = Date.now();
      const extendedDeadline = new Date(extensionAt + 20 * 60_000);
      act(() => {
        createdTrackers[0]?.noteDeadline(extendedDeadline);
      });

      // Advance well past where the ORIGINAL deadline's warning would
      // have fired.
      await act(async () => {
        await vi.advanceTimersByTimeAsync(10_000);
      });

      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();

      // CRITICAL review finding: it is not enough that the ORIGINAL
      // warning was suppressed — the EXTENDED deadline gets its own
      // warning later, and that one must render the correct ~2:00, not
      // 0:00. A component that mirrored the deadline into its own ref
      // (written only from THIS tab's own activity responses) would
      // still hold the stale, already-past original deadline here, and
      // Math.max(0, ...) would floor a negative remainder to 0:00 — a
      // warning that claims the session is already gone while 2 real
      // minutes remain.
      const remainingToExtendedWarn = extendedDeadline.getTime() - WARNING_LEAD_MS - Date.now();
      await act(async () => {
        await vi.advanceTimersByTimeAsync(remainingToExtendedWarn);
      });

      const dialog = screen.getByRole("dialog");
      expect(dialog).toHaveTextContent(/2:00|1:59/);
      expect(dialog).not.toHaveTextContent(/0:00/);
    });

    // Important review finding — D5's "or any interaction … the modal
    // closes" was unimplemented: only the Stay button cleared the
    // warning, so a qualifying keydown/scroll while the modal was open
    // extended the session server-side but left the modal counting down
    // to 0:00 and sitting there forever (onExpire is still a stub). A
    // frozen modal over a live session teaches clinicians to ignore it —
    // worse than never warning them.
    it("clears the warning when a qualifying interaction extends the session while it is open", async () => {
      vi.useFakeTimers();
      let activityCalls = 0;
      const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => {
        const url = typeof input === "string" ? input : input.toString();
        if (url.includes("/auth/session/activity")) {
          activityCalls += 1;
          // First call (the mount seed) returns a deadline just past the
          // warning lead time, so the modal opens quickly. A LATER call
          // (the in-modal keydown below) returns a deadline a full 15
          // minutes out, as a genuine extension would.
          const d =
            activityCalls === 1
              ? new Date(Date.now() + WARNING_LEAD_MS + 65_000)
              : new Date(Date.now() + 15 * 60_000);
          return Promise.resolve({
            ok: true,
            status: 200,
            json: async () => ({ idle_deadline: d.toISOString() }),
          });
        }
        return Promise.resolve({ ok: true, status: 200, json: async () => ({ data: [] }) });
      });
      vi.stubGlobal("fetch", fetchMock);
      renderWithProviders(<HmsShell active="/">content</HmsShell>);

      // 65s: past both the warn point (WARNING_LEAD_MS + 65s - WARNING_LEAD_MS
      // = 65s) AND the mount seed's debounce window (60s), so the
      // keydown below is a genuine, undebounced qualifying interaction.
      await act(async () => {
        await vi.advanceTimersByTimeAsync(65_000);
      });
      expect(screen.getByRole("dialog")).toBeInTheDocument();

      await act(async () => {
        window.dispatchEvent(new Event("keydown"));
        await vi.advanceTimersByTimeAsync(0);
      });

      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    });

    // Same finding, the cross-tab path: another tab's broadcast extending
    // the deadline while THIS tab's modal is already open must also
    // clear it, not just prevent a not-yet-shown one from appearing (the
    // earlier test above).
    it("clears the warning when another tab's broadcast extends the deadline while it is open", async () => {
      vi.useFakeTimers();
      const deadline = new Date(Date.now() + WARNING_LEAD_MS + 1_000);
      stubActivityFetch(() => deadline);
      renderWithProviders(<HmsShell active="/">content</HmsShell>);

      await act(async () => {
        await vi.advanceTimersByTimeAsync(1_000);
      });
      expect(screen.getByRole("dialog")).toBeInTheDocument();

      const extended = new Date(Date.now() + 20 * 60_000);
      act(() => {
        createdTrackers[0]?.noteDeadline(extended);
      });

      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    });

    // Important review finding: the mount seed call reported activity to
    // the server but never advanced the tracker's OWN debounce clock, so
    // a qualifying interaction moments after mount (a clinician who
    // starts typing or scrolling right away) fired a SECOND, redundant
    // POST within the same debounce window — against the narrowly
    // budgeted `Tight` rate-limit bucket the activity endpoint uses.
    it("does not fire a second activity POST for a qualifying interaction immediately after the mount seed", async () => {
      vi.useFakeTimers();
      const fetchMock = stubActivityFetch(() => new Date(Date.now() + 15 * 60_000));
      renderWithProviders(<HmsShell active="/">content</HmsShell>);

      await act(async () => {
        await vi.advanceTimersByTimeAsync(0);
      });
      const callsAfterMount = fetchMock.mock.calls.filter((c) =>
        String(c[0]).includes("/auth/session/activity"),
      ).length;
      expect(callsAfterMount).toBe(1);

      await act(async () => {
        window.dispatchEvent(new Event("keydown"));
        await vi.advanceTimersByTimeAsync(0);
      });
      const callsAfterKeydown = fetchMock.mock.calls.filter((c) =>
        String(c[0]).includes("/auth/session/activity"),
      ).length;
      expect(callsAfterKeydown).toBe(1);
    });
  });

  // #848 task 7 (D6): ending THIS browser's session on idle expiry, from
  // BOTH triggers the spec requires — the client's own onExpire timer and
  // the server's 401 `session_idle` refusal.
  describe("idle session teardown (D6)", () => {
    function stubActivityFetch(deadlines: () => Date) {
      const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => {
        const url = typeof input === "string" ? input : input.toString();
        if (url.includes("/auth/session/activity")) {
          return Promise.resolve({
            ok: true,
            status: 200,
            json: async () => ({ idle_deadline: deadlines().toISOString() }),
          });
        }
        if (url === "/logout") {
          return Promise.resolve({ ok: true, status: 200 });
        }
        return Promise.resolve({ ok: true, status: 200, json: async () => ({ data: [] }) });
      });
      vi.stubGlobal("fetch", fetchMock);
      return fetchMock;
    }

    it("ends this browser's session when the client's own onExpire timer fires", async () => {
      vi.useFakeTimers();
      // A deadline close enough that the SAME advance clears both the
      // warn point (max(0, deadline - WARNING_LEAD_MS - now), floored at
      // 0 here since 5s < WARNING_LEAD_MS) and the deadline itself.
      const fetchMock = stubActivityFetch(() => new Date(Date.now() + 5_000));
      renderWithProviders(<HmsShell active="/">content</HmsShell>);

      // Two advances: the first crosses the deadline and fires onExpire,
      // whose async body (fetch("/logout") → endZitadelSession(mark))
      // resolves purely through microtasks the fake-timer flush already
      // drains; the second is a zero-length flush for any leftover tick.
      await act(async () => {
        await vi.advanceTimersByTimeAsync(5_000);
        await vi.advanceTimersByTimeAsync(0);
      });

      expect(fetchMock).toHaveBeenCalledWith("/logout", { method: "POST" });
      // A client timer reaching the deadline IS idle expiry — see
      // hms-shell.tsx's onExpire comment. endZitadelSession itself owns
      // the actual sessionStorage.setItem for this mark (proved directly
      // in zitadel-session.test.ts, since this module mocks
      // endZitadelSession wholesale); this test's job is only to prove
      // HmsShell asks for the right mark.
      expect(endZitadelSession).toHaveBeenCalledWith(IDLE_ENDED_MARK);
      expect(endZitadelSession).toHaveBeenCalledTimes(1);
    });

    // The idle path is the SECOND of the three teardown paths that must
    // leave nothing of this session in the browser
    // (packages/api/src/renew-schedule.ts's clearRenewAt comment lists all
    // three). Only the sign-out path was asserted before this — and a
    // component that cleared on sign-out but not on idle timeout would
    // have passed every existing test while leaving the previous
    // clinician's renewal schedule behind on a ward terminal that timed
    // out unattended, which is the very scenario #848 is about.
    it("drops the stored renewal schedule and cached permissions when the idle timer expires", async () => {
      vi.useFakeTimers();
      storeRenewAt(new Date(Date.now() + 60_000).toISOString());
      expect(window.sessionStorage.getItem(RENEW_AT_KEY)).not.toBeNull();
      // The permissions cache lives in localStorage (permissions-cache.ts),
      // not sessionStorage, so it survives a tab close and must be
      // dropped explicitly here too — otherwise the previous clinician's
      // cached permission set would survive an idle timeout on a shared
      // ward terminal, exactly the leak #848's idle timeout exists to
      // prevent.
      seedCache(["patients:read"]);
      expect(window.localStorage.getItem(PERMISSIONS_CACHE_KEY)).not.toBeNull();
      stubActivityFetch(() => new Date(Date.now() + 5_000));
      renderWithProviders(<HmsShell active="/">content</HmsShell>);

      await act(async () => {
        await vi.advanceTimersByTimeAsync(5_000);
        await vi.advanceTimersByTimeAsync(0);
      });

      expect(
        window.sessionStorage.getItem(RENEW_AT_KEY),
        "an idle timeout ends the session, so the schedule it left behind goes with it",
      ).toBeNull();
      expect(
        window.localStorage.getItem(PERMISSIONS_CACHE_KEY),
        "an idle timeout ends the session, so the cached permission set it left behind goes with it",
      ).toBeNull();
    });

    // D6 explicitly forbids subject-wide revocation here — that is
    // sign-out's #781 semantics. Nothing in this teardown path may call
    // anything resembling a global/subject revoke; the only server call is
    // the same-origin, this-browser-only POST /logout.
    it("never calls anything beyond this-browser teardown on expiry", async () => {
      vi.useFakeTimers();
      const fetchMock = stubActivityFetch(() => new Date(Date.now() + 5_000));
      renderWithProviders(<HmsShell active="/">content</HmsShell>);

      await act(async () => {
        await vi.advanceTimersByTimeAsync(5_000);
        await vi.advanceTimersByTimeAsync(0);
      });

      expect(endZitadelSession).toHaveBeenCalledTimes(1);
      // POSTs beyond the mount seed's activity call: exactly one, to
      // same-origin /logout — nothing resembling a subject-wide revoke
      // endpoint.
      const postCallsOtherThanActivity = fetchMock.mock.calls.filter((c) => {
        const url = typeof c[0] === "string" ? c[0] : c[0].toString();
        const init = c[1] as RequestInit | undefined;
        return init?.method === "POST" && !url.includes("/auth/session/activity");
      });
      expect(postCallsOtherThanActivity).toHaveLength(1);
      expect(postCallsOtherThanActivity[0]?.[0]).toBe("/logout");
    });

    // The other trigger: a 401 `session_idle` from the activity endpoint
    // means the session is ALREADY gone server-side — spec "Errors and
    // failure handling" says run D6's teardown immediately rather than
    // waiting for this tab's own timer, which matters for a laptop that
    // slept through the deadline and never ran its own onExpire at all.
    it("ends this browser's session when the activity endpoint answers 401", async () => {
      const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => {
        const url = typeof input === "string" ? input : input.toString();
        if (url.includes("/auth/session/activity")) {
          return Promise.resolve({
            ok: false,
            status: 401,
            json: async () => ({ error: "session_idle", message: "session expired" }),
          });
        }
        if (url === "/logout") {
          return Promise.resolve({ ok: true, status: 200 });
        }
        return Promise.resolve({ ok: true, status: 200, json: async () => ({ data: [] }) });
      });
      vi.stubGlobal("fetch", fetchMock);
      renderWithProviders(<HmsShell active="/">content</HmsShell>);

      await waitFor(() => expect(fetchMock).toHaveBeenCalledWith("/logout", { method: "POST" }));
      await waitFor(() => expect(endZitadelSession).toHaveBeenCalledWith(IDLE_ENDED_MARK));
      expect(endZitadelSession).toHaveBeenCalledTimes(1);
    });

    // Important review finding: tearing down is correct for ANY 401 (the
    // session really is gone), but the WORDING is not — a 401 caused by
    // #781's revocation watermark or an ordinary lapsed `exp` is not
    // inactivity, and claiming it is sends the clinician looking for the
    // wrong explanation. Only `ApiError.code === "session_idle"` may set
    // IDLE_ENDED_MARK; endZitadelSession must still run (this-browser
    // teardown is still correct), just with no mark, so /login falls back
    // to its neutral greeting rather than either specific claim.
    it("tears down on a differently-coded 401 but does not claim inactivity", async () => {
      const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => {
        const url = typeof input === "string" ? input : input.toString();
        if (url.includes("/auth/session/activity")) {
          return Promise.resolve({
            ok: false,
            status: 401,
            json: async () => ({ error: "revoked", message: "session revoked" }),
          });
        }
        if (url === "/logout") {
          return Promise.resolve({ ok: true, status: 200 });
        }
        return Promise.resolve({ ok: true, status: 200, json: async () => ({ data: [] }) });
      });
      vi.stubGlobal("fetch", fetchMock);
      renderWithProviders(<HmsShell active="/">content</HmsShell>);

      await waitFor(() => expect(fetchMock).toHaveBeenCalledWith("/logout", { method: "POST" }));
      // Teardown ran (endZitadelSession was called), but with no mark —
      // neither wording is true here.
      await waitFor(() => expect(endZitadelSession).toHaveBeenCalledWith(undefined));
      expect(endZitadelSession).toHaveBeenCalledTimes(1);
    });

    // A transient failure (network error, 5xx, rate limit) must NEVER
    // trigger teardown — only a 401 does. This is the discriminating
    // assertion for the `err.status === 401` check: a mutation that
    // dropped the status guard (teardown on ANY activity failure) would
    // pass every other test in this file but fail this one, since a 500
    // here must leave the session alone.
    it("does not tear down the session on a non-401 activity failure", async () => {
      const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => {
        const url = typeof input === "string" ? input : input.toString();
        if (url.includes("/auth/session/activity")) {
          return Promise.resolve({
            ok: false,
            status: 500,
            json: async () => ({ error: "internal", message: "server error" }),
          });
        }
        return Promise.resolve({ ok: true, status: 200, json: async () => ({ data: [] }) });
      });
      vi.stubGlobal("fetch", fetchMock);
      renderWithProviders(<HmsShell active="/">content</HmsShell>);

      await waitFor(() =>
        expect(fetchMock).toHaveBeenCalledWith(
          "/api/v1/auth/session/activity",
          expect.objectContaining({ method: "POST" }),
        ),
      );
      await new Promise((resolve) => setTimeout(resolve, 0));
      expect(fetchMock).not.toHaveBeenCalledWith("/logout", { method: "POST" });
      expect(endZitadelSession).not.toHaveBeenCalled();
      expect(window.sessionStorage.getItem(IDLE_ENDED_MARK)).toBeNull();
    });
  });
});
