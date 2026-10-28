"use client";

import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import { CheckCheck, Pill, Search, type LucideIcon } from "lucide-react";
import { useRouter, useSearchParams } from "next/navigation";
import { Can, PAGE_SIZE, apiFetch, useApiMutation, useApiPageQuery } from "@helivanta/api";
import { useUrlPage, useStepBackWhenEmpty } from "@/lib/use-url-page";
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
  const router = useRouter();
  const params = useSearchParams();
  const tab: Tab = params.get("tab") === "dispensed" ? "dispensed" : "waiting";
  // Waiting only (D9): Dispensed history is unbounded and the server refuses it.
  // The search stays in the URL on both tabs, so changing tab keeps it (D7),
  // but only Waiting is searched.
  const search = (params.get("q") ?? "").trim();
  const q = tab === "waiting" ? search : "";
  const url = useUrlPage({ tab, q });

  const dispenses = useApiPageQuery<Dispense>(
    ["dispenses", tab],
    `/pharmacy/dispenses/${tab}${q ? `?q=${encodeURIComponent(q)}` : ""}`,
    url.cursor,
    {
      poll: true,
    },
  );
  const emptied = useStepBackWhenEmpty(dispenses, url);

  return (
    <Panel flush>
      <PageTabs
        label="Dispense queue"
        current={tab}
        tabs={(Object.keys(TABS) as Tab[]).map((key) => ({
          key,
          label: TABS[key].label,
          href: url.hrefFor({ tab: key, q: search }),
        }))}
      />
      {tab === "waiting" && (
        <WaitingSearch
          key={q}
          initial={q}
          onSearch={(value) => router.replace(url.hrefFor({ tab: "waiting", q: value.trim() }))}
        />
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
          page={url.page}
          pageSize={PAGE_SIZE}
          offset={dispenses.offset}
          shown={dispenses.items.length}
          total={dispenses.total}
          previousHref={url.previousHref}
          nextHref={url.nextHref(dispenses.nextCursor)}
          firstHref={url.first}
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
