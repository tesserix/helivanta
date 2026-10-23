"use client";

import Link from "next/link";
import { ArrowLeft } from "lucide-react";
import type { ReactNode } from "react";

/**
 * A list and its open record (#1303 D1): side by side when the shell's main
 * is at least 860px wide, the record alone below that. It lays out by main's
 * width through container queries, never the viewport's: the rail expanding
 * changes the space without changing the screen. One grid at every width,
 * so the record is never remounted and a half-typed form survives the flip.
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
  if (detail === null) return <>{list}</>;
  return (
    <div
      data-testid="list-detail"
      className="grid items-start gap-4 @[860px]/main:grid-cols-[minmax(0,1fr)_400px] @[1400px]/main:grid-cols-[minmax(0,1fr)_600px]"
    >
      <div data-testid="list-detail-list" className="hidden min-w-0 @[860px]/main:block">
        {list}
      </div>
      {/* Its own scroll, below the header: the list keeps its place while the
          record scrolls (#1303). 7rem is the header and its gaps. */}
      <section
        aria-label={label}
        className="min-w-0 @[860px]/main:sticky @[860px]/main:top-0 @[860px]/main:max-h-[calc(100dvh-7rem)] @[860px]/main:overflow-y-auto"
      >
        <Link
          href={backHref}
          className="mb-3 inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground @[860px]/main:hidden"
        >
          <ArrowLeft className="size-4" aria-hidden="true" />
          {backLabel}
        </Link>
        {detail}
      </section>
    </div>
  );
}
