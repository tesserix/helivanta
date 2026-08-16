"use client";

import { useCallback, useEffect, useRef, useState, type FormEvent, type ReactNode } from "react";
import { LogOut, PanelLeftClose, PanelLeftOpen } from "lucide-react";
import {
  ApiError,
  apiFetch,
  usePermissions,
  useApiMutation,
  clearPermissionsCache,
} from "@hms/api";
import { visibleZones, activeZone } from "./zones";
import { ThemeToggle } from "./theme";
import { endZitadelSession } from "./zitadel-session";
import { createIdleTracker, WARNING_LEAD_MS, type IdleTracker } from "./idle-timer";
import { IdleWarning } from "./idle-warning";

// Two-rail chrome in the tesserix-home AdminSidebar style. The zone rail is
// fixed icons-only (no expand/collapse); only the page panel toggles open ↔
// hidden, and that preference persists per browser. All links stay plain
// <a> — cross-zone navigation is a hard navigation by design (phase 1 spec
// D3). Sign-out is the one documented exception (docs/standards/frontend.md
// §6): it is a state change, not navigation, so it renders as a form.

const PANEL_KEY = "hms.panel.open";

function usePersistedFlag(key: string, fallback: boolean) {
  const [value, setValue] = useState(fallback);
  useEffect(() => {
    const stored = window.localStorage.getItem(key);
    if (stored !== null) setValue(stored === "1");
  }, [key]);
  const update = (next: boolean) => {
    setValue(next);
    window.localStorage.setItem(key, next ? "1" : "0");
  };
  return [value, update] as const;
}

/**
 * `tenantPicker` is a slot rendered in the content header rather than a
 * component this package owns. Switching hospitals re-mints the HMS
 * session server-side (`POST /v1/iam/me/tenant`, design spec D3) — no
 * client-side token exchange is needed any more, but the call still
 * belongs to the shell app (`apps/shell/components/tenant-picker.tsx`),
 * consistent with `onSignOut` below. Keeping the control here would drag
 * app-specific plumbing into `@hms/ui` for no benefit. Zone apps pass
 * nothing and show no switcher; their users switch from the dashboard.
 */
