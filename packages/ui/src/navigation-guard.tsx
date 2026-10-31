"use client";

import {
  createContext,
  useCallback,
  useContext,
  useLayoutEffect,
  useId,
  useMemo,
  useRef,
  useState,
  useTransition,
  type AnchorHTMLAttributes,
  type MouseEvent,
  type ReactNode,
} from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { BACK_GUARD_KEY } from "./chrome-flags";
import { ConfirmDialog } from "./confirm-dialog";

type Guard = { dirty: boolean; saving: boolean; name: string };

type GuardContext = {
  register: (id: string, guard: Guard | null) => void;
  confirm: (go: () => void) => void;
  dirty: () => boolean;
  navigationPending: boolean;
  isNavigationPending: () => boolean;
};

const Context = createContext<GuardContext | null>(null);

// On window, popstate listeners run in the order they were added, and the
// App Router adds its own as the app shell mounts. To stop it moving on Back
// while edits are unsaved, ours must run first: CHROME_INIT_SCRIPT adds it
// before any app script, and the provider switches what it does. Where that
// script did not run (component tests), the listener is added here instead.
type BackGuard = { on: ((event: PopStateEvent) => void) | null };
function backGuard(): BackGuard {
  const host = window as unknown as Record<string, BackGuard | undefined>;
  if (!host[BACK_GUARD_KEY]) {
    const guard: BackGuard = { on: null };
    host[BACK_GUARD_KEY] = guard;
    window.addEventListener("popstate", (event) => guard.on?.(event), { capture: true });
  }
  return host[BACK_GUARD_KEY];
}

/**
 * Unsaved edits are never lost silently (#1303 D5). A detail registers while
 * dirty; in-app links, Back and reload then ask "Discard changes to {name}?".
 * One dialog for the whole shell, so two forms cannot ask at once.
 */
export function NavigationGuardProvider({ children }: { children: ReactNode }) {
  const guards = useRef(new Map<string, Guard>());
  const [pending, setPending] = useState<{ name: string; go: () => void } | null>(null);
  const [navigationPending, startNavigation] = useTransition();
  // Effects can run after navigation starts but before its pending render commits.
  const navigationInFlight = useRef(false);
  useLayoutEffect(() => {
    navigationInFlight.current = navigationPending;
  }, [navigationPending]);
  const transition = useCallback((go: () => void) => {
    navigationInFlight.current = true;
    startNavigation(go);
  }, []);
  const isNavigationPending = useCallback(() => navigationInFlight.current, []);

  const held = useCallback(
    () => [...guards.current.values()].find((guard) => guard.dirty || guard.saving),
    [],
  );

  const register = useCallback((id: string, guard: Guard | null) => {
    if (guard) guards.current.set(id, guard);
    else guards.current.delete(id);
  }, []);

  // A save in flight is not discardable: the request carries on and commits.
  // Hold the move until it finishes, rather than offer a Discard that lies.
  const confirm = useCallback(
    (go: () => void) => {
      const guard = held();
      if (!guard) transition(go);
      else if (!guard.saving) setPending({ name: guard.name, go });
    },
    [held, transition],
  );

  // Back cannot be cancelled, and the App Router moves on popstate too. The
  // module's listener runs before the router's and stops it: the page never
  // leaves, and history.go(1) puts the address back on the record's own
  // entry. Discard then goes back for real.
  const leaving = useRef(false);
  const restoring = useRef(false);
  useLayoutEffect(() => {
    const onPopState = (event: PopStateEvent) => {
      if (leaving.current) {
        leaving.current = false;
        return;
      }
      if (restoring.current) {
        event.stopImmediatePropagation();
        restoring.current = false;
        return;
      }
      const guard = held();
      if (!guard) return;
      event.stopImmediatePropagation();
      restoring.current = true;
      window.history.go(1);
      if (guard.saving) return;
      setPending({
        name: guard.name,
        go: () => {
          leaving.current = true;
          window.history.back();
        },
      });
    };
    const onBeforeUnload = (event: BeforeUnloadEvent) => {
      if (!held()) return;
      event.preventDefault();
      // Older Safari shows the prompt only when returnValue is set.
      event.returnValue = "";
    };
    const back = backGuard();
    back.on = onPopState;
    window.addEventListener("beforeunload", onBeforeUnload);
    return () => {
      if (back.on === onPopState) back.on = null;
      window.removeEventListener("beforeunload", onBeforeUnload);
      // A Discard whose Back never arrived must not wave through the next one.
      leaving.current = false;
      restoring.current = false;
    };
  }, [held]);

  const value = useMemo(
    () => ({ register, confirm, dirty: () => !!held(), navigationPending, isNavigationPending }),
    [register, confirm, held, navigationPending, isNavigationPending],
  );

  return (
    <Context value={value}>
      {children}
      <ConfirmDialog
        open={pending !== null}
        onOpenChange={(open) => !open && setPending(null)}
        title={`Discard changes to ${pending?.name ?? ""}?`}
        description="Your unsaved changes will be lost."
        confirmLabel="Discard"
        cancelLabel="Keep editing"
        onConfirm={() => {
          const go = pending?.go;
          setPending(null);
          if (go) transition(go);
        }}
      />
    </Context>
  );
}

