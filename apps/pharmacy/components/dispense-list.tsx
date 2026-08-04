"use client";

import { useCallback, useEffect, useState } from "react";

type Dispense = {
  id: string;
  visit_id: string;
  patient_name: string;
  medication: string;
  status: "pending" | "dispensed";
  dispensed_at: string | null;
  created_at: string;
};

export function DispenseList() {
  const [rows, setRows] = useState<Dispense[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [busyID, setBusyID] = useState<string | null>(null);
  const [medication, setMedication] = useState("Paracetamol 500mg");

  const load = useCallback(async () => {
    try {
      const res = await fetch("/api/v1/pharmacy/dispenses");
      if (!res.ok) throw new Error(String(res.status));
      const body = await res.json();
      setRows(body.data ?? []);
      setError(null);
    } catch {
      setError("Could not load dispenses. Is the API running?");
    }
  }, []);

  useEffect(() => {
    void load();
    const timer = setInterval(() => void load(), 3000);
    return () => clearInterval(timer);
  }, [load]);

  async function dispense(id: string) {
    setBusyID(id);
    try {
      const res = await fetch(`/api/v1/pharmacy/dispenses/${id}/dispense`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ medication }),
      });
      if (!res.ok) throw new Error(String(res.status));
      await load();
    } catch {
      setError("Dispense failed.");
    } finally {
      setBusyID(null);
    }
  }

  return (
    <section className="max-w-3xl space-y-4">
      <label className="flex max-w-sm flex-col gap-1 text-sm">
        Medication
        <input
          value={medication}
          onChange={(e) => setMedication(e.target.value)}
          className="rounded-md border px-3 py-2"
        />
      </label>
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      <ul className="divide-y rounded-md border text-sm">
        {rows.length === 0 && <li className="p-3 text-muted-foreground">No dispense tasks yet.</li>}
        {rows.map((d) => (
          <li key={d.id} className="flex items-center justify-between gap-4 p-3">
            <div className="min-w-0">
              <div className="font-medium">{d.patient_name}</div>
              <div className="text-muted-foreground">
                {d.status === "dispensed" ? `Dispensed ${d.medication}` : "Pending dispense"}
              </div>
            </div>
            {d.status === "pending" ? (
              <button
                onClick={() => dispense(d.id)}
                disabled={busyID === d.id}
                className="rounded-md bg-primary px-3 py-2 text-primary-foreground disabled:opacity-50"
              >
                {busyID === d.id ? "Dispensing…" : "Dispense"}
              </button>
            ) : (
              <span className="text-muted-foreground">Done</span>
            )}
          </li>
        ))}
      </ul>
    </section>
  );
}
