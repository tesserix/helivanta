"use client";

import { useCallback, useEffect, useState } from "react";

type Medication = { id: string; name: string; strength: string; created_at: string };

export function MedicationsPanel() {
  const [rows, setRows] = useState<Medication[]>([]);
  const [name, setName] = useState("");
  const [strength, setStrength] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    try {
      const res = await fetch("/api/v1/pharmacy/medications");
      if (!res.ok) throw new Error(String(res.status));
      const body = await res.json();
      setRows(body.data ?? []);
      setError(null);
    } catch {
      setError("Could not load medications. Is the API running?");
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  async function add(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    try {
      const res = await fetch("/api/v1/pharmacy/medications", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name, strength }),
      });
      if (!res.ok) throw new Error(String(res.status));
      setName("");
      setStrength("");
      await load();
    } catch {
      setError("Could not add medication.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="max-w-xl space-y-4">
      <form onSubmit={add} className="flex flex-wrap items-end gap-3">
        <label className="flex flex-col gap-1 text-sm">
          Name
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
            className="rounded-md border px-3 py-2"
          />
        </label>
        <label className="flex flex-col gap-1 text-sm">
          Strength
          <input
            value={strength}
            onChange={(e) => setStrength(e.target.value)}
            className="rounded-md border px-3 py-2"
          />
        </label>
        <button
          type="submit"
          disabled={busy}
          className="rounded-md bg-primary px-3 py-2 text-sm text-primary-foreground disabled:opacity-50"
        >
          {busy ? "Adding…" : "Add medication"}
        </button>
      </form>
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      <ul className="divide-y rounded-md border text-sm">
        {rows.length === 0 && <li className="p-3 text-muted-foreground">No medications yet.</li>}
        {rows.map((m) => (
          <li key={m.id} className="flex justify-between p-3">
            <span>{m.name}</span>
            <span className="text-muted-foreground">{m.strength}</span>
          </li>
        ))}
      </ul>
    </section>
  );
}
