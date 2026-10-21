"use client";

import { useState } from "react";
import Link from "next/link";
import { ArrowUpRight, CalendarDays, Info, Stethoscope } from "lucide-react";
import {
  Button,
  Input,
  Popover,
  PopoverContent,
  PopoverTrigger,
  Tabs,
  TabsList,
  TabsTrigger,
} from "@tesserix/web";
import {
  dashboardFiltersSchema,
  dashboardPeriod,
  insightsSchema,
  insightsSearch,
  useApiQuery,
  usePermissions,
  type DashboardFilters,
} from "@helivanta/api";
import { Field, FormSelectField, PageHeading, SelectControl, useZodForm } from "@helivanta/ui";
import { TrendChart, type ChartCategory } from "./trend-chart";
import { CohortBreakdown } from "./cohort-breakdown";
import { WorkforceInsights } from "./workforce-insights";

const clinicalCategories: ChartCategory[] = [
  { key: "OPD", label: "OPD registrations", color: "var(--hms-accent)" },
  { key: "IPD", label: "IPD registrations", color: "var(--hms-violet)" },
];
const laboratoryCategories: ChartCategory[] = [
  { key: "pending", label: "Pending", color: "var(--hms-amber)" },
  { key: "completed", label: "Completed", color: "var(--hms-violet)" },
];
const pharmacyCategories: ChartCategory[] = [
  { key: "pending", label: "Pending", color: "var(--hms-amber)" },
  { key: "dispensed", label: "Dispensed", color: "var(--hms-accent)" },
];
const periods = [
  { key: "today", label: "Today" },
  { key: "week", label: "This week" },
  { key: "month", label: "This month" },
  { key: "year", label: "This year" },
  { key: "fiveYears", label: "Last 5 years" },
] as const;

