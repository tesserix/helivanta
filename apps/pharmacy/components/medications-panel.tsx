"use client";

import { useCallback, useEffect, useState } from "react";
import { Button, Input } from "@tesserix/web";

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
    <section className="max-w-2xl rounded-lg border bg-card">
      <div className="border-b px-5 py-4">
        <h2 className="text-sm font-semibold text-foreground">Formulary</h2>
        <p className="mt-0.5 text-sm text-muted-foreground">
          Medications available for dispensing in this store.
        </p>
      </div>
      <form onSubmit={add} className="flex flex-wrap items-end gap-3 border-b px-5 py-4">
        <label htmlFor="medication-name" className="flex min-w-48 flex-1 flex-col gap-1.5 text-sm font-medium">
          Name
          <Input
            id="medication-name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
            placeholder="e.g. Paracetamol"
          />
        </label>
        <label htmlFor="medication-strength" className="flex w-36 flex-col gap-1.5 text-sm font-medium">
          Strength
          <Input
            id="medication-strength"
            value={strength}
            onChange={(e) => setStrength(e.target.value)}
            placeholder="500mg"
          />
        </label>
        <Button type="submit" disabled={busy}>
          {busy ? "Adding…" : "Add medication"}
        </Button>
      </form>
      {error && (
        <p role="alert" className="border-b px-5 py-3 text-sm text-destructive">
          {error}
        </p>
      )}
      <ul className="divide-y text-sm">
        {rows.length === 0 && (
          <li className="px-5 py-8 text-center text-muted-foreground">
            No medications yet. Add the first one above.
          </li>
        )}
        {rows.map((m) => (
          <li key={m.id} className="flex items-center justify-between gap-4 px-5 py-3">
            <span className="truncate font-medium text-foreground">{m.name}</span>
            <span className="shrink-0 text-muted-foreground">{m.strength}</span>
          </li>
        ))}
      </ul>
    </section>
  );
}
