import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import { renderWithProviders } from "@hms/api/testing";
import { PERMISSIONS_CACHE_KEY } from "@hms/api";
import { HmsShell } from "./hms-shell";
import { WARNING_LEAD_MS, type IdleTracker } from "./idle-timer";

const endZitadelSession = vi.hoisted(() => vi.fn());
vi.mock("./zitadel-session", () => ({ endZitadelSession }));

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
  // revoke the HMS session server-side AND end Zitadel's own SSO session
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
    await waitFor(() => expect(endZitadelSession).toHaveBeenCalledTimes(1));
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
      const extendedDeadline = new Date(Date.now() + 20 * 60_000);
      act(() => {
        createdTrackers[0]?.noteDeadline(extendedDeadline);
      });

      // Advance well past where the ORIGINAL deadline's warning would
      // have fired.
      await act(async () => {
        await vi.advanceTimersByTimeAsync(10_000);
      });

      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    });
  });
});
