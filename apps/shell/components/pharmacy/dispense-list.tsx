"use client";

import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import { CheckCheck, Pill, Search, type LucideIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { Can, PAGE_SIZE, apiFetch, useApiMutation, useApiPageQuery } from "@helivanta/api";
import { Field, formatTime, PageTabs, Pager, Panel, QueryState, useZodForm } from "@helivanta/ui";

type Dispense = {
  id: string;
  visit_id: string;
  patient_name: string;
  medication: string;
  status: "pending" | "dispensed";
  dispensed_at: string | null;
  created_at: string;
};

// No default: each dispense records what the pharmacist typed for that row (#1272).
const dispenseSchema = z.object({
  medication: z.string().trim().min(1, "Medication is required").max(200),
});

type Tab = "waiting" | "dispensed";

const TABS: Record<
  Tab,
  { label: string; noun: string; empty: { icon: LucideIcon; title: string; hint?: string } }
> = {
  waiting: {
    label: "Waiting",
    noun: "waiting dispenses",
    empty: {
      icon: Pill,
      title: "Nothing waiting",
      hint: "Prescriptions from MediCore visits appear here.",
    },
  },
  dispensed: {
    label: "Dispensed",
    noun: "dispensed medicines",
    empty: { icon: CheckCheck, title: "Nothing dispensed yet" },
  },
};

// Waiting and Dispensed are tabs, each its own counted route (#1295 D8):
// Waiting oldest first, so the longest wait is at the top of page 1, and
// Dispensed newest first by hand-over time. The tab, page and cursor live
// in the URL (D5), so back, forward and a shared link land on the same page.
export function DispenseList() {
  const pathname = usePathname();
  const router = useRouter();
  const params = useSearchParams();
  const tab: Tab = params.get("tab") === "dispensed" ? "dispensed" : "waiting";
  const page = Math.max(1, Number.parseInt(params.get("page") ?? "1", 10) || 1);
  const cursor = page > 1 ? params.get("cursor") : null;
  // Waiting only (D9): Dispensed history is unbounded and the server refuses it.
  const q = tab === "waiting" ? (params.get("q") ?? "").trim() : "";

  // The cursor that opened each page this visit, per tab. Not persisted:
  // after a reload the way back is unknown, and the pager says so (D5).
  // Recorded with React's set-state-during-render pattern, so the record is
  // in step with the page being rendered.
  const [visited, setVisited] = useState<Record<Tab, (string | null)[]>>({
    waiting: [null],
    dispensed: [null],
  });
  if (visited[tab][page - 1] !== cursor) {
    const pages = [...visited[tab]];
    pages[page - 1] = cursor;
    setVisited({ ...visited, [tab]: pages });
  }

  const href = (p: number, c: string | null) => {
    const search = new URLSearchParams({ tab });
    if (q) search.set("q", q);
    if (p > 1 && c) {
      search.set("page", String(p));
      search.set("cursor", c);
    }
    return `${pathname}?${search}`;
  };
  const first = href(1, null);
  // A new search starts at page 1 of the Waiting tab.
  const hrefFor = (value: string) => {
    const search = new URLSearchParams({ tab: "waiting" });
    if (value.trim()) search.set("q", value.trim());
    return `${pathname}?${search}`;
  };
  const back = page > 1 ? visited[tab][page - 2] : undefined;
  const previousHref = back === undefined ? null : back === null ? first : href(page - 1, back);

  const dispenses = useApiPageQuery<Dispense>(
    ["dispenses", tab],
    `/pharmacy/dispenses/${tab}${q ? `?q=${encodeURIComponent(q)}` : ""}`,
    cursor,
    {
      poll: true,
    },
  );

  // A later page can empty while open (its last row dispensed). Step back
  // rather than show "of N" over nothing.
  const emptied =
    !dispenses.isPending && !dispenses.isError && dispenses.items.length === 0 && page > 1;
  useEffect(() => {
    if (emptied) router.replace(previousHref ?? first);
  }, [emptied, previousHref, first, router]);

  return (
    <Panel flush>
      <PageTabs
        label="Dispense queue"
        current={tab}
        tabs={(Object.keys(TABS) as Tab[]).map((key) => ({
          key,
          label: TABS[key].label,
          href: `${pathname}?tab=${key}`,
        }))}
      />
      {tab === "waiting" && (
        <WaitingSearch key={q} initial={q} onSearch={(value) => router.replace(hrefFor(value))} />
      )}
      <QueryState
        query={dispenses}
        label={TABS[tab].noun}
        empty={
          q
            ? { icon: Search, title: "No waiting patient matches", hint: `Nothing for “${q}”.` }
            : TABS[tab].empty
        }
      >
        {(items) => (
          <ul className="divide-y text-sm">
            {items.map((d) => (
              // Below sm a waiting row stacks: the form needs the full width (#1293).
              <li
                key={d.id}
                className="flex flex-col gap-3 px-5 py-3 sm:flex-row sm:items-center sm:justify-between sm:gap-4"
              >
                <div className="min-w-0">
                  <span className="block truncate font-medium text-foreground">
                    {d.patient_name}
                  </span>
                  <span className="mt-0.5 block text-muted-foreground">
                    {d.status === "dispensed"
                      ? d.medication
                      : `Waiting since ${formatTime(d.created_at)}`}
                  </span>
                </div>
                {d.status === "pending" ? (
                  <Can permission="pharmacy.dispense.fulfil">
                    <DispenseForm id={d.id} />
                  </Can>
                ) : (
                  <time className="shrink-0 tabular-nums text-muted-foreground">
                    {formatTime(d.dispensed_at ?? "")}
                  </time>
                )}
              </li>
            ))}
          </ul>
        )}
      </QueryState>
      {!dispenses.isPending && !emptied && (
        <Pager
          page={page}
          pageSize={PAGE_SIZE}
          shown={dispenses.items.length}
          total={dispenses.total}
          previousHref={previousHref}
          nextHref={dispenses.nextCursor ? href(page + 1, dispenses.nextCursor) : null}
          firstHref={first}
        />
      )}
    </Panel>
  );
}

const searchSchema = z.object({
  q: z.string().trim().max(100, "Search is at most 100 characters"),
});

function WaitingSearch({ initial, onSearch }: { initial: string; onSearch: (q: string) => void }) {
  const form = useZodForm(searchSchema, { q: initial });
  const error = form.formState.errors.q?.message;
  return (
    <form
      noValidate
      role="search"
      onSubmit={form.handleSubmit((values) => onSearch(values.q))}
      className="flex items-end gap-2 border-b px-5 py-3"
    >
      <div className="min-w-0 flex-1">
        <Field id="dispense-search" label="Search waiting patients" error={error}>
          <Input
            id="dispense-search"
            type="search"
            placeholder="Patient name"
            aria-invalid={!!error}
            aria-describedby={error ? "dispense-search-error" : undefined}
            {...form.register("q")}
          />
        </Field>
      </div>
      <Button type="submit" variant="outline">
        Search
      </Button>
    </form>
  );
}

function DispenseForm({ id }: { id: string }) {
  const form = useZodForm(dispenseSchema, { medication: "" });
  const dispense = useApiMutation(
    (values: z.infer<typeof dispenseSchema>) =>
      apiFetch("/pharmacy/dispenses/" + id + "/dispense", {
        method: "POST",
        body: JSON.stringify(values),
      }),
    {
      successToast: "Dispensed",
      invalidate: [["dispenses"]],
    },
  );
  const fieldId = `dispense-medication-${id}`;

  return (
    <form
      noValidate
      onSubmit={form.handleSubmit((values) => dispense.mutate(values))}
      className="flex w-full items-start gap-2 sm:w-auto sm:shrink-0"
    >
      <div className="min-w-0 flex-1 sm:w-56 sm:flex-none">
        <Field id={fieldId} label="Medication" error={form.formState.errors.medication?.message}>
          <Input id={fieldId} {...form.register("medication")} />
        </Field>
      </div>
      <Button type="submit" className="mt-6" disabled={dispense.isPending}>
        {dispense.isPending ? "Dispensing…" : "Dispense"}
      </Button>
    </form>
  );
}
