"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type ComponentProps,
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
  const [anyDirty, setAnyDirty] = useState(false);

  const held = () => [...guards.current.values()].find((guard) => guard.dirty || guard.saving);

  const register = useCallback((id: string, guard: Guard | null) => {
    if (guard) guards.current.set(id, guard);
    else guards.current.delete(id);
    setAnyDirty([...guards.current.values()].some((g) => g.dirty || g.saving));
  }, []);

  // A save in flight is not discardable: the request carries on and commits.
  // Hold the move until it finishes, rather than offer a Discard that lies.
  const confirm = useCallback((go: () => void) => {
    const guard = held();
    if (!guard) go();
    else if (!guard.saving) setPending({ name: guard.name, go });
  }, []);

  // Back cannot be cancelled, and the App Router moves on popstate too. The
  // module's listener runs before the router's and stops it: the page never
  // leaves, and history.go(1) puts the address back on the record's own
  // entry. Discard then goes back for real.
  const leaving = useRef(false);
  const restoring = useRef(false);
  useEffect(() => {
    if (!anyDirty) return;
    const onPopState = (event: PopStateEvent) => {
      if (leaving.current) {
        leaving.current = false;
        return;
      }
      event.stopImmediatePropagation();
      if (restoring.current) {
        restoring.current = false;
        return;
      }
      const guard = held();
      if (!guard) return;
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
  }, [anyDirty]);

  const value = useMemo(() => ({ register, confirm, dirty: () => !!held() }), [register, confirm]);

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
          go?.();
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
  useEffect(() => {
    context.register(id, { dirty, saving, name });
  }, [context, id, dirty, saving, name]);
  useEffect(() => () => context.register(id, null), [context, id]);
  return { confirm: context.confirm };
}

/** For shell links: holds a client-side move while a detail is unsaved. */
export function useGuardedLinkClick() {
  const context = useContext(Context);
  const router = useRouter();
  return useCallback(
    (event: MouseEvent<HTMLAnchorElement>, href: string) => {
      if (!context?.dirty()) return;
      event.preventDefault();
      // Client-side, as the link itself would have gone: zones never reload.
      context.confirm(() => router.push(href));
    },
    [context, router],
  );
}

/** A shell link that asks first while a detail is unsaved (#1303 D5). */
export function GuardedLink({ onClick, ...props }: ComponentProps<typeof Link>) {
  const guard = useGuardedLinkClick();
  return (
    <Link
      {...props}
      onClick={(event) => {
        guard(event, String(props.href));
        if (!event.defaultPrevented) onClick?.(event);
      }}
    />
  );
}
