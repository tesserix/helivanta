"use client";

import Link from "next/link";
import { Button } from "@tesserix/web";

/**
 * Pager is Previous / Next with the range shown (#1295 D6). Its controls are
 * links, so the page state lives in the URL and the back button works.
 *
 * `previousHref` is null when the way back is unknown, after a reload or a
 * shared link to a later page; the pager then offers "First page" rather
 * than guessing where the previous page began (D5).
 */
export function Pager({
  page,
  pageSize,
  shown,
  total,
  previousHref,
  nextHref,
  firstHref,
}: {
  page: number;
  pageSize: number;
  shown: number;
  total: number | null;
  previousHref: string | null;
  nextHref: string | null;
  firstHref: string;
}) {
  if (shown === 0 && page === 1) return null;
  const from = (page - 1) * pageSize + 1;
  const to = from + shown - 1;
  return (
    <nav
      aria-label="Pages"
      className="flex flex-wrap items-center justify-between gap-3 border-t px-5 py-3 text-sm"
    >
      <p className="tabular-nums text-muted-foreground">
        {from}–{to}
        {total !== null ? ` of ${total}` : ""}
      </p>
      <div className="flex gap-2">
        {previousHref ? (
          <Button asChild variant="outline" size="sm">
            <Link href={previousHref}>Previous</Link>
          </Button>
        ) : page > 1 ? (
          <Button asChild variant="outline" size="sm">
            <Link href={firstHref}>First page</Link>
          </Button>
        ) : (
          <Button variant="outline" size="sm" disabled>
            Previous
          </Button>
        )}
        {nextHref ? (
          <Button asChild variant="outline" size="sm">
            <Link href={nextHref}>Next</Link>
          </Button>
        ) : (
          <Button variant="outline" size="sm" disabled>
            Next
          </Button>
        )}
      </div>
    </nav>
  );
}
