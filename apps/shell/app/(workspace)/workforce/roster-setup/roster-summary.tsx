import { WEEKDAYS, type RosterConfig } from "@helivanta/api";
export const effectiveDate = (value: string) =>
  new Date(`${value}T00:00:00Z`).toLocaleDateString(undefined, {
    timeZone: "UTC",
    year: "numeric",
    month: "short",
    day: "numeric",
  });
export function ConfigurationSummary({ record }: { record: RosterConfig }) {
  if (record.kind !== "weekly_template")
    return (
      <ul className="flex flex-wrap gap-2">
        {record.shifts.map((s) => (
          <li key={s.revision_id} className="rounded-lg border bg-muted/30 px-3 py-2 text-xs">
            <span className="font-medium">
              {s.name} · {s.code}
            </span>
            <span className="ml-2 text-muted-foreground">
              {s.start_time}–{s.end_time}
              {s.end_time < s.start_time ? " · next day" : ""}
            </span>
          </li>
        ))}
      </ul>
    );
  return (
    <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
      {record.days.map((d) => (
        <div key={d.weekday} className="rounded-xl border bg-muted/20 p-3">
          <p className="text-xs font-semibold">{WEEKDAYS[d.weekday]}</p>
          {d.weekly_off ? (
            <p className="mt-2 text-xs text-muted-foreground">Weekly off</p>
          ) : (
            d.allocations.map((a, i) => (
              <div key={a.shift_revision_id} className="mt-2 text-xs">
                <p className="font-medium">
                  {i + 1}. {a.shift.name} · {a.shift.code}
                </p>
                <p className="mt-1 text-muted-foreground">
                  {a.area_name} · {a.shift.start_time}–{a.shift.end_time}
                  {a.shift.end_time < a.shift.start_time ? " · next day" : ""}
                </p>
              </div>
            ))
          )}
        </div>
      ))}
    </div>
  );
}
