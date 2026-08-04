"use client";

import { useState } from "react";
import { Badge, Button, Input } from "@tesserix/web";
import { FlaskConical } from "lucide-react";
import { apiFetch, useApiMutation, useApiQuery } from "@hms/api";
import { EmptyState, formatTime } from "@hms/ui";

type Order = {
  id: string;
  visit_id: string;
  patient_name: string;
  test_name: string;
  status: "pending" | "completed";
  result_value: string | null;
  resulted_at: string | null;
  created_at: string;
};

export function OrderList() {
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const orders = useApiQuery<{ data: Order[] }>(["orders"], "/lab/orders", { poll: true });

  const saveResult = useApiMutation(
    ({ id, value }: { id: string; value: string }) =>
      apiFetch("/lab/orders/" + id + "/result", {
        method: "POST",
        body: JSON.stringify({ result_value: value }),
      }),
    {
      successToast: "Result saved",
      invalidate: [["orders"]],
    },
  );

  const pending = orders.data?.data.filter((o) => o.status === "pending").length ?? 0;

  return (
    <section className="max-w-3xl rounded-lg border bg-card">
      <div className="border-b px-5 py-4">
        <h2 className="text-sm font-semibold text-foreground">Order queue</h2>
        <p className="mt-0.5 text-sm text-muted-foreground">
          {pending === 0 ? "Nothing waiting right now." : `${pending} order${pending === 1 ? "" : "s"} awaiting results.`}
        </p>
      </div>
      <ul className="divide-y text-sm">
        {orders.data?.data.length === 0 && (
          <li>
            <EmptyState
              icon={FlaskConical}
              title="No lab orders yet"
              hint="They appear here when a visit is created in MediCore."
            />
          </li>
        )}
        {orders.data?.data.map((o) => (
          <li key={o.id} className="flex flex-wrap items-center justify-between gap-4 px-5 py-3">
            <div className="min-w-0">
              <div className="flex items-center gap-3">
                <span className="truncate font-medium text-foreground">{o.patient_name}</span>
                <Badge variant={o.status === "pending" ? "secondary" : "outline"}>
                  {o.test_name}
                </Badge>
              </div>
              <div className="mt-0.5 text-muted-foreground">
                {o.status === "completed" ? `Result: ${o.result_value}` : "Awaiting result"}
              </div>
            </div>
            {o.status === "pending" ? (
              <div className="flex items-center gap-2">
                <label className="sr-only" htmlFor={`result-${o.id}`}>
                  Result for {o.patient_name}
                </label>
                <Input
                  id={`result-${o.id}`}
                  value={drafts[o.id] ?? ""}
                  onChange={(e) => setDrafts((d) => ({ ...d, [o.id]: e.target.value }))}
                  placeholder="e.g. WBC 6.1"
                  className="w-44"
                />
                <Button
                  onClick={() => saveResult.mutate({ id: o.id, value: drafts[o.id] ?? "" })}
                  disabled={
                    (saveResult.isPending && saveResult.variables?.id === o.id) || !(drafts[o.id] ?? "").trim()
                  }
                >
                  {saveResult.isPending && saveResult.variables?.id === o.id ? "Saving…" : "Save result"}
                </Button>
              </div>
            ) : (
              <time className="shrink-0 tabular-nums text-muted-foreground">
                {formatTime(o.resulted_at ?? "")}
              </time>
            )}
          </li>
        ))}
      </ul>
    </section>
  );
}
