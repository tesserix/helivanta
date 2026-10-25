"use client";

import Link from "next/link";
import { ArrowLeft } from "lucide-react";
import { useLayoutEffect, useRef, type ReactNode } from "react";

/**
 * A list and its open record (#1303 D1): side by side when the shell's main
 * is at least 840px wide, the record alone below that. It lays out by main's
 * width through container queries, never the viewport's: the rail expanding
 * changes the space without changing the screen. One grid at every width,
 * so the record is never remounted and a half-typed form survives the flip,
 * and one tree open or closed, so the list is never remounted either (E7).
 */
export function ListDetail({
  list,
  detail,
  label,
  backHref,
  backLabel,
}: {
  list: ReactNode;
  /** The open record, or null when nothing is open. */
  detail: ReactNode | null;
  /** The record's region name, such as "Bill TEST-001". */
  label: string;
  backHref: string;
  backLabel: string;
}) {
  const open = detail !== null;
  const listRef = useRef<HTMLDivElement>(null);
  const pane = useRef<HTMLElement>(null);
  // Where the record replaces the list (a phone), each record opens at its
  // own top, not wherever the list or the last record was scrolled to
  // (#1303 E7). The label names the record, so Next counts as opening.
  useLayoutEffect(() => {
    if (!open || !listRef.current || getComputedStyle(listRef.current).display !== "none") return;
    pane.current?.closest<HTMLElement>("[data-hms-scroll]")?.scrollTo({ top: 0 });
  }, [open, label]);
  return (
    <div
      data-testid="list-detail"
      className={
        open
          ? "grid items-start gap-4 @[840px]/main:grid-cols-[minmax(0,1fr)_400px] @[1400px]/main:grid-cols-[minmax(0,1fr)_600px]"
          : undefined
      }
    >
      <div
        ref={listRef}
        data-testid="list-detail-list"
        className={open ? "hidden min-w-0 @[840px]/main:block" : undefined}
      >
        {list}
      </div>
      {open && (
        // Its own scroll, below the header: the list keeps its place while
        // the record scrolls (#1303). 7rem is the header and its gaps.
        <section
          ref={pane}
          aria-label={label}
          className="@container/pane min-w-0 @[840px]/main:sticky @[840px]/main:top-0 @[840px]/main:max-h-[calc(100dvh-7rem)] @[840px]/main:overflow-y-auto"
        >
          <Link
            href={backHref}
            className="mb-3 inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground @[840px]/main:hidden"
          >
            <ArrowLeft className="size-4" aria-hidden="true" />
            {backLabel}
          </Link>
          {detail}
        </section>
      )}
    </div>
  );
}
