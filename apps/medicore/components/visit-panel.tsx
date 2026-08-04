"use client";

import { useCallback, useEffect, useState } from "react";
import { Badge, Button, Input } from "@tesserix/web";

type Visit = {
  id: string;
  patient_name: string;
  department: string;
  status: string;
  created_at: string;
};

export function VisitPanel({ department }: { department: "OPD" | "IPD" }) {
  const [visits, setVisits] = useState<Visit[]>([]);
  const [patientName, setPatientName] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    try {
      const res = await fetch("/api/v1/medicore/visits");
      if (!res.ok) throw new Error(String(res.status));
      const body = await res.json();
      setVisits(body.data ?? []);
      setError(null);
    } catch {
      setError("Could not load visits. Is the API running?");
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  async function createVisit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    try {
      const res = await fetch("/api/v1/medicore/visits", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ patient_name: patientName, department }),
      });
      if (!res.ok) throw new Error(String(res.status));
      setPatientName("");
      await load();
    } catch {
      setError("Could not create visit.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="max-w-2xl rounded-lg border bg-card">
      <div className="border-b px-5 py-4">
        <h2 className="text-sm font-semibold text-foreground">Visits</h2>
        <p className="mt-0.5 text-sm text-muted-foreground">
          New visits open a pending dispense in Pharmacy and a pending order in Lab.
        </p>
      </div>
      <form onSubmit={createVisit} className="flex flex-wrap items-end gap-3 border-b px-5 py-4">
        <label className="flex min-w-56 flex-1 flex-col gap-1.5 text-sm font-medium">
          Patient name
          <Input
            value={patientName}
            onChange={(e) => setPatientName(e.target.value)}
            required
            maxLength={200}
            placeholder="e.g. Asha Rao"
          />
        </label>
        <Button type="submit" disabled={busy}>
          {busy ? "Creating…" : "Create visit"}
        </Button>
      </form>
      {error && (
        <p role="alert" className="border-b px-5 py-3 text-sm text-destructive">
          {error}
        </p>
      )}
      <ul className="divide-y text-sm">
        {visits.length === 0 && (
          <li className="px-5 py-8 text-center text-muted-foreground">
            No visits yet. Create the first one above.
          </li>
        )}
        {visits.map((v) => (
          <li key={v.id} className="flex items-center justify-between gap-4 px-5 py-3">
            <div className="flex min-w-0 items-center gap-3">
              <span className="truncate font-medium text-foreground">{v.patient_name}</span>
              <Badge variant="secondary">{v.department}</Badge>
            </div>
            <time className="shrink-0 tabular-nums text-muted-foreground">
              {new Date(v.created_at).toLocaleTimeString()}
            </time>
          </li>
        ))}
      </ul>
    </section>
  );
}
