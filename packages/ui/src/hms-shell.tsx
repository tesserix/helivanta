"use client";

import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  Fragment,
  useSyncExternalStore,
  type FormEvent,
  type ReactNode,
} from "react";
import { usePathname } from "next/navigation";
import { ChevronsLeft, ChevronsRight, Menu, X } from "lucide-react";
import {
  ApiError,
  useApiQuery,
  brandingSchema,
  DEFAULT_BRANDING,
  apiFetch,
  usePermissions,
  useApiMutation,
  clearPermissionsCache,
  clearRenewAt,
  notifySessionChanged,
} from "@helivanta/api";
import { GuardedLink, NavigationGuardProvider } from "./navigation-guard";
import { ZONES, visibleZones, activeZone, activePage, railName, type Zone } from "./zones";
import { BrandLogo, TesserixFooter } from "./branding";
import { BrandThemeContext } from "./brand-theme";
import {
  endZitadelSession,
  IDLE_ENDED_MARK,
  SIGNED_OUT_MARK,
  type SessionEndMark,
} from "./zitadel-session";
import { createIdleTracker, WARNING_LEAD_MS, type IdleTracker } from "./idle-timer";
import { IdleWarning } from "./idle-warning";
import { AccountMenu } from "./account-menu";
import { CHROME_FLAG_ATTRIBUTES, NAVIGATION_KEY, flagAttributeValue } from "./chrome-flags";

// Navigation stays within the unified app. Sign-out remains a POST because
// it revokes server-side credentials.

// Same-tab writes fire no `storage` event, and unwritable storage keeps the flag in memory.
const flagListeners = new Set<() => void>();
const unpersistedFlags = new Map<string, boolean>();

function subscribeFlags(listener: () => void) {
  flagListeners.add(listener);
  window.addEventListener("storage", listener);
  return () => {
    flagListeners.delete(listener);
    window.removeEventListener("storage", listener);
  };
}

function readFlag(key: string, fallback: boolean) {
  const unpersisted = unpersistedFlags.get(key);
  if (unpersisted !== undefined) return unpersisted;
  try {
    const stored = window.localStorage.getItem(key);
    return stored === null ? fallback : stored === "1";
  } catch {
    return fallback;
  }
}

// The server snapshot is the fallback, so hydration matches and stored values apply right after.
function usePersistedFlag(key: string, fallback: boolean) {
  const value = useSyncExternalStore(
    subscribeFlags,
    () => readFlag(key, fallback),
    () => fallback,
  );
  // Mirrored to <html> for styles.css (#1091 D7), from every source: this tab,
  // another tab, unwritable storage. A layout effect, so the attribute and the
  // classes change in the same paint.
  useLayoutEffect(() => {
    document.documentElement.dataset[CHROME_FLAG_ATTRIBUTES[key]] = flagAttributeValue(value);
  }, [key, value]);
  const update = (next: boolean) => {
    try {
      window.localStorage.setItem(key, next ? "1" : "0");
      unpersistedFlags.delete(key);
    } catch {
      unpersistedFlags.set(key, next);
    }
    flagListeners.forEach((listener) => listener());
  };
  return [value, update] as const;
}

function ZoneLink({
  zone,
  active,
  expanded,
  onNavigate,
}: {
  zone: Zone;
  active: boolean;
  expanded: boolean;
  onNavigate: () => void;
}) {
  return (
    <GuardedLink
      href={zone.href}
      onClick={onNavigate}
      title={zone.label}
      aria-label={railName(zone)}
      aria-current={active ? "page" : undefined}
      className={`flex items-center rounded-lg transition-colors ${expanded ? "h-11 w-full gap-3 px-3" : "h-11 w-11 justify-center md:h-auto md:w-16 md:flex-col md:gap-1 md:py-2"} ${
        active
          ? "bg-(--hms-accent-dim) text-(--hms-accent)"
          : "text-sidebar-foreground/70 hover:bg-sidebar-accent/50 hover:text-sidebar-foreground"
      }`}
    >
      <zone.icon className="h-5 w-5 shrink-0" aria-hidden="true" />
      {expanded ? (
        <span className="truncate text-sm font-medium">{zone.label}</span>
      ) : (
        <span className="hidden max-w-full truncate text-[11px] leading-none font-medium md:block">
          {zone.railLabel}
        </span>
      )}
    </GuardedLink>
  );
}

