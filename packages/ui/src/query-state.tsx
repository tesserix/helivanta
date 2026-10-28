"use client";

import type { ReactNode } from "react";
import type { LucideIcon } from "lucide-react";
import type { UseApiPagedQueryResult } from "@helivanta/api";
import { Button } from "@tesserix/web";
import { EmptyState } from "./empty-state";
import { RequestFailure } from "./request-failure";

/**
 * The loading, failed, empty and filled states of a paged list (#1276).
 *
 * Fail closed: before the first answer, and after a failed one, `items` is
 * [] as well, so the empty state renders only after a successful answer with
 * no rows. A failed refresh or Load more keeps the rows already shown, under
 * a warning, rather than replacing them or passing them off as current.
 * This is the only renderer of EmptyState (it is not exported), so a list
 * cannot show "No … yet" without these checks.
 */
export function QueryState<T>({
  query,
  label,
  empty,
  children,
}: {
  query: Pick<UseApiPagedQueryResult<T>, "items" | "isPending" | "isError" | "retry">;
  /** Plural noun for the rows: "visits" reads "Loading visits…". */
  label: string;
  empty: { icon: LucideIcon; title: string; hint?: string };
  children: (items: T[]) => ReactNode;
}) {
  if (query.isPending) {
    return (
      <div role="status" aria-label={`Loading ${label}…`} className="space-y-3 px-5 py-5">
        <span className="sr-only">Loading {label}…</span>
        {["w-11/12", "w-3/4", "w-1/2"].map((width) => (
          <div
            key={width}
            aria-hidden="true"
            className={`h-4 ${width} animate-pulse rounded bg-muted motion-reduce:animate-none`}
          />
        ))}
      </div>
    );
  }
  if (query.isError && query.items.length === 0) {
    return (
      <div className="p-5">
        <RequestFailure message={`Could not load ${label}.`} retry={query.retry} />
      </div>
    );
  }
  if (query.items.length === 0) {
    return <EmptyState icon={empty.icon} title={empty.title} hint={empty.hint} />;
  }
  return (
    <>
      {query.isError && (
        <div
          role="alert"
          className="flex flex-wrap items-center justify-between gap-3 border-b bg-muted px-5 py-3 text-sm"
        >
          Could not load all {label}. The list may be out of date.
          <Button variant="outline" size="sm" onClick={() => void query.retry()}>
            Try again
          </Button>
        </div>
      )}
      {children(query.items)}
    </>
  );
}
