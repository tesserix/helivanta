"use client";

import { useCallback, useEffect, useEffectEvent, useLayoutEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { isModifiedClick, useLeaveGuard } from "@helivanta/ui";
import { useDetail } from "./use-detail";

/** What a detail's form reports so its page can guard unsaved edits (#1303 D5). */
export type FormState = (dirty: boolean, pending: boolean) => void;

/** Reports a form's state while it is mounted; a form that goes away holds nothing. */
export function useReportState(report: FormState | undefined, dirty: boolean, pending: boolean) {
  // Browser Back must see the dirty state before the next event can navigate (#1347).
  useLayoutEffect(() => {
    report?.(dirty, pending);
  }, [report, dirty, pending]);
  useLayoutEffect(() => () => report?.(false, false), [report]);
}

type Forms = Record<string, { dirty: boolean; pending: boolean }>;

/**
 * A list page and its open record (#1303 D2–D7, all-pages E1), the one copy
 * every pharmacy page uses:
 * - the open record is the URL (`useDetail`);
 * - Previous / Next follow the rows as they were when it was opened, held
 *   in memory, so a record that leaves the list keeps Next on the row
 *   below it; opened from a link or a reload there is no snapshot, and
 *   stepping is off rather than guessed;
 * - every form in the record reports through `formState(key)`, and while
 *   any is unsaved, switching, closing and in-page links ask first;
 * - viewing leaves focus to the record's heading, creating focuses the
 *   first field, Escape closes, and focus returns to the row.
 */
export function useListDetail<Row extends { id: string }>({
  rows,
  name,
  fallbackName,
  newName,
  headingId,
}: {
  /** The list's rows now, in list order. */
  rows: Row[];
  name: (row: Row) => string;
  /** The open record's name when it is not among the rows ("this bill"). */
  fallbackName: string;
  newName: string;
  /** The list heading, for focus when the row has gone. */
  headingId: string;
}) {
  const router = useRouter();
  const detail = useDetail();
  const [snapshot, setSnapshot] = useState<Row[] | null>(null);
  const at = snapshot?.findIndex((row) => row.id === detail.openId) ?? -1;
  const open = (snapshot ?? []).concat(rows).find((row) => row.id === detail.openId);
  const openName = open ? name(open) : null;

  const [forms, setForms] = useState<Forms>({});
  const reporters = useRef(new Map<string, FormState>());
  const formState = useCallback((key: string) => {
    let report = reporters.current.get(key);
    if (!report) {
      report = (dirty, pending) =>
        setForms((was) =>
          was[key]?.dirty === dirty && was[key]?.pending === pending
            ? was
            : { ...was, [key]: { dirty, pending } },
        );
      reporters.current.set(key, report);
    }
    return report;
  }, []);
  const dirty = Object.values(forms).some((form) => form.dirty);
  const busy = Object.values(forms).some((form) => form.pending);
  // A save in flight is held, not offered as discardable: it commits anyway.
  const guard = useLeaveGuard(dirty, detail.creating ? newName : (openName ?? fallbackName), busy);

  // A save that finishes after the person has left must not pull them back.
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  const lastOpened = useRef<string | null>(null);
  function openRow(id: string) {
    guard.confirm(() => {
      setSnapshot(rows);
      lastOpened.current = id;
      detail.open(id);
    });
  }
  function step(by: -1 | 1) {
    const next = snapshot?.[at + by];
    if (!next) return;
    guard.confirm(() => {
      lastOpened.current = next.id;
      detail.open(next.id);
    });
  }
  const canStep = (by: -1 | 1) =>
    !busy && !!snapshot && at >= 0 && at + by >= 0 && at + by < snapshot.length;
  function close() {
    guard.confirm(() => {
      const returnTo = lastOpened.current ?? detail.openId;
      setSnapshot(null);
      detail.close();
      // Back to the row it opened from, or the list when that row has gone.
      requestAnimationFrame(() =>
        (
          document.querySelector<HTMLElement>(`[data-open-row="${returnTo}"]`) ??
          document.getElementById(headingId)
        )?.focus(),
      );
    });
  }
  /** After a create: the new record opens, and closing returns to its row (E7). */
  function saved(id: string) {
    if (!mounted.current) return;
    lastOpened.current = id;
    detail.open(id);
  }

  // Tabs, the pager and the back link are links: hold them while unsaved.
  // A modified click opens another tab and leaves this one as it is.
  function holdLinks(event: React.MouseEvent) {
    const link = (event.target as Element).closest("a");
    if (!link || !(dirty || busy) || isModifiedClick(event)) return;
    event.preventDefault();
    const href = link.getAttribute("href");
    if (href) guard.confirm(() => router.push(href));
  }

  const paneRef = useRef<HTMLDivElement | null>(null);
  const isOpen = detail.openId !== null || detail.creating;
  // Escape closes, unless it is closing a menu inside the record first.
  const onEscape = useEffectEvent((event: KeyboardEvent) => {
    if (event.key !== "Escape" || event.defaultPrevented) return;
    if ((event.target as Element).closest('[role="listbox"],[aria-expanded="true"]')) return;
    close();
  });
  useEffect(() => {
    const node = paneRef.current;
    if (!isOpen || !node) return;
    node.addEventListener("keydown", onEscape);
    return () => node.removeEventListener("keydown", onEscape);
  }, [isOpen]);
  // Creating focuses the first field; viewing focuses the heading (in the record).
  useEffect(() => {
    if (detail.creating)
      requestAnimationFrame(() => paneRef.current?.querySelector<HTMLElement>("input")?.focus());
  }, [detail.creating]);

  return {
    detail,
    isOpen,
    openName,
    busy,
    formState,
    openRow,
    create: () => guard.confirm(() => detail.create()),
    saved,
    step,
    canStep,
    close,
    holdLinks,
    paneRef,
    rowProps: (id: string) => ({
      "aria-current": id === detail.openId ? ("true" as const) : undefined,
    }),
    openProps: (id: string) => ({ "data-open-row": id }),
  };
}
