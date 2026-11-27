"use client";

import { useState } from "react";
import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import {
  closeWardIndent,
  fetchFulfilmentIndent,
  fetchFulfilmentIndents,
  formatZonedDateTime,
  pharmacyKey,
  usePagedQuery,
  useApiMutation,
  wardIndentIssueUrl,
  wardIndentOutstanding,
  type PharmacyScope,
  type WardIndent,
  type WardIndentStatus,
} from "@helivanta/api";
import { Field, LoadMore, Panel, RequestFailure, useZodForm } from "@helivanta/ui";

const STATUSES: { key: WardIndentStatus; label: string }[] = [
  { key: "open", label: "Open" },
  { key: "closed", label: "Closed" },
  { key: "cancelled", label: "Cancelled" },
];

const reasonSchema = z.object({
  reason: z
    .string()
    .trim()
    .min(5, "Give a reason of at least 5 characters")
    .max(200, "At most 200 characters"),
});

export function WardIndentQueue({
  scope,
  timeZone,
  canFulfil,
}: {
  scope: PharmacyScope;
  timeZone: string;
  canFulfil: boolean;
}) {
  const [status, setStatus] = useState<WardIndentStatus>("open");
  const [selected, setSelected] = useState<string | null>(null);
  const list = usePagedQuery<WardIndent>(
    pharmacyKey(scope, "ward-indents", status),
    (cursor, signal) => fetchFulfilmentIndents(scope, status, cursor, signal),
    { poll: status === "open" },
  );
  if (selected)
    return (
      <WardIndentDetail
        scope={scope}
        timeZone={timeZone}
        id={selected}
        canFulfil={canFulfil}
        onBack={() => setSelected(null)}
      />
    );
  return (
    <Panel>
      <div role="tablist" aria-label="Ward indent status" className="mb-4 flex gap-2">
        {STATUSES.map((s) => (
          <Button
            key={s.key}
            role="tab"
            aria-selected={status === s.key}
            variant={status === s.key ? "default" : "outline"}
            onClick={() => setStatus(s.key)}
          >
            {s.label}
          </Button>
        ))}
      </div>
      {list.error ? (
        <RequestFailure
          message="Could not load ward indents."
          error={list.error}
          retry={list.retry}
        />
      ) : list.isPending ? (
        <p role="status">Loading ward indents…</p>
      ) : list.items.length === 0 ? (
        <p>No {status} ward indents at this outlet.</p>
      ) : (
        <ul className="divide-y">
          {list.items.map((indent) => (
            <li key={indent.id}>
              <button
                type="button"
                className="flex w-full flex-wrap items-center justify-between gap-2 py-3 text-left"
                onClick={() => setSelected(indent.id)}
              >
                <span className="font-medium">
                  {indent.hospital_name} · {indent.department_name}
                </span>
                <span className="text-sm text-muted-foreground">
                  Ward {indent.ward_code}, room {indent.room_code}, bed {indent.bed_code} ·{" "}
                  {indent.line_count} {indent.line_count === 1 ? "line" : "lines"} ·{" "}
                  {formatZonedDateTime(indent.raised_at, timeZone)}
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
      <LoadMore hasMore={list.hasMore} isLoading={list.isFetchingMore} onClick={list.loadMore} />
    </Panel>
  );
}

function WardIndentDetail({
  scope,
  timeZone,
  id,
  canFulfil,
  onBack,
}: {
  scope: PharmacyScope;
  timeZone: string;
  id: string;
  canFulfil: boolean;
  onBack: () => void;
}) {
  const key = pharmacyKey(scope, "ward-indent", id);
  const detail = useQuery({
    queryKey: key,
    queryFn: ({ signal }) => fetchFulfilmentIndent(scope, id, signal),
    staleTime: 0,
  });
  const form = useZodForm(reasonSchema, { reason: "" });
  const close = useApiMutation((reason: string | null) => closeWardIndent(scope, id, reason), {
    successToast: "Indent closed",
    invalidate: [key, pharmacyKey(scope, "ward-indents")],
  });
  const indent = detail.data;
  return (
    <Panel>
      <Button variant="outline" onClick={onBack}>
        Back to indents
      </Button>
      {detail.error ? (
        <RequestFailure
          message="Could not load this ward indent."
          error={detail.error}
          retry={() => void detail.refetch()}
        />
      ) : !indent ? (
        <p role="status">Loading ward indent…</p>
      ) : (
        <div className="mt-4 space-y-4">
          <h2 className="text-lg font-semibold">
            {indent.hospital_name} · {indent.department_name}
          </h2>
          <p className="text-sm text-muted-foreground">
            Ward {indent.ward_code}, room {indent.room_code}, bed {indent.bed_code} · raised{" "}
            {formatZonedDateTime(indent.raised_at, timeZone)} · {indent.status}
            {indent.close_reason ? ` · ${indent.close_reason}` : ""}
          </p>
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left">
                <th>Product</th>
                <th>Requested</th>
                <th>Issued</th>
                <th>Returned</th>
              </tr>
            </thead>
            <tbody>
              {(indent.lines ?? []).map((line) => (
                <tr key={line.line_number}>
                  <td>
                    {line.brand_name}{" "}
                    <span className="text-muted-foreground">{line.generic_name}</span>
                  </td>
                  <td>{line.requested_units}</td>
                  <td>{line.issued_units}</td>
                  <td>{line.returned_units}</td>
                </tr>
              ))}
            </tbody>
          </table>
          {indent.status === "open" && canFulfil && (
            <div className="space-y-3">
              <Link
                className="inline-flex rounded-md border px-3 py-2 text-sm font-medium"
                href={wardIndentIssueUrl(indent.id)}
              >
                Issue at the counter
              </Link>
              {wardIndentOutstanding(indent) ? (
                <form
                  noValidate
                  className="space-y-2"
                  onSubmit={form.handleSubmit((values) => close.mutate(values.reason))}
                >
                  <Field
                    id="close-reason"
                    label="Reason for closing short"
                    error={form.formState.errors.reason?.message}
                  >
                    <Input id="close-reason" {...form.register("reason")} />
                  </Field>
                  <Button type="submit" variant="outline" disabled={close.isPending}>
                    Close short
                  </Button>
                </form>
              ) : (
                <Button
                  variant="outline"
                  disabled={close.isPending}
                  onClick={() => close.mutate(null)}
                >
                  Close fully issued indent
                </Button>
              )}
            </div>
          )}
        </div>
      )}
    </Panel>
  );
}
