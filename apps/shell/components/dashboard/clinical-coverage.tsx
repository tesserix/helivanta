import { Panel } from "@helivanta/ui";
import { Activity, Baby, BedDouble, DoorOpen, HeartPulse, Scissors } from "lucide-react";

const reports = [
  {
    title: "Emergency department",
    icon: Activity,
    measures: "Attendances · triage · waiting · dispositions",
    detail: "Emergency department attendance requires its own clinical encounter records.",
  },
  {
    title: "Maternity & births",
    icon: Baby,
    measures: "Delivery episodes · babies born",
    detail:
      "Live births and stillbirths require confirmed birth records, including multiple births.",
  },
  {
    title: "Admissions & discharges",
    icon: DoorOpen,
    measures: "Admissions · discharges · current inpatients",
    detail: "Admission and discharge events are needed alongside visit registrations.",
  },
  {
    title: "Beds & occupancy",
    icon: BedDouble,
    measures: "Occupied beds · staffed beds · occupancy",
    detail: "Occupancy requires a current bed census and the available staffed capacity.",
  },
  {
    title: "Procedures & theatre",
    icon: Scissors,
    measures: "Completed procedures · theatre activity · cancellations",
    detail: "Procedure reporting requires confirmed events and their completion times.",
  },
  {
    title: "Deaths",
    icon: HeartPulse,
    measures: "Confirmed deaths · event-date reporting",
    detail: "Death statistics require verified records for the relevant hospital and period.",
  },
];

export function ClinicalCoverage() {
  return (
    <section aria-label="Clinical reporting coverage" className="space-y-4">
      <div>
        <h3 className="text-lg font-semibold tracking-tight">Clinical reporting coverage</h3>
        <p className="mt-1 max-w-3xl text-sm leading-6 text-muted-foreground">
          These clinical measures are awaiting verified reporting. Available patient, laboratory and
          pharmacy activity appears in the panels below, according to your access.
        </p>
      </div>
      <div className="grid min-w-0 gap-3 sm:grid-cols-2 xl:grid-cols-3">
        {reports.map(({ title, icon: Icon, measures, detail }) => (
          <Panel as="article" key={title} aria-label={title} className="min-w-0">
            <div className="mb-3 flex flex-wrap items-start justify-between gap-2">
              <Icon className="size-5 text-(--hms-accent)" aria-hidden="true" />
              <span className="rounded-full bg-muted px-2.5 py-1 text-xs font-medium text-muted-foreground">
                Not yet reported
              </span>
            </div>
            <h4 className="font-semibold">{title}</h4>
            <p className="mt-2 text-xs font-medium leading-5">{measures}</p>
            <p className="mt-2 text-xs leading-5 text-muted-foreground">{detail}</p>
          </Panel>
        ))}
      </div>
    </section>
  );
}