export function OperationalDashboard({ now }: { now?: Date }) {
  const { can } = usePermissions();
  const [filters, setFilters] = useState(() => dashboardPeriod(now ?? new Date(), "month", "UTC"));
  const form = useZodForm(dashboardFiltersSchema, filters);
  const [department, setDepartment] = useState("all");
  const [mode, setMode] = useState<"line" | "bar">("line");
  const [view, setView] = useState<"overview" | "clinical" | "services" | "team">("overview");
  const [rangeOpen, setRangeOpen] = useState(false);
  // The Group by menu renders its options in a portal, outside the popover, so
  // the popover sees choosing one as a click outside. Ignore close requests
  // while a listbox is open; Escape and a real outside click still close it.
  const changeRange = (open: boolean) => {
    if (!open && document.querySelector("[role=listbox]")) return;
    setRangeOpen(open);
  };
  const hasAnalytics =
    can("medicore.visit.read") || can("pharmacy.dispense.read") || can("lab.order.read");
  const views = [
    { key: "overview", label: "Overview", allowed: true },
    { key: "clinical", label: "Patient activity", allowed: can("medicore.visit.read") },
    {
      key: "services",
      label: "Diagnostics & pharmacy",
      allowed: can("lab.order.read") || can("pharmacy.dispense.read"),
    },
    { key: "team", label: "Care teams", allowed: true },
  ] as const;
  const activeView = views.some((item) => item.key === view && item.allowed) ? view : "overview";
  const showClinical = activeView === "overview" || activeView === "clinical";
  const showServices = activeView === "overview" || activeView === "services";
  const showTeam = activeView === "overview" || activeView === "team";
  return (
    <div className="mb-10 space-y-6">
      <PageHeading
        title="Hospital overview"
        actions={
          hasAnalytics &&
          activeView !== "team" && (
            <div
              role="group"
              aria-label="Reporting period"
              className="flex flex-wrap items-center gap-2"
            >
              <div className="flex rounded-md border bg-card p-0.5">
                {periods.map((period) => (
                  <Button
                    key={period.key}
                    size="sm"
                    variant="ghost"
                    className="h-8 px-3 text-muted-foreground hover:text-foreground"
                    onClick={() => {
                      const next = dashboardPeriod(now ?? new Date(), period.key, filters.timezone);
                      form.reset(next);
                      setFilters(next);
                    }}
                  >
                    {period.label}
                  </Button>
                ))}
              </div>
              <div className="relative">
                <Popover open={rangeOpen} onOpenChange={changeRange}>
                  <PopoverTrigger
                    aria-label={`Custom range, ${filters.from} to ${filters.to}`}
                    className="inline-flex h-9 items-center gap-2 rounded-md border bg-card px-3 text-sm tabular-nums transition-colors hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
                  >
                    <CalendarDays className="h-4 w-4 text-muted-foreground" aria-hidden="true" />
                    {filters.from} – {filters.to}
                  </PopoverTrigger>
                  <PopoverContent align="end" className="w-[min(32rem,90vw)] bg-popover">
                    <form
                      noValidate
                      aria-label="Dashboard filters"
                      onSubmit={form.handleSubmit((next) => {
                        setFilters(next);
                        setRangeOpen(false);
                      })}
                      className="grid gap-4 sm:grid-cols-2"
                    >
                      <Field
                        id="insights-from"
                        label="From"
                        error={form.formState.errors.from?.message}
                      >
                        <Input id="insights-from" type="date" {...form.register("from")} />
                      </Field>
                      <Field id="insights-to" label="To" error={form.formState.errors.to?.message}>
                        <Input id="insights-to" type="date" {...form.register("to")} />
                      </Field>
                      <FormSelectField
                        control={form.control}
                        name="interval"
                        id="insights-interval"
                        label="Group by"
                        options={[
                          { value: "day", label: "Day" },
                          { value: "week", label: "Week" },
                          { value: "month", label: "Month" },
                          { value: "year", label: "Year" },
                        ]}
                      />
                      <Field
                        id="insights-timezone"
                        label="Timezone"
                        error={form.formState.errors.timezone?.message}
                      >
                        <Input
                          id="insights-timezone"
                          list="insights-timezones"
                          {...form.register("timezone")}
                        />
                        <datalist id="insights-timezones">
                          {[
                            "UTC",
                            "Asia/Kolkata",
                            "Australia/Melbourne",
                            "Europe/London",
                            "America/New_York",
                          ].map((zone) => (
                            <option key={zone} value={zone} />
                          ))}
                        </datalist>
                      </Field>
                      <p className="text-xs text-muted-foreground sm:col-span-2">
                        Inclusive dates · weeks start Monday · first and last buckets may be
                        partial.
                      </p>
                      <div className="flex justify-end gap-2 sm:col-span-2">
                        <Button type="button" variant="ghost" onClick={() => setRangeOpen(false)}>
                          Cancel
                        </Button>
                        <Button type="submit">Apply</Button>
                      </div>
                    </form>
                  </PopoverContent>
                </Popover>
              </div>
            </div>
          )
        }
      />
      <Tabs value={activeView} onValueChange={(next) => setView(next as typeof view)}>
        <TabsList aria-label="Dashboard views">
          {views
            .filter((item) => item.allowed)
            .map((item) => (
              <TabsTrigger key={item.key} value={item.key}>
                {item.label}
              </TabsTrigger>
            ))}
        </TabsList>
      </Tabs>
      {showClinical && can("medicore.visit.read") && (
        <AnalyticsPanel
          title="Patient activity"
          module="medicore"
          filters={filters}
          categories={
            department === "all"
              ? clinicalCategories
              : clinicalCategories.filter((c) => c.key === department)
          }
          mode={mode}
          onModeChange={setMode}
          description="Visit registrations by creation date. IPD registrations count admission visits, not unique patients, occupied beds or length of stay."
          href="/medicore/ipd"
        >
          <div className="w-full sm:w-56">
            <Field id="insights-department" label="Visit type">
              <SelectControl
                id="insights-department"
                value={department}
                onValueChange={setDepartment}
                options={[
                  { value: "all", label: "OPD & IPD" },
                  { value: "OPD", label: "Outpatients (OPD)" },
                  { value: "IPD", label: "Inpatients (IPD)" },
                ]}
              />
            </Field>
          </div>
        </AnalyticsPanel>
      )}
      {showServices && (
        <div className="grid items-start gap-6 xl:grid-cols-2">
          {can("lab.order.read") && (
            <AnalyticsPanel
              title="Laboratory"
              module="lab"
              filters={filters}
              categories={laboratoryCategories}
              mode={mode}
              onModeChange={setMode}
              description="Current status of orders created in this period. Completed does not mean resulted during this period."
              href="/lab"
            />
          )}
          {can("pharmacy.dispense.read") && (
            <AnalyticsPanel
              title="Pharmacy"
              module="pharmacy"
              filters={filters}
              categories={pharmacyCategories}
              mode={mode}
              onModeChange={setMode}
              description="Current status of dispense tasks created in this period. Dispensed does not mean dispensed during this period."
              href="/pharmacy"
            />
          )}
        </div>
      )}
      {showTeam && <WorkforceInsights />}
      {showClinical && can("medicore.visit.read") && (
        <aside className="flex gap-3 rounded-lg border bg-card p-4 text-sm">
          <Stethoscope
            className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground"
            aria-hidden="true"
          />
          <div>
            <h3 className="font-semibold">Appointments & doctor workload</h3>
            <p className="mt-1 leading-6 text-muted-foreground">
              Appointment scheduling and doctor assignments are not recorded in the current clinical
              module. Appointment utilisation, no-shows and doctor workload will need those source
              records before they can be reported reliably.
            </p>
          </div>
        </aside>
      )}
    </div>
  );
}

