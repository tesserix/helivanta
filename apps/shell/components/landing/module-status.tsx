import { useId } from "react";
import { moduleStatus, type ModuleName, type ModuleStatus } from "@/app/platform-catalog";
import { StatusGuide } from "./term";

const tone: Record<ModuleStatus, string> = {
  Available: "border-(--hms-accent) bg-(--hms-accent)/15 text-(--hms-accent)",
  "Foundation available": "border-(--hms-accent)/60 text-(--hms-accent)",
  // Planned and Future differ only by their border, so it must meet 3:1 (WCAG 1.4.11).
  Planned: "border-muted-foreground text-muted-foreground",
  Future: "border-dashed border-muted-foreground text-muted-foreground",
};

export function ModuleStatusLabel({ status }: { status: ModuleStatus }) {
  return (
    <span
      className={`inline-flex shrink-0 items-center rounded-full border px-2.5 py-0.5 text-[13px] font-medium ${tone[status]}`}
    >
      {status}
    </span>
  );
}

type AvailabilityProps = { modules: readonly ModuleName[]; note: string };

export function Availability({ modules, note }: AvailabilityProps) {
  const headingId = useId();
  return (
    <div>
      <div className="flex items-center gap-1">
        <p id={headingId} className="text-base font-semibold text-foreground">
          Availability
        </p>
        <StatusGuide />
      </div>
      <ul
        aria-labelledby={headingId}
        className="mt-3 divide-y divide-border border-y border-border"
      >
        {modules.map((name) => (
          <li key={name} className="flex items-center justify-between gap-4 py-2.5 text-base">
            <span className="text-foreground">{name}</span>
            <ModuleStatusLabel status={moduleStatus(name)} />
          </li>
        ))}
      </ul>
      <p className="mt-3 text-base leading-relaxed text-muted-foreground">{note}</p>
    </div>
  );
}
