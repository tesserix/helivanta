"use client";

import { useCallback, useEffect, useState } from "react";

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

  return (
    <section className="max-w-3xl space-y-4">
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      <ul className="divide-y rounded-md border text-sm">
        {rows.length === 0 && <li className="p-3 text-muted-foreground">No lab orders yet.</li>}
        {rows.map((o) => (
          <li key={o.id} className="flex items-center justify-between gap-4 p-3">
            <div className="min-w-0">
              <div className="font-medium">
                {o.patient_name} — {o.test_name}
              </div>
              <div className="text-muted-foreground">
                {o.status === "completed" ? `Result: ${o.result_value}` : "Awaiting result"}
              </div>
            </div>
            {o.status === "pending" ? (
              <div className="flex items-center gap-2">
                <label className="sr-only" htmlFor={`result-${o.id}`}>
                  Result for {o.patient_name}
                </label>
                <input
                  id={`result-${o.id}`}
                  value={drafts[o.id] ?? ""}
                  onChange={(e) => setDrafts((d) => ({ ...d, [o.id]: e.target.value }))}
                  placeholder="e.g. WBC 6.1"
                  className="w-40 rounded-md border px-3 py-2"
                />
                <button
                  onClick={() => saveResult(o.id)}
                  disabled={busyID === o.id || !(drafts[o.id] ?? "").trim()}
                  className="rounded-md bg-primary px-3 py-2 text-primary-foreground disabled:opacity-50"
                >
                  {busyID === o.id ? "Saving…" : "Save result"}
                </button>
              </div>
            ) : (
              <span className="text-muted-foreground">Completed</span>
            )}
          </li>
        ))}
      </ul>
    </section>
  );
}
