"use client";

import { useState } from "react";
import Link from "next/link";
import { Button, Badge } from "@tesserix/web";
import {
  Can,
  useApiPagedQuery,
  usePermissions,
  type EmergencyBase,
  type EmergencyCase,
} from "@helivanta/api";
import { LoadMore, formatTime } from "@helivanta/ui";
import { EmergencyBaseAdmin } from "./emergency-base-admin";
import { EmergencyIntake } from "./emergency-intake";
import { EmergencyFleet } from "./emergency-fleet";

export function EmergencyWorkspace() {
  const { can } = usePermissions();
  const bases = useApiPagedQuery<EmergencyBase>(["emergency", "bases"], "/emergency/bases", {
    enabled: can("emergency.base.read"),
  });
  const [selected, setSelected] = useState<string | null>(null);
  const [configure, setConfigure] = useState<string | null>(null);
  const base = bases.items.find((item) => item.id === selected);
  const configured = bases.items.find((item) => item.id === configure);
  return (
    <div className="mx-auto max-w-6xl space-y-6">
      <header>
        <h1 className="text-2xl font-semibold">EmergencyConnect</h1>
        <p className="mt-2 text-muted-foreground">
          Staff intake and case review. No ambulance has been assigned through this workflow.
        </p>
      </header>
      {bases.isPending && can("emergency.base.read") && (
        <p role="status">Loading operating bases…</p>
      )}
      {bases.isError && (
        <div role="alert">
          {bases.error?.message}
          <Button onClick={bases.retry}>Retry bases</Button>
        </div>
      )}
      {!bases.isPending && !bases.isError && bases.items.length === 0 && (
        <p>No operating bases configured in this tenant.</p>
      )}
      <ul className="grid gap-3 sm:grid-cols-2">
        {bases.items.map((item) => (
          <li className="min-w-0 space-y-3 rounded-xl border bg-card p-5" key={item.id}>
            <h2 className="font-semibold">{item.name}</h2>
            <p className="text-sm text-muted-foreground">
              {item.timezone} · {item.access_level ?? "No case access"}
            </p>
            <div className="flex flex-wrap gap-2">
              {item.access_level && can("emergency.case.read") && (
                <Button
                  className="h-auto min-h-11 max-w-full whitespace-normal text-left"
                  variant="outline"
                  onClick={() => setSelected(item.id)}
                >
                  Open {item.name}
                </Button>
              )}
              <Can permission="emergency.base.manage">
                <Button
                  className="h-auto min-h-11 max-w-full whitespace-normal text-left"
                  variant="outline"
                  onClick={() => setConfigure(item.id)}
                >
                  Configure {item.name}
                </Button>
              </Can>
            </div>
          </li>
        ))}
      </ul>
      <LoadMore hasMore={bases.hasMore} isLoading={bases.isFetchingMore} onClick={bases.loadMore} />
      <Can permission="emergency.base.manage">
        <EmergencyBaseAdmin key={configured?.id ?? "new-base"} base={configured} />
        {configured && (
          <Button variant="outline" onClick={() => setConfigure(null)}>
            Configure a new base
          </Button>
        )}
      </Can>
      {base?.access_level && can("emergency.case.read") && (
        <EmergencyBaseCases key={base.id} base={base} />
      )}
    </div>
  );
}
function EmergencyBaseCases({ base }: { base: EmergencyBase }) {
  const { can } = usePermissions();
  const cases = useApiPagedQuery<EmergencyCase>(
    ["emergency", "cases", base.id],
    `/emergency/bases/${base.id}/cases`,
    { poll: true },
  );
  return (
    <section className="space-y-5">
      <h2 className="text-xl font-semibold">{base.name} reports</h2>
      {base.access_level && <EmergencyFleet base={base.id} level={base.access_level} />}
      {(base.access_level === "dispatcher" || base.access_level === "supervisor") &&
        can("emergency.case.intake") && <EmergencyIntake base={base} />}
      {cases.isPending && <p role="status">Loading reports…</p>}
      {cases.isError ? (
        <div role="alert">
          {cases.error?.message}
          <Button onClick={cases.retry}>Retry reports</Button>
        </div>
      ) : (
        <>
          <ul className="divide-y rounded-xl border bg-card">
            {!cases.isPending && cases.items.length === 0 && (
              <li className="p-5">No reports received at this base.</li>
            )}
            {cases.items.map((row) => (
              <li key={row.id} className="flex flex-wrap items-center justify-between gap-3 p-4">
                <div>
                  <Link
                    className="break-all font-medium text-primary underline"
                    href={`/emergency/bases/${base.id}/cases/${row.id}`}
                  >
                    {row.case_number}
                  </Link>
                  <p className="text-sm text-muted-foreground">
                    Received {formatTime(row.created_at)} · {row.requested_service}
                  </p>
                </div>
                <div className="flex gap-2">
                  <Badge variant="secondary">{row.status}</Badge>
                  <Badge variant="outline">Untriaged</Badge>
                </div>
              </li>
            ))}
          </ul>
          <LoadMore
            hasMore={cases.hasMore}
            isLoading={cases.isFetchingMore}
            onClick={cases.loadMore}
          />
        </>
      )}
    </section>
  );
}
