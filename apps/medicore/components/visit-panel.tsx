"use client";

import { useCallback, useEffect, useState } from "react";

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
    <section className="max-w-xl space-y-4">
      <form onSubmit={createVisit} className="flex items-end gap-3">
        <label className="flex flex-col gap-1 text-sm">
          Patient name
          <input
            value={patientName}
            onChange={(e) => setPatientName(e.target.value)}
            required
            maxLength={200}
            className="rounded-md border px-3 py-2"
          />
        </label>
        <button
          type="submit"
          disabled={busy}
          className="rounded-md bg-primary px-3 py-2 text-sm text-primary-foreground disabled:opacity-50"
        >
          {busy ? "Creating…" : "Create visit"}
        </button>
      </form>
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      <ul className="divide-y rounded-md border text-sm">
        {visits.length === 0 && <li className="p-3 text-muted-foreground">No visits yet.</li>}
        {visits.map((v) => (
          <li key={v.id} className="flex justify-between p-3">
            <span>
              {v.patient_name} <span className="text-muted-foreground">({v.department})</span>
            </span>
            <time className="text-muted-foreground">{new Date(v.created_at).toLocaleTimeString()}</time>
          </li>
        ))}
      </ul>
    </section>
  );
}