// The phone menu (#1299): every zone, grouped as on the rail, with the
// current zone's pages under their section headings. A modal drawer: focus
// moves in on open and back to the menu button on Escape or close; choosing a
// link closes it and lets the navigation carry focus.
function PhoneMenu({
  open,
  onClose,
  zones,
  zone,
  currentHref,
  isLoading,
  returnFocusTo,
}: {
  open: boolean;
  onClose: () => void;
  zones: Zone[];
  zone: Zone;
  currentHref?: string;
  isLoading: boolean;
  returnFocusTo: React.RefObject<HTMLButtonElement | null>;
}) {
  const closeRef = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    if (!open) return;
    closeRef.current?.focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      onClose();
      returnFocusTo.current?.focus();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open, onClose, returnFocusTo]);
  if (!open) return null;
  const dismiss = () => {
    onClose();
    returnFocusTo.current?.focus();
  };
  const main = zones.filter((z) => z.group !== "admin");
  const admin = zones.filter((z) => z.group === "admin");
  const zoneLink = (z: Zone) => {
    const active = z.key === zone.key;
    return (
      <Fragment key={z.key}>
        <GuardedLink
          href={z.href}
          onClick={onClose}
          aria-current={active ? "page" : undefined}
          className={`flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors ${
            active
              ? "bg-(--hms-accent-dim) text-(--hms-accent)"
              : "text-sidebar-foreground/70 hover:bg-sidebar-accent/50 hover:text-sidebar-foreground"
          }`}
        >
          <z.icon className="h-5 w-5 shrink-0" aria-hidden="true" />
          {z.label}
        </GuardedLink>
        {active && z.pages.length > 1 && (
          <div className="hms-sidebar-border mb-2 ml-5 border-l pl-3">
            {z.pages.map((pageLink, index) => {
              const heading =
                pageLink.section && pageLink.section !== z.pages[index - 1]?.section
                  ? pageLink.section
                  : null;
              return (
                <Fragment key={pageLink.href}>
                  {heading && (
                    <p className="px-3 pt-3 pb-1 text-xs font-medium text-sidebar-foreground/60">
                      {heading}
                    </p>
                  )}
                  <GuardedLink
                    href={pageLink.href}
                    onClick={onClose}
                    aria-current={pageLink.href === currentHref ? "page" : undefined}
                    className={`flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors ${
                      pageLink.href === currentHref
                        ? "bg-sidebar-accent text-sidebar-foreground"
                        : "text-sidebar-foreground/70 hover:bg-sidebar-accent/50 hover:text-sidebar-foreground"
                    }`}
                  >
                    <pageLink.icon className="h-4 w-4 shrink-0" aria-hidden="true" />
                    {pageLink.label}
                  </GuardedLink>
                </Fragment>
              );
            })}
          </div>
        )}
      </Fragment>
    );
  };
  return (
    <div className="md:hidden">
      <button
        type="button"
        tabIndex={-1}
        aria-hidden="true"
        onClick={dismiss}
        className="fixed inset-0 z-40 bg-foreground/20"
      />
      <div
        id="hms-phone-menu"
        role="dialog"
        aria-modal="true"
        aria-label="Menu"
        className="fixed inset-y-0 left-0 z-50 flex w-72 max-w-[85vw] flex-col bg-(--sidebar-rail) shadow-xl"
      >
        <div className="hms-sidebar-border flex h-16 shrink-0 items-center justify-end border-b px-3">
          <button
            ref={closeRef}
            type="button"
            onClick={dismiss}
            aria-label="Close menu"
            className="flex h-9 w-9 items-center justify-center rounded-lg text-sidebar-foreground/70 hover:bg-sidebar-accent/50"
          >
            <X className="h-5 w-5" aria-hidden="true" />
          </button>
        </div>
        <nav aria-label="Menu" className="flex min-h-0 flex-1 flex-col gap-1 overflow-y-auto p-3">
          {isLoading
            ? ZONES.map((z) => <NavPlaceholder key={z.key} wide />)
            : main.map((z, index) => (
                <Fragment key={z.key}>
                  {index > 0 && main[index - 1].group !== z.group && (
                    <div aria-hidden="true" className="hms-sidebar-border my-1 border-t" />
                  )}
                  {zoneLink(z)}
                </Fragment>
              ))}
          {!isLoading && admin.length > 0 && (
            <>
              <div aria-hidden="true" className="hms-sidebar-border my-1 border-t" />
              {admin.map(zoneLink)}
            </>
          )}
        </nav>
      </div>
    </div>
  );
}

