"use client";

import { useCallback, useEffect, useState } from "react";
import { Badge, Button, Input } from "@tesserix/web";

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
  const [rows, setRows] = useState<Order[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [busyID, setBusyID] = useState<string | null>(null);
  const [drafts, setDrafts] = useState<Record<string, string>>({});

  const load = useCallback(async () => {
    try {
      const res = await fetch("/api/v1/lab/orders");
      if (!res.ok) throw new Error(String(res.status));
      const body = await res.json();
      setRows(body.data ?? []);
      setError(null);
    } catch {
      setError("Could not load orders. Is the API running?");
    }
  }, []);

  useEffect(() => {
    void load();
    const timer = setInterval(() => void load(), 3000);
    return () => clearInterval(timer);
  }, [load]);

  async function saveResult(id: string) {
    setBusyID(id);
    try {
      const res = await fetch(`/api/v1/lab/orders/${id}/result`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ result_value: drafts[id] ?? "" }),
      });
      if (!res.ok) throw new Error(String(res.status));
      await load();
    } catch {
      setError("Could not save result.");
    } finally {
      setBusyID(null);
    }
  }

  const pending = rows.filter((o) => o.status === "pending").length;

  return (
    <section className="max-w-3xl rounded-lg border bg-card">
      <div className="border-b px-5 py-4">
        <h2 className="text-sm font-semibold text-foreground">Order queue</h2>
        <p className="mt-0.5 text-sm text-muted-foreground">
          {pending === 0 ? "Nothing waiting right now." : `${pending} order${pending === 1 ? "" : "s"} awaiting results.`}
        </p>
      </div>
      {error && (
        <p role="alert" className="border-b px-5 py-3 text-sm text-destructive">
          {error}
        </p>
      )}
      <ul className="divide-y text-sm">
        {rows.length === 0 && (
          <li className="px-5 py-8 text-center text-muted-foreground">
            No lab orders yet. They appear here when a visit is created in MediCore.
          </li>
        )}
        {rows.map((o) => (
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
                  onClick={() => saveResult(o.id)}
                  disabled={busyID === o.id || !(drafts[o.id] ?? "").trim()}
                >
                  {busyID === o.id ? "Saving…" : "Save result"}
                </Button>
              </div>
            ) : (
              <time className="shrink-0 tabular-nums text-muted-foreground">
                {o.resulted_at ? new Date(o.resulted_at).toLocaleTimeString() : ""}
              </time>
            )}
          </li>
        ))}
      </ul>
    </section>
  );
}
