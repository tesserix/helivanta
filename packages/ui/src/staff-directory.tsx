"use client";

import { useId, useState, type ReactNode } from "react";
import { UsersRound } from "lucide-react";
import { JOB_LABELS, type Staff } from "@helivanta/api";
import { Button } from "./button";
import { Panel } from "./panel";
import { QueryState } from "./query-state";

export type StaffDirectoryProps = {
  title: string;
  description: string;
  emptyDescription: string;
  scopeDescription: string;
  rows: readonly Staff[];
  isPending: boolean;
  isError: boolean;
  /** The failed read's error, so a refusal shows as one (#1591). */
  error: unknown;
  onRetry: () => void | Promise<unknown>;
  headerActions?: ReactNode;
  rowActions?: (staff: Staff) => ReactNode;
  onFirstPage?: () => void;
  onNextPage?: () => void;
};

export function StaffDirectory({
  title,
  description,
  emptyDescription,
  scopeDescription,
  rows,
  isPending,
  isError,
  error,
  onRetry,
  headerActions,
  rowActions,
  onFirstPage,
  onNextPage,
}: StaffDirectoryProps) {
  const filterId = useId();
  const headingId = useId();
  const [filter, setFilter] = useState("");
  const visible =
    isPending || isError
      ? []
      : rows.filter((row) =>
          `${row.display_name} ${row.employee_code} ${JOB_LABELS[row.job_category]}`
            .toLowerCase()
            .includes(filter.toLowerCase()),
        );
  return (
    <Panel flush aria-labelledby={headingId}>
      <div className="flex flex-wrap items-center justify-between gap-4 border-b p-5">
        <div>
          <h3 id={headingId} className="font-semibold">
            {title}
          </h3>
          <p className="mt-1 text-xs text-muted-foreground">{description}</p>
        </div>
        {!isPending && !isError && headerActions}
      </div>
      <div className="flex flex-wrap items-center justify-between gap-3 border-b px-5 py-4">
        <label htmlFor={filterId} className="w-full max-w-sm">
          <span className="sr-only">Filter this page</span>
          <input
            id={filterId}
            className="w-full rounded-lg border bg-background px-3 py-2.5 text-sm outline-none focus-visible:ring-2 focus-visible:ring-(--hms-accent)"
            placeholder="Filter this page by name, code or category"
            value={filter}
            onChange={(event) => setFilter(event.target.value)}
            disabled={isPending || isError}
          />
        </label>
        {!isPending && !isError && (
          <span aria-live="polite" className="text-xs text-muted-foreground">
            {visible.length} {visible.length === 1 ? "record" : "records"} on this page
          </span>
        )}
      </div>
      <QueryState
        query={{ items: visible, isPending, isError, error, retry: onRetry }}
        label="staff"
        empty={{
          icon: UsersRound,
          title: filter ? "No matching records on this page" : "Your staff directory starts here",
          hint: filter ? "Try a different name or employee code." : emptyDescription,
        }}
      >
        {(items) => (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm">
              <caption className="sr-only">Staff at {title}</caption>
              <thead className="bg-muted/40 text-xs text-muted-foreground">
                <tr>
                  {[
                    "Staff member",
                    "Employee code",
                    "Job category",
                    "Status",
                    ...(rowActions ? ["Actions"] : []),
                  ].map((label) => (
                    <th key={label} scope="col" className="px-5 py-3 font-medium">
                      {label}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody className="divide-y">
                {items.map((row) => (
                  <tr key={row.id} className="hover:bg-muted/25">
                    <td className="px-5 py-4 font-medium">{row.display_name}</td>
                    <td className="px-5 py-4 font-mono text-xs text-muted-foreground">
                      {row.employee_code}
                    </td>
                    <td className="px-5 py-4 text-muted-foreground">
                      {JOB_LABELS[row.job_category]}
                    </td>
                    <td className="px-5 py-4">
                      <span
                        className={
                          row.employment_status === "active"
                            ? "rounded-full bg-(--hms-accent-dim) px-2.5 py-1 text-xs font-medium text-(--hms-accent)"
                            : "rounded-full bg-muted px-2.5 py-1 text-xs text-muted-foreground"
                        }
                      >
                        {row.employment_status === "active" ? "Active" : "Inactive"}
                      </span>
                    </td>
                    {rowActions && (
                      <td className="px-5 py-4">
                        <div className="flex flex-wrap gap-1">{rowActions(row)}</div>
                      </td>
                    )}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </QueryState>
      <div className="flex flex-wrap items-center justify-between gap-3 border-t px-5 py-3">
        <span className="text-xs text-muted-foreground">{scopeDescription}</span>
        {!isPending && !isError && (onFirstPage || onNextPage) && (
          <nav aria-label="Staff directory pages" className="flex gap-2">
            {onFirstPage && (
              <Button size="sm" variant="outline" onClick={onFirstPage}>
                First page
              </Button>
            )}
            {onNextPage && (
              <Button size="sm" variant="outline" onClick={onNextPage}>
                Next page
              </Button>
            )}
          </nav>
        )}
      </div>
    </Panel>
  );
}