function AnalyticsPanel({
  title,
  module,
  filters,
  categories,
  mode,
  onModeChange,
  description,
  href,
  children,
}: {
  title: string;
  module: string;
  filters: DashboardFilters;
  categories: ChartCategory[];
  mode: "line" | "bar";
  onModeChange: (mode: "line" | "bar") => void;
  description: string;
  href: string;
  children?: React.ReactNode;
}) {
  const query = useApiQuery<unknown>(
    ["insights", module, filters],
    `/${module}/insights?${insightsSearch(filters)}`,
  );
  const parsed = insightsSchema.safeParse(query.data);
  const data = parsed.success ? parsed.data : undefined;
  const total = data ? categories.reduce((sum, c) => sum + (data.totals[c.key] ?? 0), 0) : 0;
  const calendarDays = data
    ? (Date.parse(`${data.to}T00:00:00Z`) - Date.parse(`${data.from}T00:00:00Z`)) / 86_400_000 + 1
    : 1;
  return (
    <section aria-label={title} className="min-w-0 rounded-lg border bg-card shadow-xs">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b px-5 py-4">
        <div className="flex items-center gap-1.5">
          <h3 className="text-base font-semibold">{title}</h3>
          {/* The definition is #1234's measurement contract: hidden from the
              layout, never removed. A click (not hover) opens it, so it works
              on touch and by keyboard. */}
          <div className="relative">
            <Popover>
              <PopoverTrigger
                aria-label={`About ${title}`}
                className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                <Info className="h-4 w-4" aria-hidden="true" />
              </PopoverTrigger>
              <PopoverContent align="start" className="w-72 bg-popover text-sm leading-6">
                {description}
              </PopoverContent>
            </Popover>
          </div>
        </div>
        <div className="flex items-center gap-3">
          <div
            role="group"
            aria-label={`${title} chart style`}
            className="flex rounded-md border p-0.5"
          >
            {(["line", "bar"] as const).map((choice) => (
              <Button
                key={choice}
                size="sm"
                variant={mode === choice ? "secondary" : "ghost"}
                aria-pressed={mode === choice}
                className="h-7 px-2.5 text-xs"
                onClick={() => onModeChange(choice)}
              >
                {choice === "line" ? "Lines" : "Bars"}
              </Button>
            ))}
          </div>
          <Link
            href={href}
            className="inline-flex items-center gap-1 text-sm font-medium text-(--hms-accent) hover:underline"
          >
            Open workspace
            <ArrowUpRight className="h-3.5 w-3.5" aria-hidden="true" />
          </Link>
        </div>
      </div>
      <div className="p-5 sm:p-6">
        {query.isPending ? (
          <div role="status" className="py-12 text-center text-sm text-muted-foreground">
            Loading {title.toLowerCase()} insights…
          </div>
        ) : query.isError || !data ? (
          <div role="alert" className="py-8 text-sm">
            <p>Could not load {title.toLowerCase()} insights.</p>
            <Button
              className="mt-3"
              variant="outline"
              aria-label={`Retry ${title}`}
              onClick={() => void query.refetch()}
            >
              Try again
            </Button>
          </div>
        ) : (
          <>
            <div className="mb-6 flex flex-wrap items-end justify-between gap-5">
              <div className="grid w-full divide-y rounded-md border sm:grid-cols-2 sm:divide-y-0 sm:divide-x xl:grid-cols-4">
                <Metric
                  label={module === "medicore" ? "Total registrations" : "Tasks in period"}
                  value={total}
                />
                {module === "medicore" && (
                  <Metric
                    label="Registrations / day"
                    value={total / calendarDays}
                    fractionDigits={1}
                  />
                )}
                {categories.map((category) => (
                  <Metric
                    key={category.key}
                    label={category.label}
                    value={data.totals[category.key] ?? 0}
                  />
                ))}
              </div>
              {children}
            </div>
            {total === 0 ? (
              <p
                role="status"
                className="mb-6 rounded-md bg-muted p-4 text-sm text-muted-foreground"
              >
                No {title.toLowerCase()} records in this period. Choose another date range to
                explore your activity.
              </p>
            ) : null}
            <div
              className={
                module === "medicore"
                  ? "grid items-start gap-6 xl:grid-cols-[minmax(0,1fr)_17rem]"
                  : "space-y-6"
              }
            >
              <div className="min-w-0">
                <TrendChart title={title} data={data} categories={categories} mode={mode} />
              </div>
              <CohortBreakdown
                title={module === "medicore" ? "Visit mix" : `${title} cohort status`}
                totals={data.totals}
                categories={categories}
                presentation={module === "medicore" ? "ring" : "bar"}
              />
            </div>
            <div className="mt-4 flex flex-wrap justify-between gap-2 text-xs text-muted-foreground">
              <span>
                {data.from} – {data.to} · {data.timezone}
              </span>
              <span>
                {query.isFetching ? "Refreshing…" : "Queried"}{" "}
                <time dateTime={data.as_at}>
                  {new Date(data.as_at).toLocaleString("en", {
                    timeZone: data.timezone,
                    dateStyle: "medium",
                    timeStyle: "short",
                  })}
                </time>
                <button
                  type="button"
                  onClick={() => void query.refetch()}
                  disabled={query.isFetching}
                  className="ml-3 underline underline-offset-4 disabled:opacity-50"
                  aria-label={`Refresh ${title}`}
                >
                  Refresh
                </button>
              </span>
            </div>
          </>
        )}
      </div>
    </section>
  );
}

function Metric({
  label,
  value,
  fractionDigits = 0,
}: {
  label: string;
  value: number;
  fractionDigits?: number;
}) {
  return (
    <div className="min-w-0 px-4 py-3">
      <p className="text-xs font-medium text-muted-foreground">{label}</p>
      <p className="mt-1 text-2xl font-semibold tabular-nums">
        {value.toLocaleString(undefined, {
          minimumFractionDigits: fractionDigits,
          maximumFractionDigits: fractionDigits,
        })}
      </p>
    </div>
  );
}
