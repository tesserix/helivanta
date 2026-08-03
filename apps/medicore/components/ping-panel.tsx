"use client";

import { useCallback, useEffect, useState } from "react";

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
    <section className="max-w-xl space-y-4">
      <button
        onClick={ping}
        disabled={busy}
        className="rounded-md bg-primary px-3 py-2 text-sm text-primary-foreground disabled:opacity-50"
      >
        {busy ? "Pinging…" : `Ping from ${department}`}
      </button>
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      <ul className="divide-y rounded-md border text-sm">
        {pings.length === 0 && <li className="p-3 text-muted-foreground">No activity yet.</li>}
        {pings.map((p) => (
          <li key={p.id} className="flex justify-between p-3">
            <span>{p.message}</span>
            <time className="text-muted-foreground">{new Date(p.created_at).toLocaleTimeString()}</time>
          </li>
        ))}
      </ul>
    </section>
  );
}
