"use client";

import { useCallback, useEffect, useState } from "react";
import { Badge, Button, Input } from "@tesserix/web";

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

  const pending = rows.filter((d) => d.status === "pending").length;

  return (
    <section className="max-w-3xl rounded-lg border bg-card">
      <div className="flex flex-wrap items-end justify-between gap-3 border-b px-5 py-4">
        <div>
          <h2 className="text-sm font-semibold text-foreground">Dispense queue</h2>
          <p className="mt-0.5 text-sm text-muted-foreground">
            {pending === 0 ? "Nothing waiting right now." : `${pending} pending dispense${pending === 1 ? "" : "s"}.`}
          </p>
        </div>
        <label className="flex flex-col gap-1.5 text-sm font-medium">
          Medication
          <Input
            value={medication}
            onChange={(e) => setMedication(e.target.value)}
            className="w-56"
          />
        </label>
      </div>
      {error && (
        <p role="alert" className="border-b px-5 py-3 text-sm text-destructive">
          {error}
        </p>
      )}
      <ul className="divide-y text-sm">
        {rows.length === 0 && (
          <li className="px-5 py-8 text-center text-muted-foreground">
            No dispense tasks yet. They appear here when a visit is created in MediCore.
          </li>
        )}
        {rows.map((d) => (
          <li key={d.id} className="flex items-center justify-between gap-4 px-5 py-3">
            <div className="min-w-0">
              <div className="flex items-center gap-3">
                <span className="truncate font-medium text-foreground">{d.patient_name}</span>
                <Badge variant={d.status === "pending" ? "secondary" : "outline"}>
                  {d.status === "pending" ? "Pending" : "Dispensed"}
                </Badge>
              </div>
              <div className="mt-0.5 text-muted-foreground">
                {d.status === "dispensed" ? d.medication : "Awaiting dispense"}
              </div>
            </div>
            {d.status === "pending" ? (
              <Button onClick={() => dispense(d.id)} disabled={busyID === d.id}>
                {busyID === d.id ? "Dispensing…" : "Dispense"}
              </Button>
            ) : (
              <time className="shrink-0 tabular-nums text-muted-foreground">
                {d.dispensed_at ? new Date(d.dispensed_at).toLocaleTimeString() : ""}
              </time>
            )}
          </li>
        ))}
      </ul>
    </section>
  );
}
