"use client";

import { useState } from "react";
import Link from "next/link";
import {
  Activity,
  ArrowUpRight,
  BedDouble,
  CalendarDays,
  FlaskConical,
  Pill,
  Stethoscope,
} from "lucide-react";
import { Button, Input } from "@tesserix/web";
import {
  dashboardFiltersSchema,
  dashboardPeriod,
  insightsSchema,
  insightsSearch,
  useApiQuery,
  usePermissions,
  type DashboardFilters,
} from "@helivanta/api";
import { Field, FormSelectField, SelectControl, useZodForm } from "@helivanta/ui";
import { TrendChart, type ChartCategory } from "./trend-chart";
import { CohortBreakdown } from "./cohort-breakdown";
import { WorkforceInsights } from "./workforce-insights";
import { ClinicalCoverage } from "./clinical-coverage";

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
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <p className="mb-2 text-xs font-semibold uppercase tracking-[0.18em] text-(--hms-accent)">
            Hospital intelligence
          </p>
          <h2 className="text-3xl font-semibold tracking-tight sm:text-4xl">
            Your care, in perspective.
          </h2>
          <p className="mt-3 max-w-2xl text-sm leading-6 text-muted-foreground">
            Understand patient activity, follow operational queues and see the team behind your
            care.
          </p>
        </div>
        <span className="inline-flex items-center gap-2 rounded-full border bg-card px-3 py-2 text-xs text-muted-foreground">
          <Activity className="h-3.5 w-3.5 text-(--hms-accent)" aria-hidden="true" />
          Current tenant · authorised data
        </span>
      </header>
      <div
        role="group"
        aria-label="Dashboard views"
        className="flex flex-wrap gap-2 rounded-2xl border bg-card p-2"
      >
        {views
          .filter((item) => item.allowed)
          .map((item) => (
            <Button
              key={item.key}
              aria-pressed={activeView === item.key}
              variant={activeView === item.key ? "secondary" : "ghost"}
              className="min-h-11 flex-1 sm:flex-none"
              onClick={() => setView(item.key)}
            >
              {item.label}
            </Button>
          ))}
      </div>
      {hasAnalytics && activeView !== "team" && (
        <section className="rounded-2xl border bg-card p-4 sm:p-5" aria-label="Reporting period">
          <div className="mb-5 flex flex-wrap items-center gap-2">
            <CalendarDays className="mr-1 h-4 w-4 text-muted-foreground" aria-hidden="true" />
            {periods.map((period) => (
              <Button
                key={period.key}
                size="sm"
                className="min-h-11"
                variant="outline"
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
          <form
            noValidate
            aria-label="Dashboard filters"
            onSubmit={form.handleSubmit(setFilters)}
            className="grid items-start gap-4 sm:grid-cols-2 lg:grid-cols-[1fr_1fr_1fr_1.5fr_auto]"
          >
            <Field id="insights-from" label="From" error={form.formState.errors.from?.message}>
              <Input
                id="insights-from"
                className="min-h-11"
                type="date"
                {...form.register("from")}
              />
            </Field>
            <Field id="insights-to" label="To" error={form.formState.errors.to?.message}>
              <Input id="insights-to" className="min-h-11" type="date" {...form.register("to")} />
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
            <Button type="submit" className="mt-6 min-h-11">
              Apply filters
            </Button>
          </form>
          <div className="mt-4 flex flex-wrap items-center justify-between gap-3 border-t pt-4 text-xs text-muted-foreground">
            <p>Inclusive dates · weeks start Monday · first and last buckets may be partial.</p>
            <div
              className="flex gap-1 rounded-lg bg-muted p-1"
              role="group"
              aria-label="Chart presentation"
            >
              {(["line", "bar"] as const).map((choice) => (
                <Button
                  key={choice}
                  size="sm"
                  className="min-h-11"
                  variant={mode === choice ? "secondary" : "ghost"}
                  aria-pressed={mode === choice}
                  onClick={() => setMode(choice)}
                >
                  {choice === "line" ? "Lines" : "Bars"}
                </Button>
              ))}
            </div>
          </div>
        </section>
      )}
      {showClinical && can("medicore.visit.read") && <ClinicalCoverage />}
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
          description="Visit registrations by creation date across the current tenant. The staff hospital selection does not filter these visits. IPD registrations count admission visits, not unique patients, occupied beds or length of stay."
          href="/medicore/ipd"
          icon={BedDouble}
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
              description="Current status of orders created in this period. Completed does not mean resulted during this period."
              href="/lab"
              icon={FlaskConical}
            />
          )}
          {can("pharmacy.dispense.read") && (
            <AnalyticsPanel
              title="Pharmacy"
              module="pharmacy"
              filters={filters}
              categories={pharmacyCategories}
              mode={mode}
              description="Current status of dispense tasks created in this period. Dispensed does not mean dispensed during this period."
              href="/pharmacy"
              icon={Pill}
            />
          )}
        </div>
      )}
      {showTeam && <WorkforceInsights />}
      {showClinical && can("medicore.visit.read") && (
        <aside className="flex gap-4 rounded-2xl border bg-(--hms-violet-tint) p-5 text-sm">
          <Stethoscope className="mt-0.5 h-5 w-5 shrink-0 text-(--hms-violet)" aria-hidden="true" />
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
  description,
  href,
  icon: Icon,
  children,
}: {
  title: string;
  module: string;
  filters: DashboardFilters;
  categories: ChartCategory[];
  mode: "line" | "bar";
  description: string;
  href: string;
  icon: typeof Activity;
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
    <section aria-label={title} className="min-w-0 rounded-2xl border bg-card shadow-sm">
      <div className="flex flex-wrap items-start justify-between gap-4 border-b p-5 sm:p-6">
        <div className="flex items-start gap-3">
          <span className="rounded-xl bg-(--hms-accent-dim) p-2.5 text-(--hms-accent)">
            <Icon className="h-5 w-5" aria-hidden="true" />
          </span>
          <div>
            <h3 className="text-lg font-semibold tracking-tight">{title}</h3>
            <p className="mt-1 max-w-lg text-xs leading-5 text-muted-foreground">{description}</p>
          </div>
        </div>
        <Link
          href={href}
          className="inline-flex items-center gap-1 text-xs font-medium text-(--hms-accent)"
        >
          Open workspace
          <ArrowUpRight className="h-3.5 w-3.5" aria-hidden="true" />
        </Link>
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
              <div className="grid w-full gap-3 sm:grid-cols-2 xl:grid-cols-4">
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
                className="mb-6 rounded-xl bg-muted p-5 text-sm text-muted-foreground"
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
    <div className="min-w-0 rounded-xl border bg-muted/40 p-4">
      <p className="text-xs font-medium text-muted-foreground">{label}</p>
      <p className="mt-2 text-3xl font-semibold tracking-tight tabular-nums">
        {value.toLocaleString(undefined, {
          minimumFractionDigits: fractionDigits,
          maximumFractionDigits: fractionDigits,
        })}
      </p>
    </div>
  );
}