// A neutral slot holding a link's place while permissions load (#1270 D2).
function NavPlaceholder({ wide }: { wide: boolean }) {
  return (
    <div
      data-testid="nav-placeholder"
      aria-hidden="true"
      className={`shrink-0 animate-pulse rounded-lg bg-sidebar-accent/40 motion-reduce:animate-none ${wide ? "h-11 w-full" : "h-11 w-11 md:h-[3.25rem] md:w-16"}`}
    />
  );
}

// Seconds are derived from the server's deadline on every tick, never decremented locally.
function IdleCountdown({ deadline, onStay }: { deadline: Date; onStay: () => void }) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const interval = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(interval);
  }, []);
  const secondsRemaining = Math.max(0, Math.round((deadline.getTime() - now) / 1000));
  return <IdleWarning secondsRemaining={secondsRemaining} onStay={onStay} />;
}

/**
 * `tenantPicker` is a slot rendered in the content header rather than a
 * component this package owns. Switching hospitals re-mints the Helivanta
 * session server-side (`POST /v1/iam/me/tenant`, design spec D3) — no
 * client-side token exchange is needed any more, but the call still
 * belongs to the shell app (`apps/shell/components/tenant-picker.tsx`),
 * consistent with `onSignOut` below. Keeping the control here would drag
 * app-specific plumbing into `@helivanta/ui` for no benefit. The workspace
 * layout (`apps/shell/app/(workspace)/layout.tsx`) always passes it, so it is on
 * every authenticated page; it renders nothing below two memberships.
 */
