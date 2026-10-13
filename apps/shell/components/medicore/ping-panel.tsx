"use client";

import { Activity } from "lucide-react";
import { Button } from "@tesserix/web";
import { apiFetch, useApiMutation, useApiPagedQuery } from "@helivanta/api";
import { EmptyState, LoadMore, formatTime } from "@helivanta/ui";

type Ping = { id: string; message: string; created_at: string };

export function PingPanel({ department }: { department: string }) {
  const pings = useApiPagedQuery<Ping>(["pings"], "/reference/pings");

  const sendPing = useApiMutation(
    () =>
      apiFetch<{ id: string }>("/reference/ping", {
        method: "POST",
        body: JSON.stringify({ message: `${department} ping` }),
      }),
    {
      successToast: "Ping sent",
      invalidate: [["pings"]],
    },
  );

  return (
    <section className="max-w-2xl rounded-lg border bg-card">
      <div className="flex items-center justify-between border-b px-5 py-4">
        <div>
          <h2 className="text-sm font-semibold text-foreground">Platform check</h2>
          <p className="mt-0.5 text-sm text-muted-foreground">
            Round-trips the event pipeline end to end.
          </p>
        </div>
        <Button variant="outline" onClick={() => sendPing.mutate()} disabled={sendPing.isPending}>
          {sendPing.isPending ? "Pinging…" : `Ping from ${department}`}
        </Button>
      </div>
      <ul className="divide-y text-sm">
        {pings.items.length === 0 && (
          <li>
            <EmptyState icon={Activity} title="No activity yet" />
          </li>
        )}
        {pings.items.map((p) => (
          <li key={p.id} className="flex items-center justify-between gap-4 px-5 py-3">
            <span className="truncate text-foreground">{p.message}</span>
            <time className="shrink-0 tabular-nums text-muted-foreground">
              {formatTime(p.created_at)}
            </time>
          </li>
        ))}
      </ul>
      <LoadMore hasMore={pings.hasMore} isLoading={pings.isFetchingMore} onClick={pings.loadMore} />
    </section>
  );
}
