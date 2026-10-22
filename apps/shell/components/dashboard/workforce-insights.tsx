"use client";

import { useState } from "react";
import Link from "next/link";
import { Button } from "@tesserix/web";
import {
  JOB_LABELS,
  staffInsightsSchema,
  useApiPagedQuery,
  useApiQuery,
  type Hospital,
} from "@helivanta/api";
import { Field, LoadMore, Panel, SelectControl } from "@helivanta/ui";

export function WorkforceInsights() {
  const hospitals = useApiPagedQuery<Hospital>(["workforce", "hospitals"], "/workforce/hospitals");
  const [selected, setSelected] = useState("");
  const hospital = hospitals.items.find((item) => item.id === selected) ?? hospitals.items[0];
  return (
    <Panel aria-label="Doctors & care teams">
      <div className="mb-6 flex flex-wrap items-start justify-between gap-4">
        <div className="flex items-start gap-3">
          <div>
            <h3 className="text-lg font-semibold tracking-tight">Doctors & care teams</h3>
            <p className="mt-1 text-xs leading-5 text-muted-foreground">
              Current hospital staff snapshot. Historical date filters above do not apply.
            </p>
          </div>
        </div>
        <Link href="/workforce" className="text-xs font-medium text-(--hms-accent)">
          Open staff directory ↗
        </Link>
      </div>
      {hospitals.isPending ? (
        <p role="status">Loading hospital access…</p>
      ) : hospitals.isError ? (
        <div role="alert">
          Could not load hospital access.{" "}
          <Button variant="outline" onClick={hospitals.retry}>
            Retry hospitals
          </Button>
        </div>
      ) : !hospital ? (
        <p className="rounded-md bg-muted p-4 text-sm text-muted-foreground">
          No hospital staff access. Ask your administrator for an explicit hospital grant to view
          team insights.
        </p>
      ) : (
        <>
          <div className="mb-5 max-w-sm">
            <Field id="insights-hospital" label="Staff hospital">
              <SelectControl
                id="insights-hospital"
                value={hospital.id}
                onValueChange={setSelected}
                options={hospitals.items.map((item) => ({ value: item.id, label: item.name }))}
              />
            </Field>
          </div>
          <LoadMore
            hasMore={hospitals.hasMore}
            isLoading={hospitals.isFetchingMore}
            onClick={hospitals.loadMore}
          />
          <StaffSnapshot key={hospital.id} hospital={hospital} />
        </>
      )}
    </Panel>
  );
}

function StaffSnapshot({ hospital }: { hospital: Hospital }) {
  const query = useApiQuery<unknown>(
    ["insights", "workforce", hospital.id],
    `/workforce/hospitals/${encodeURIComponent(hospital.id)}/insights`,
  );
  const parsed = staffInsightsSchema.safeParse(query.data);
  // A late or malformed response must never render a different hospital's snapshot.
  const data = parsed.success && parsed.data.hospital_id === hospital.id ? parsed.data : undefined;
  if (query.isPending) return <p role="status">Loading team insights…</p>;
  if (query.isError || !data)
    return (
      <div role="alert" className="py-5 text-sm">
        Could not load team insights. Access may have changed.{" "}
        <Button variant="outline" onClick={() => void query.refetch()}>
          Retry team insights
        </Button>
      </div>
    );
  const active = data.counts
    .filter((row) => row.status === "active")
    .reduce((sum, row) => sum + row.count, 0);
  const inactive = data.counts
    .filter((row) => row.status === "inactive")
    .reduce((sum, row) => sum + row.count, 0);
  const doctors = data.counts
    .filter((row) => row.status === "active" && row.category === "doctor")
    .reduce((sum, row) => sum + row.count, 0);
  const max = Math.max(
    1,
    ...data.counts.filter((row) => row.status === "active").map((row) => row.count),
  );
  return (
    <>
      <div className="mb-6 grid grid-cols-3 gap-4 border-b pb-6">
        {[
          { label: "Active staff", value: active },
          { label: "Active doctors", value: doctors },
          { label: "Inactive staff", value: inactive },
        ].map((metric) => (
          <div key={metric.label}>
            <p className="text-xs text-muted-foreground">{metric.label}</p>
            <p className="mt-2 text-3xl font-semibold tabular-nums">
              {metric.value.toLocaleString()}
            </p>
          </div>
        ))}
      </div>
      {active + inactive === 0 ? (
        <p role="status" className="text-sm text-muted-foreground">
          No staff records for {hospital.name}.
        </p>
      ) : (
        <table aria-label="Staff by profession" className="w-full text-left text-sm">
          <thead>
            <tr className="text-xs text-muted-foreground">
              <th scope="col" className="pb-3 font-medium">
                Profession
              </th>
              <th scope="col" className="pb-3 text-right font-medium">
                Active
              </th>
              <th scope="col" className="pb-3 text-right font-medium">
                Inactive
              </th>
            </tr>
          </thead>
          <tbody>
            {Object.entries(JOB_LABELS).map(([category, label]) => {
              const count = (status: string) =>
                data.counts.find((row) => row.category === category && row.status === status)
                  ?.count ?? 0;
              return (
                <tr key={category} className="border-t">
                  <th scope="row" className="w-3/5 py-3 pr-5 font-normal">
                    <span>{label}</span>
                    <div className="mt-2 h-1.5 rounded-full bg-(--hms-track)" aria-hidden="true">
                      <div
                        className="h-full rounded-full bg-(--hms-violet)"
                        style={{ width: `${(count("active") / max) * 100}%` }}
                      />
                    </div>
                  </th>
                  <td className="text-right tabular-nums">{count("active").toLocaleString()}</td>
                  <td className="text-right tabular-nums text-muted-foreground">
                    {count("inactive").toLocaleString()}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
      <p className="mt-5 text-xs text-muted-foreground">
        Employment status is not attendance or current shift coverage. Queried{" "}
        <time dateTime={data.as_at}>{new Date(data.as_at).toLocaleString()}</time>.{" "}
        <button
          type="button"
          onClick={() => void query.refetch()}
          disabled={query.isFetching}
          className="underline underline-offset-4"
        >
          {query.isFetching ? "Refreshing…" : "Refresh team"}
        </button>
      </p>
    </>
  );
}