export function HmsShell({
  tenantPicker,
  onSignOut,
  children,
}: {
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
  // Read here, not passed per page: one layout renders the shell for every
  // authenticated route (#1091), so the path is the only source of "where".
  const pathname = usePathname();
  const { can, isLoading } = usePermissions();
  const brandQuery = useApiQuery(["workspace", "branding"], "/workspace/branding");
  const parsedBrand = brandingSchema.safeParse(brandQuery.data);
  const branding = parsedBrand.success ? parsedBrand.data : DEFAULT_BRANDING;
  const zones = visibleZones(can);
  // While permissions load, the zone comes from the route and the navigation
  // renders placeholders, never links: a partial list read as items appearing
  // at random, and showing a link before permissions are known fails open
  // (#1270 D2). Once loaded, a zone the user cannot see falls back to the
  // dashboard, which is always present in `zones`.
  const zone = isLoading
    ? activeZone(pathname)
    : (zones.find((z) => z.key === activeZone(pathname).key) ?? zones[0]);
  const [navigationOpen, setNavigationOpen] = usePersistedFlag(NAVIGATION_KEY, false);
  // Below md the rail and page panel are not shown; one header menu holds
  // both (#1299). From md the panel is always shown (#1286 D4).
  const [menuOpen, setMenuOpen] = useState(false);
  const menuButtonRef = useRef<HTMLButtonElement>(null);
  const closeOverlays = () => setMenuOpen(false);

  // Idle-session tracking (design spec D2/D4/D7). Mounted here, not in
  // apps/shell, for the exact reason endZitadelSession's doc comment
  // records for sign-out: every app that renders HmsShell — medicore,
  // pharmacy, lab, not only the shell's own dashboard — must count as
  // "active" while a clinician works in it, or they get signed out
  // mid-consultation while the feature looks implemented.
  //
  // onWarn opens the IdleWarning modal (D5, #848 task 6 — see
  // reportActivity/handleStayLoggedIn and the tracker effect below).
  // onExpire runs endIdleSession() (D6, #848 task 7 — see that function's
  // own comment), and the server-side idle_deadline (already fail-closed
  // in authn.Middleware) remains the backstop regardless, for a browser
  // that never got the chance to run its own timer at all.
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

  // D6 teardown — the ONE thing that ends this browser's session on
  // expiry OR on a session-gone 401. Two triggers reach this same
  // function: the client's own onExpire timer (below) and ANY 401 from
  // the activity endpoint (reportActivity's catch, further down) — a 401
  // there always means the session is gone server-side, whether that is
  // idle expiry, #781's revocation watermark, or a lapsed `exp`, so
  // teardown always runs. `mark` is separate from "should teardown run at
  // all": it is ONLY about which wording `/login` shows afterwards, and
  // is `IDLE_ENDED_MARK` for onExpire (a client timer reaching the
  // deadline is definitionally idle) and for a 401 whose `ApiError.code`
  // is specifically `"session_idle"`, `undefined` for any other 401 code
  // (review finding: tearing down is correct for those too, but claiming
  // inactivity when the real cause was a revoked membership or an
  // ordinary token lapse is its own false claim, so those get /login's
  // neutral wording instead — see reportActivity's catch for the code
  // check).
  //
  // Deliberately NOT subject-wide revocation (that is sign-out's #781
  // semantics, driven through handleSignOut below): ending every device's
  // session because ONE ward terminal sat idle would sign a clinician out
  // of their office desktop and phone too.
  //
  // tornDownRef guards against running this twice — onExpire's timer and a
  // 401 from a THEN-in-flight activity call can both fire once the
  // deadline has passed, and endZitadelSession() must only ever be asked
  // to navigate once.
  const tornDownRef = useRef(false);
  const endIdleSession = useCallback((mark: SessionEndMark | undefined) => {
    if (tornDownRef.current) return;
    tornDownRef.current = true;
    // Same reasoning as handleSignOut's calls below: an idle timeout ends
    // this session too, so both the renewal schedule and the cached
    // permission set it left behind go with it. The permissions cache is
    // convenience only (the API enforces — see CLAUDE.md's frontend
    // rules), so a stale entry here is not a privilege escalation, but it
    // lives in localStorage rather than sessionStorage (permissions-cache.ts),
    // so without this call the previous clinician's cached permissions
    // would survive an idle timeout on a shared ward terminal — exactly
    // the leak #848's idle timeout exists to prevent.
    clearPermissionsCache();
    clearRenewAt();
    notifySessionChanged();
    void (async () => {
      try {
        await fetch("/logout", { method: "POST" });
      } catch {
        // Same reasoning as handleSignOut below: the browser must still
        // end up on a signed-out-looking page even if the Helivanta session
        // revoke call itself is unreachable.
      }
      // endZitadelSession() itself does the sessionStorage.setItem for
      // `mark`, immediately before it navigates — see that function's own
      // doc comment for why it, and only it, may ever set one of these
      // marks (review finding: two call sites each stamping their own
      // mark is what previously left BOTH marks in sessionStorage at
      // once).
      await endZitadelSession(mark);
    })();
  }, []);

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
        // session is already gone server-side (spec "Errors and failure
        // handling": "the session is already gone. Run D6's teardown
        // immediately rather than waiting for a timer"). Every OTHER
        // failure here (network, 5xx, rate limit) IS transient and must
        // never sign anyone out — see the comment on `activity` above —
        // so only this one status code triggers teardown.
        //
        // The WORDING is a separate decision from whether to tear down
        // (review finding): only `err.code === "session_idle"` means
        // inactivity actually caused this — the binding constraint (spec
        // D6/authn.Middleware) names that code specifically. A 401 from
        // #781's revocation watermark or an ordinary lapsed `exp` still
        // ends the session (it IS gone), but /login must not claim
        // inactivity did it, so those pass `undefined` and get the
        // neutral greeting instead.
        if (err instanceof ApiError && err.status === 401) {
          endIdleSession(err.code === "session_idle" ? IDLE_ENDED_MARK : undefined);
        }
      });
  }, [endIdleSession]);

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
      // D6: this tab's own clock reaching the deadline runs the SAME
      // teardown a server-side 401 (reportActivity's catch, above) does —
      // see endIdleSession's own comment for why both triggers exist and
      // why running it twice is guarded against there, not here. Always
      // IDLE_ENDED_MARK: a client timer reaching the deadline IS idle
      // expiry, unlike a 401 whose cause could be something else.
      onExpire: () => endIdleSession(IDLE_ENDED_MARK),
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
  }, [reportActivity, endIdleSession]);

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
  // The default path ends BOTH sessions — the Helivanta one (POST /logout) and
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
    // #781's rule is that a signed-out session must not be reconstructable
    // from anything the browser kept. The stored renewal schedule (#916
    // Task 4's F3) is not a credential and carries no PHI — one timestamp,
    // overwritten by the next login — so leaving it would not have been a
    // vulnerability. It is dropped so "the browser keeps nothing from the
    // previous session" stays a statement with no exceptions.
    clearRenewAt();
    notifySessionChanged();
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
    await endZitadelSession(SIGNED_OUT_MARK);
  }

  const currentPage = activePage(activeZone(pathname), pathname);
  const hasPagePanel = zone.pages.length > 1;

  return (
    <NavigationGuardProvider>
      <BrandThemeContext value={branding.theme}>
        {warningDeadline && (
          <IdleCountdown deadline={warningDeadline} onStay={handleStayLoggedIn} />
        )}
        <div data-brand-theme={branding.theme} className="flex h-screen overflow-hidden">
          <PhoneMenu
            open={menuOpen}
            onClose={() => setMenuOpen(false)}
            zones={zones}
            zone={zone}
            currentHref={currentPage?.href}
            isLoading={isLoading}
            returnFocusTo={menuButtonRef}
          />
          <aside
            // Floating and content-height from md (#1286 D1); below md the rail
            // stays the flush 64px icon column and the open rail a full-height
            // drawer (D3), so every card class is md-scoped.
            className={`hms-rail hms-sidebar-border hidden shrink-0 flex-col bg-(--sidebar-rail) md:flex ${navigationOpen ? "absolute inset-y-0 left-0 z-40 w-52 border-r shadow-xl md:relative md:inset-auto md:m-2 md:max-h-[calc(100dvh-1rem)] md:self-start md:rounded-xl md:border md:shadow-sm" : "w-16 border-r md:m-2 md:max-h-[calc(100dvh-1rem)] md:w-20 md:self-start md:rounded-xl md:border md:shadow-sm"}`}
          >
            <div className={`flex h-16 items-center ${navigationOpen ? "px-4" : "justify-center"}`}>
              <GuardedLink
                href="/dashboard"
                aria-label="Helivanta home"
                className="flex h-9 items-center gap-2 rounded-lg text-lg font-semibold text-sidebar-primary"
              >
                <span className="flex h-9 w-9 items-center justify-center rounded-lg bg-primary text-primary-foreground">
                  <BrandLogo key={branding.logo_url} branding={branding} compact />
                </span>
                {navigationOpen && (
                  <span className="text-sm text-sidebar-foreground">Helivanta</span>
                )}
              </GuardedLink>
            </div>
            <div className="hms-sidebar-border mx-2 border-t" />
            <nav
              id="hms-zone-navigation"
              aria-label="Zones"
              aria-busy={isLoading || undefined}
              className={`flex min-h-0 flex-1 flex-col gap-1 overflow-y-auto py-4 ${navigationOpen ? "px-3" : "items-center"}`}
            >
              {isLoading
                ? ZONES.filter((z) => z.group !== "admin").map((z) => (
                    <NavPlaceholder key={z.key} wide={navigationOpen} />
                  ))
                : zones
                    .filter((z) => z.group !== "admin")
                    .map((z, index, list) => (
                      <Fragment key={z.key}>
                        {index > 0 && list[index - 1].group !== z.group && (
                          <div
                            aria-hidden="true"
                            className="hms-sidebar-border my-2 w-8 self-center border-t"
                          />
                        )}
                        <ZoneLink
                          zone={z}
                          active={z.key === zone.key}
                          expanded={navigationOpen}
                          onNavigate={closeOverlays}
                        />
                      </Fragment>
                    ))}
            </nav>
            {/* The foot of the rail: admin zones, then the collapse control, which
              is chrome rather than a destination (#1270 D1, D3). */}
            <div
              className={`hms-sidebar-border mx-2 flex flex-col gap-1 border-t py-3 ${navigationOpen ? "px-1" : "items-center"}`}
            >
              {!isLoading &&
                zones
                  .filter((z) => z.group === "admin")
                  .map((z) => (
                    <ZoneLink
                      key={z.key}
                      zone={z}
                      active={z.key === zone.key}
                      expanded={navigationOpen}
                      onNavigate={closeOverlays}
                    />
                  ))}
              <button
                type="button"
                aria-controls="hms-zone-navigation"
                aria-expanded={navigationOpen}
                aria-label={navigationOpen ? "Collapse navigation" : "Expand navigation"}
                title={navigationOpen ? "Collapse navigation" : "Expand navigation"}
                onClick={() => setNavigationOpen(!navigationOpen)}
                className={`flex h-9 w-9 items-center justify-center rounded-lg text-sidebar-foreground/70 hover:bg-sidebar-accent/50 hover:text-sidebar-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-(--hms-accent) ${navigationOpen ? "ml-2" : ""}`}
              >
                {navigationOpen ? (
                  <ChevronsLeft className="size-4" aria-hidden="true" />
                ) : (
                  <ChevronsRight className="size-4" aria-hidden="true" />
                )}
              </button>
            </div>
          </aside>

          {/* Right of the rail: the header card spans the panel and the page, and
            the panel and page sit below it, top-aligned (#1286 D6). The header
            is outside the scroll container, so it needs no sticky backdrop. */}
          <div className="flex min-w-0 flex-1 flex-col md:gap-2 md:py-2 md:pr-2">
            <header className="hms-sidebar-border z-30 shrink-0 border-b bg-background md:rounded-xl md:border md:bg-(--sidebar-rail) md:shadow-sm">
              <div className="flex min-h-16 flex-wrap items-center justify-between gap-3 px-4 py-3 sm:px-6">
                <div className="flex items-center gap-2">
                  <button
                    ref={menuButtonRef}
                    type="button"
                    onClick={() => setMenuOpen(true)}
                    aria-label="Open menu"
                    aria-expanded={menuOpen}
                    aria-controls="hms-phone-menu"
                    className="mr-1 flex h-9 w-9 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-accent hover:text-foreground md:hidden"
                  >
                    <Menu className="h-5 w-5" aria-hidden="true" />
                  </button>
                  {/* The header names the hospital on every page, so the tenant is
                      never off screen. */}
                  <span
                    data-testid="tenant-name"
                    className="truncate text-sm font-medium text-muted-foreground"
                  >
                    {branding.display_name}
                  </span>
                  <span aria-hidden="true" className="text-sm text-border">
                    /
                  </span>
                  <h1 className="text-xl font-semibold text-foreground">
                    {hasPagePanel ? (currentPage?.label ?? zone.label) : zone.label}
                  </h1>
                </div>
                <div className="flex flex-wrap items-center gap-3">
                  {tenantPicker}
                  <AccountMenu hospitalName={branding.display_name} onSignOut={handleSignOut} />
                </div>
              </div>
            </header>
            <div className="flex min-h-0 flex-1">
              {/* Page panel: always shown from md (#1286 D4); on a phone its pages
                are in the menu (#1299).
                A single-page zone has none: it would repeat the rail's one link
                (#1255 spec D4). */}
              {hasPagePanel && (
                <aside className="hms-page-panel hms-sidebar-border hidden shrink-0 flex-col overflow-hidden bg-(--sidebar-rail) md:static md:mr-2 md:flex md:max-h-full md:w-56 md:self-start md:rounded-xl md:border md:shadow-sm">
                  <nav
                    aria-label={zone.label}
                    className="flex w-56 min-h-0 flex-1 flex-col gap-1 overflow-y-auto px-3 py-4"
                  >
                    {isLoading &&
                      zone.pages.map((pageLink) => <NavPlaceholder key={pageLink.href} wide />)}
                    {!isLoading &&
                      zone.pages.map((pageLink, index) => {
                        const isActive = currentPage?.href === pageLink.href;
                        // A section heading once, before the first permitted page of
                        // its run (#1258 D1); zones.test keeps each run contiguous.
                        const heading =
                          pageLink.section && pageLink.section !== zone.pages[index - 1]?.section
                            ? pageLink.section
                            : null;
                        return (
                          <Fragment key={pageLink.href}>
                            {heading && (
                              <p
                                data-section
                                className={`px-3 pb-1 text-xs font-medium text-sidebar-foreground/60 ${index > 0 ? "pt-4" : ""}`}
                              >
                                {heading}
                              </p>
                            )}
                            <GuardedLink
                              href={pageLink.href}
                              aria-current={isActive ? "page" : undefined}
                              onClick={closeOverlays}
                              className={`flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors ${
                                isActive
                                  ? "bg-sidebar-accent text-sidebar-foreground"
                                  : "text-sidebar-foreground/70 hover:bg-sidebar-accent/50 hover:text-sidebar-foreground"
                              }`}
                            >
                              <pageLink.icon className="h-4 w-4 shrink-0" aria-hidden="true" />
                              {pageLink.label}
                            </GuardedLink>
                          </Fragment>
                        );
                      })}
                  </nav>
                </aside>
              )}
              {/* relative: the content's absolutely positioned pieces (a select's
                hidden native <select>) are placed and clipped in here, not
                against the page, which would scroll the document too (#1313). */}
              <div
                data-hms-scroll
                className="relative flex min-w-0 flex-1 flex-col overflow-y-auto"
              >
                {/* A size container, so a list lays out by the space it has: the
                  rail expanding changes that without the viewport changing (#1303). */}
                <main className="@container/main flex-1 px-6 pt-6 pb-6 md:pt-0">
                  {/* Left-aligned with the header card, not centred: centring left an
                    empty band beside the rail on a page with no panel (#1296). A zone
                    declares full width in zones.ts; it still stops at 1760px. */}
                  <div
                    data-width={zone.width === "full" ? "full" : "capped"}
                    className={`w-full ${zone.width === "full" ? "max-w-[1760px]" : "max-w-6xl"}`}
                  >
                    {children}
                  </div>
                </main>
                <TesserixFooter />
              </div>
            </div>
          </div>
        </div>
      </BrandThemeContext>
    </NavigationGuardProvider>
  );
}
