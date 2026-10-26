import { useId } from "react";
import { moduleStatus, type ModuleName, type ModuleStatus } from "@/app/platform-catalog";

const tone: Record<ModuleStatus, string> = {
  Available: "border-(--night-care) bg-(--night-care)/15 text-(--night-care)",
  "Foundation available": "border-(--night-care)/60 text-(--night-care)",
  Planned: "border-(--night-line) text-(--night-muted)",
  Future: "border-dashed border-(--night-line) text-(--night-muted)",
};

export function ModuleStatusLabel({ status }: { status: ModuleStatus }) {
  return (
    <span
      className={`inline-flex shrink-0 items-center rounded-full border px-2.5 py-0.5 text-xs font-medium ${tone[status]}`}
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
      <p id={headingId} className="text-sm font-semibold text-(--night-ink)">
        Availability
      </p>
      <ul
        aria-labelledby={headingId}
        className="mt-3 divide-y divide-(--night-line) border-y border-(--night-line)"
      >
        {modules.map((name) => (
          <li key={name} className="flex items-center justify-between gap-4 py-2.5 text-sm">
            <span className="text-(--night-ink)">{name}</span>
            <ModuleStatusLabel status={moduleStatus(name)} />
          </li>
        ))}
      </ul>
      <p className="mt-3 text-sm leading-relaxed text-(--night-muted)">{note}</p>
    </div>
  );
}
