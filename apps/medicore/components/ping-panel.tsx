"use client";

import { useCallback, useEffect, useState } from "react";
import { Button } from "@tesserix/web";

type Ping = { id: string; message: string; created_at: string };

export function PingPanel({ department }: { department: string }) {
  const [pings, setPings] = useState<Ping[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    try {
      const res = await fetch("/api/v1/reference/pings");
      if (!res.ok) throw new Error(String(res.status));
      const body = await res.json();
      setPings(body.data ?? []);
      setError(null);
    } catch {
      setError("Could not load activity. Is the API running?");
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  async function ping() {
    setBusy(true);
    try {
      const res = await fetch("/api/v1/reference/ping", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ message: `${department} ping` }),
      });
      if (!res.ok) throw new Error(String(res.status));
      await load();
    } catch {
      setError("Ping failed.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="max-w-2xl rounded-lg border bg-card">
      <div className="flex items-center justify-between border-b px-5 py-4">
        <div>
          <h2 className="text-sm font-semibold text-foreground">Platform check</h2>
          <p className="mt-0.5 text-sm text-muted-foreground">
            Round-trips the event pipeline end to end.
          </p>
        </div>
        <Button variant="outline" onClick={ping} disabled={busy}>
          {busy ? "Pinging…" : `Ping from ${department}`}
        </Button>
      </div>
      {error && (
        <p role="alert" className="border-b px-5 py-3 text-sm text-destructive">
          {error}
        </p>
      )}
      <ul className="divide-y text-sm">
        {pings.length === 0 && (
          <li className="px-5 py-8 text-center text-muted-foreground">No activity yet.</li>
        )}
        {pings.map((p) => (
          <li key={p.id} className="flex items-center justify-between gap-4 px-5 py-3">
            <span className="truncate text-foreground">{p.message}</span>
            <time className="shrink-0 tabular-nums text-muted-foreground">
              {new Date(p.created_at).toLocaleTimeString()}
            </time>
          </li>
        ))}
      </ul>
    </section>
  );
}
