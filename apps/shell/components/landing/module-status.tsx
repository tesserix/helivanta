import { useId } from "react";
import { moduleStatus, type ModuleName, type ModuleStatus } from "@/app/platform-catalog";
import { StatusGuide } from "./term";

const tone: Record<ModuleStatus, string> = {
  Available: "border-(--home-care) bg-(--home-care)/15 text-(--home-care)",
  "Foundation available": "border-(--home-care)/60 text-(--home-care)",
  // Planned and Future differ only by their border, so it must meet 3:1 (WCAG 1.4.11).
  Planned: "border-(--home-muted) text-(--home-muted)",
  Future: "border-dashed border-(--home-muted) text-(--home-muted)",
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
        <p id={headingId} className="text-base font-semibold text-(--home-ink)">
          Availability
        </p>
        <StatusGuide />
      </div>
      <ul
        aria-labelledby={headingId}
        className="mt-3 divide-y divide-(--home-line) border-y border-(--home-line)"
      >
        {modules.map((name) => (
          <li key={name} className="flex items-center justify-between gap-4 py-2.5 text-base">
            <span className="text-(--home-ink)">{name}</span>
            <ModuleStatusLabel status={moduleStatus(name)} />
          </li>
        ))}
      </ul>
      <p className="mt-3 text-base leading-relaxed text-(--home-muted)">{note}</p>
    </div>
  );
}