export function HmsShell({
  active,
  tenantPicker,
  onSignOut,
  children,
}: {
  active: string;
  tenantPicker?: ReactNode;
  /**
   * An override for the ENTIRE sign-out sequence, for a caller that needs
   * something this package's own default cannot express. Not needed for
   * ordinary Zitadel-ending sign-out any more — see the default path's
   * comment below for why that turned out to be true everywhere, not only
   * in the shell app.
   *
   * A caller-supplied `onSignOut` owns navigation end to end and must not
   * return normally on success if it navigates: `handleSignOut` below
   * does not navigate after it, specifically so a redirect it starts
   * (e.g. through an IdP's own logout) is never raced by a second,
   * competing navigation to `/login`.
   */
  onSignOut?: () => void | Promise<void>;
  children: ReactNode;
}) {
  const { can } = usePermissions();
  const zones = visibleZones(can);
  // Fall back to the dashboard (always present in `zones`) if the active
  // zone was filtered out — e.g. while permissions are still loading.
  const zone = zones.find((z) => z.key === activeZone(active).key) ?? zones[0];
  const [panelOpen, setPanelOpen] = usePersistedFlag(PANEL_KEY, true);

  // Idle-session tracking (design spec D2/D4/D7). Mounted here, not in
  // apps/shell, for the exact reason endZitadelSession's doc comment
  // records for sign-out: every app that renders HmsShell — medicore,
  // pharmacy, lab, not only the shell's own dashboard — must count as
  // "active" while a clinician works in it, or they get signed out
  // mid-consultation while the feature looks implemented.
  //
  // onWarn now opens the IdleWarning modal (D5, #848 task 6 — see
  // reportActivity/handleStayLoggedIn and the tracker effect below).
  // onExpire is still a deliberate no-op — this-browser teardown (D6) is
  // task 7. Until it lands, the server-side idle_deadline (already
  // fail-closed in authn.Middleware) remains the backstop for expiry.
  const activity = useApiMutation<{ idle_deadline: string }>(
    () => apiFetch<{ idle_deadline: string }>("/auth/session/activity", { method: "POST" }),
    // A failed activity call must NEVER sign the user out — the
    // server-side deadline is the backstop, and the next qualifying
    // interaction retries. A toast on every dropped background ping
    // would just be alarming noise on top of behaviour that is already
    // correct, so it is suppressed rather than surfaced.
    { suppressErrorToast: true },
  );
  // A ref, not a plain closure over `activity.mutateAsync`, because the
  // tracker below is created exactly once (empty dep array — see its own
  // comment for why) and must still call the LATEST mutation function
  // rather than whichever one existed at mount. Assigned in an effect,
  // not during render, so this component never mutates a ref as a render
  // side effect.
  const activityRef = useRef(activity.mutateAsync);
  useEffect(() => {
    activityRef.current = activity.mutateAsync;
  }, [activity.mutateAsync]);

  const trackerRef = useRef<IdleTracker | null>(null);
  // The deadline the warning modal is showing. `null` means no modal.
  // Always set FROM onWarn's own parameter or onDeadlineChange's own
  // parameter below — never mirrored into a second ref of our own first
  // (review finding: a component-owned copy, written only from this
  // tab's OWN activity responses, went stale the moment a DIFFERENT tab
  // extended the session, and the modal rendered a countdown to an
  // already-superseded deadline instead of the one it was actually
  // scheduled against).
  const [warningDeadline, setWarningDeadline] = useState<Date | null>(null);

  // Reports genuine interaction to the server and folds the response back
  // into the tracker (design spec D4's "response body returns the new
  // deadline"). Used both as the tracker's onActivity handler AND, once
  // below, called directly on mount and from the warning modal's "Stay
  // signed in" action — see those call sites' comments for why.
  const reportActivity = useCallback(() => {
    void activityRef
      .current()
      .then((data) => {
        trackerRef.current?.noteDeadline(new Date(data.idle_deadline));
      })
      .catch((err: unknown) => {
        // A 401 here is NOT a transient failure — it means the
        // session is already idle-expired or gone server-side (spec
        // "Errors and failure handling": "the session is already
        // gone. Run D6's teardown immediately rather than waiting
        // for a timer"). This is the seam task 7's teardown
        // (POST /logout + endZitadelSession) hooks into; until then
        // it deliberately does nothing beyond not-retrying, same as
        // every other failure here (network, 5xx, rate limit),
        // which ARE transient and must never sign anyone out — see
        // the comment on `activity` above.
        if (err instanceof ApiError && err.status === 401) {
          // TODO(#848 task 7): trigger D6 teardown here.
        }
      });
  }, []);

  useEffect(() => {
    const tracker = createIdleTracker({
      onActivity: reportActivity,
      // D5: "Before showing it, the client re-checks the deadline it
      // holds. Another tab may have extended the session, in which case
      // the modal must not appear at all." Guaranteed by
      // createIdleTracker itself: noteDeadline (called here, from a
      // broadcast, or from this tab's own activity) cancels and
      // reschedules the pending onWarn timer the moment a LATER deadline
      // is known, so onWarn simply never fires for a deadline that has
      // already been superseded (idle-timer.ts, exercised in
      // idle-timer.test.ts and again end to end in hms-shell.test.tsx
      // below). `deadline` here is the tracker's OWN authoritative value
      // for the warning that just fired, not a second copy this
      // component keeps — see warningDeadline's doc comment above.
      onWarn: (deadline) => setWarningDeadline(deadline),
      onExpire: () => {},
      // D5's "or any interaction … the modal closes" (review finding):
      // the deadline can move forward from THIS tab's own activity (a
      // keydown/scroll fired while the modal happens to be open) or from
      // ANOTHER tab's broadcast, and either must clear a currently-open
      // warning — a modal frozen at 0:00 over a session that is actually
      // fine teaches clinicians to ignore it, which is worse than never
      // warning them. If the new deadline is still within the warning
      // window (rare — an extension shorter than WARNING_LEAD_MS), the
      // modal stays open but re-pins to the fresh value instead of
      // silently going stale itself.
      onDeadlineChange: (deadline) => {
        setWarningDeadline((current) => {
          if (!current) return current;
          const stillWithinWarningWindow = deadline.getTime() - WARNING_LEAD_MS <= Date.now();
          return stillWithinWarningWindow ? deadline : null;
        });
      },
    });
    trackerRef.current = tracker;
    tracker.start();

    // Seed an initial idle_deadline (gap this task closes — see the NOTE
    // this replaced). The tracker only ever learns a deadline from a
    // SUCCESSFUL activity response, so without this call a terminal that
    // is loaded and never touched would have no client-side schedule at
    // all — no warning modal, ever — until the server's own eventual 401
    // refusal. Reporting once on mount treats navigating to a page as the
    // interaction it is: a clinician who opened this screen is not an
    // unattended terminal, so this is a legitimate use of the SAME
    // activity endpoint every qualifying interaction already calls, not
    // a workaround for D2/D4's "only an explicit activity call moves the
    // deadline" rule.
    //
    // markActivity() FIRST, synchronously: it syncs the tracker's own
    // debounce clock to "now" without itself calling onActivity, so a
    // qualifying DOM event firing moments after mount (a clinician who
    // starts typing or scrolling right away) is correctly debounced
    // against this seed call instead of firing its own, redundant POST
    // against the activity endpoint's narrowly-budgeted `Tight`
    // rate-limit bucket (review finding).
    tracker.markActivity();
    reportActivity();

    return () => {
      tracker.stop();
      trackerRef.current = null;
    };
  }, [reportActivity]);

  // The warning modal's countdown re-derives seconds-remaining from the
  // deadline on every tick (spec "Errors and failure handling": "the
  // client's countdown is a display derived from the server's returned
  // deadline, never its own arithmetic on a locally-stored timestamp") —
  // this recomputes from `Date.now()` each second rather than
  // decrementing a counter, so clock drift or a paused/backgrounded tab
  // can never desync the displayed time from the real deadline.
  const [secondsRemaining, setSecondsRemaining] = useState<number | null>(null);
  useEffect(() => {
    if (!warningDeadline) {
      setSecondsRemaining(null);
      return;
    }
    const tick = () => {
      const remainingMs = warningDeadline.getTime() - Date.now();
      setSecondsRemaining(Math.max(0, Math.round(remainingMs / 1000)));
    };
    tick();
    const interval = setInterval(tick, 1000);
    return () => clearInterval(interval);
  }, [warningDeadline]);

  // "Stay signed in" — and any other dismissal of the modal, per
  // IdleWarning's own onStay contract — reports activity exactly like a
  // qualifying interaction would, extending the session rather than
  // merely hiding the warning (item 6: hiding without extending would
  // leave the clinician believing they are safe while the deadline keeps
  // counting down underneath them).
  function handleStayLoggedIn() {
    setWarningDeadline(null);
    reportActivity();
  }

  // Sign-out is a state change — it revokes every session for the
  // subject server-side (#781) — so it is a POST, same-origin checked by
  // apps/shell/app/logout/route.ts, never a GET a page could trigger with
  // `<img src="/logout">`. The cache drop happens before the network
  // round-trip so a slow or failed revoke can never leave stale
  // permissions visible.
  //
  // The default path ends BOTH sessions — the HMS one (POST /logout) and
  // Zitadel's own SSO cookie (zitadel-session.ts's endZitadelSession) —
  // from EVERY app, not only the shell's dashboard. That used to be
  // shell-only, on the same "only the shell carries Zitadel client
  // config" reasoning `tenantPicker` still uses — and it was wrong for
  // sign-out specifically: signing out from a zone page hit only the POST
  // below, leaving Zitadel's SSO session alive, so the very next login on
  // that browser (even for a DIFFERENT identity) completed silently with
  // no credential prompt. Found live driving the real e2e suite, not
  // theorised — see zitadel-session.ts's doc comment for the full trace.
  //
  // Navigation only happens in the `else` branch. A caller-supplied
  // `onSignOut` (see its doc comment) owns navigation itself, and
  // endZitadelSession() ALSO owns navigation on its own success path
  // (signoutRedirect() is a real cross-origin redirect) — forcing
  // `/login` after either would cancel an in-flight navigation and
  // replace it with a same-origin one, undoing the whole point.
  // endZitadelSession() already falls back to a same-origin `/login`
  // redirect itself when it can't reach Zitadel, so there is nothing left
  // for this function to do in either case.
  async function handleSignOut(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    clearPermissionsCache();
    if (onSignOut) {
      await onSignOut();
      return;
    }
    try {
      await fetch("/logout", { method: "POST" });
    } catch {
      // The user must still be able to leave a shared workstation even
      // if the revoke call itself is unreachable.
    }
    await endZitadelSession();
  }

  const activePage = zone.pages.find((p) => p.href === active);

  return (
    <>
      {warningDeadline && secondsRemaining !== null && (
        <IdleWarning secondsRemaining={secondsRemaining} onStay={handleStayLoggedIn} />
      )}
      <div className="flex h-screen overflow-hidden">
        {/* Zone rail: fixed icons-only. Matches tesserix admin: one step
          darker than the page panel, faint border between the rails, none
          against the content. */}
        <aside className="hms-sidebar-border flex w-16 shrink-0 flex-col border-r bg-(--sidebar-rail)">
          <div className="flex h-16 items-center justify-center">
            <a
              href="/"
              aria-label="HMS home"
              className="flex h-9 items-center gap-2 rounded-lg text-lg font-semibold text-sidebar-primary"
            >
              <span className="flex h-9 w-9 items-center justify-center rounded-lg bg-linear-to-br from-(--hms-accent) to-(--hms-accent-strong) text-(--sidebar-primary-foreground)">
                H
              </span>
            </a>
          </div>
          <div className="hms-sidebar-border mx-2 border-t" />
          <nav
            aria-label="Zones"
            className="flex min-h-0 flex-1 flex-col items-center gap-1 overflow-y-auto px-0 py-4"
          >
            {zones.map((z) => {
              const isActive = z.key === zone.key;
              return (
                <a
                  key={z.key}
                  href={z.href}
                  title={z.label}
                  aria-label={z.label}
                  aria-current={isActive ? "page" : undefined}
                  className={`relative flex h-10 w-10 items-center justify-center rounded-lg transition-colors ${
                    isActive
                      ? "bg-(--hms-accent-dim) text-(--hms-accent)"
                      : "text-sidebar-foreground/70 hover:bg-sidebar-accent/50 hover:text-sidebar-foreground"
                  }`}
                >
                  {isActive && (
                    <span
                      aria-hidden="true"
                      className="absolute inset-y-1 left-0 w-[3px] rounded-full bg-(--hms-accent)"
                    />
                  )}
                  <z.icon className="h-5 w-5 shrink-0" aria-hidden="true" />
                </a>
              );
            })}
          </nav>
          <div className="flex flex-col items-center gap-1 px-0 py-3">
            <form onSubmit={handleSignOut}>
              <button
                type="submit"
                title="Sign out"
                aria-label="Sign out"
                className="flex h-10 w-10 items-center justify-center rounded-lg text-sidebar-foreground/70 transition-colors hover:bg-sidebar-accent/50 hover:text-sidebar-foreground"
              >
                <LogOut className="h-4 w-4 shrink-0" aria-hidden="true" />
              </button>
            </form>
          </div>
        </aside>

        {/* Page panel: open or hidden */}
        <aside
          className={`flex shrink-0 flex-col overflow-hidden bg-sidebar transition-[width] duration-200 ease-out motion-reduce:transition-none ${
            panelOpen ? "w-56" : "w-0"
          }`}
          aria-hidden={!panelOpen}
        >
          <div className="flex h-16 w-56 items-center justify-between pl-5 pr-3">
            <h2 className="text-sm font-semibold text-sidebar-foreground">{zone.label}</h2>
            <button
              type="button"
              onClick={() => setPanelOpen(false)}
              aria-label="Collapse page panel"
              title="Collapse panel"
              tabIndex={panelOpen ? 0 : -1}
              className="flex h-8 w-8 items-center justify-center rounded-lg text-sidebar-foreground/70 transition-colors hover:bg-sidebar-accent/50 hover:text-sidebar-foreground"
            >
              <PanelLeftClose className="h-4 w-4" aria-hidden="true" />
            </button>
          </div>
          <div className="hms-sidebar-border border-t" />
          <nav
            aria-label={zone.label}
            className="flex w-56 min-h-0 flex-1 flex-col gap-1 overflow-y-auto px-3 py-4"
          >
            {zone.pages.map((pageLink) => {
              const isActive = active === pageLink.href;
              return (
                <a
                  key={pageLink.href}
                  href={pageLink.href}
                  aria-current={isActive ? "page" : undefined}
                  tabIndex={panelOpen ? 0 : -1}
                  className={`flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors ${
                    isActive
                      ? "bg-sidebar-accent text-sidebar-foreground"
                      : "text-sidebar-foreground/70 hover:bg-sidebar-accent/50 hover:text-sidebar-foreground"
                  }`}
                >
                  <pageLink.icon className="h-4 w-4 shrink-0" aria-hidden="true" />
                  {pageLink.label}
                </a>
              );
            })}
          </nav>
        </aside>

        {/* Content */}
        <div className="flex min-w-0 flex-1 flex-col overflow-y-auto">
          <header className="sticky top-0 z-30 border-b bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/60">
            <div className="flex h-16 items-center justify-between px-6">
              <div className="flex items-center gap-2">
                {!panelOpen && (
                  <button
                    type="button"
                    onClick={() => setPanelOpen(true)}
                    aria-label="Expand page panel"
                    title="Expand panel"
                    className="mr-1 flex h-8 w-8 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                  >
                    <PanelLeftOpen className="h-4 w-4" aria-hidden="true" />
                  </button>
                )}
                <h1 className="text-xl font-semibold text-foreground">
                  {activePage?.label ?? zone.label}
                </h1>
              </div>
              <div className="flex items-center gap-3">
                {tenantPicker}
                <ThemeToggle />
                <form onSubmit={handleSignOut}>
                  <button
                    type="submit"
                    className="text-sm text-muted-foreground underline-offset-4 transition-colors hover:text-foreground hover:underline"
                  >
                    Sign out
                  </button>
                </form>
              </div>
            </div>
          </header>
          <main className="flex-1 px-6 py-6">
            <div className="mx-auto w-full max-w-6xl">{children}</div>
          </main>
        </div>
      </div>
    </>
  );
}