function useGuardContext() {
  const context = useContext(Context);
  // Fail closed: a guard with no provider would let edits go silently.
  if (!context) throw new Error("useLeaveGuard needs a NavigationGuardProvider (HmsShell)");
  return context;
}

/**
 * While `dirty`, in-app links, Back and reload ask before leaving. `confirm`
 * runs `go` at once when nothing is unsaved, and otherwise only on Discard.
 */
export function useLeaveGuard(dirty: boolean, name: string, saving = false) {
  const context = useGuardContext();
  const id = useId();
  useLayoutEffect(() => {
    context.register(id, { dirty, saving, name });
  }, [context, id, dirty, saving, name]);
  useLayoutEffect(() => () => context.register(id, null), [context, id]);
  return { confirm: context.confirm };
}

/**
 * A click that opens another tab or window (#1303 E7): nothing in this tab
 * is left, so there is nothing to ask about.
 */
export function isModifiedClick(
  event: Pick<MouseEvent, "metaKey" | "ctrlKey" | "shiftKey" | "altKey" | "button">,
) {
  return event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || event.button !== 0;
}

/**
 * Asks before a move of the page's own (an outlet picker, say) while any
 * detail is unsaved, without registering one itself.
 */
export function useConfirmLeave() {
  return useGuardContext().confirm;
}

export function useNavigationPending() {
  const context = useGuardContext();
  return { pending: context.navigationPending, isPending: context.isNavigationPending };
}

/** For shell links: holds a client-side move while a detail is unsaved. */
export function useGuardedLinkClick() {
  const context = useContext(Context);
  const router = useRouter();
  return useCallback(
    (event: MouseEvent<HTMLAnchorElement>, href: string) => {
      if (!context?.dirty() || isModifiedClick(event)) return;
      event.preventDefault();
      // Client-side, as the link itself would have gone: zones never reload.
      context.confirm(() => router.push(href));
    },
    [context, router],
  );
}

/**
 * A shell link that asks first while a detail is unsaved (#1303 D5). Its
 * props are stated rather than taken from next/link's: those differ between
 * the type copies the shell and this package resolve.
 */
export function GuardedLink({
  href,
  onClick,
  children,
  ...anchor
}: Omit<AnchorHTMLAttributes<HTMLAnchorElement>, "href" | "onClick"> & {
  href: string;
  onClick?: (event: MouseEvent<HTMLAnchorElement>) => void;
}) {
  const guard = useGuardedLinkClick();
  const context = useContext(Context);
  const router = useRouter();
  return (
    <Link
      {...anchor}
      href={href}
      onNavigate={(event: { preventDefault: () => void }) => {
        if (!context) return;
        event.preventDefault();
        context.confirm(() => router.push(href));
      }}
      onClick={(event: MouseEvent<HTMLAnchorElement>) => {
        guard(event, href);
        if (!event.defaultPrevented) onClick?.(event);
      }}
    >
      {children}
    </Link>
  );
}
