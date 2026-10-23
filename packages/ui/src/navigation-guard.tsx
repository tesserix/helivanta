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
import { ConfirmDialog } from "./confirm-dialog";

type Guard = { dirty: boolean; name: string };

type GuardContext = {
  register: (id: string, guard: Guard | null) => void;
  confirm: (go: () => void) => void;
  dirty: () => boolean;
};

const Context = createContext<GuardContext | null>(null);

/**
 * Unsaved edits are never lost silently (#1303 D5). A detail registers while
 * dirty; in-app links, Back and reload then ask "Discard changes to {name}?".
 * One dialog for the whole shell, so two forms cannot ask at once.
 */
export function NavigationGuardProvider({ children }: { children: ReactNode }) {
  const guards = useRef(new Map<string, Guard>());
  const [pending, setPending] = useState<{ name: string; go: () => void } | null>(null);
  const [anyDirty, setAnyDirty] = useState(false);

  const firstDirty = () => [...guards.current.values()].find((guard) => guard.dirty);

  const register = useCallback((id: string, guard: Guard | null) => {
    if (guard) guards.current.set(id, guard);
    else guards.current.delete(id);
    setAnyDirty([...guards.current.values()].some((g) => g.dirty));
  }, []);

  const confirm = useCallback((go: () => void) => {
    const dirty = firstDirty();
    if (!dirty) go();
    else setPending({ name: dirty.name, go });
  }, []);

  // Back cannot be cancelled: put the page's entry back, then ask. Discard
  // goes back for real, with the guard lifted for that one step.
  const leaving = useRef(false);
  useEffect(() => {
    if (!anyDirty) return;
    const onPopState = () => {
      if (leaving.current) {
        leaving.current = false;
        return;
      }
      const dirty = firstDirty();
      if (!dirty) return;
      window.history.pushState(window.history.state, "", window.location.href);
      setPending({
        name: dirty.name,
        go: () => {
          leaving.current = true;
          window.history.back();
        },
      });
    };
    const onBeforeUnload = (event: BeforeUnloadEvent) => event.preventDefault();
    window.addEventListener("popstate", onPopState);
    window.addEventListener("beforeunload", onBeforeUnload);
    return () => {
      window.removeEventListener("popstate", onPopState);
      window.removeEventListener("beforeunload", onBeforeUnload);
    };
  }, [anyDirty]);

  const value = useMemo(
    () => ({ register, confirm, dirty: () => !!firstDirty() }),
    [register, confirm],
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
export function useLeaveGuard(dirty: boolean, name: string) {
  const context = useGuardContext();
  const id = useId();
  useEffect(() => {
    context.register(id, { dirty, name });
  }, [context, id, dirty, name]);
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
