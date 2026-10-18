"use client";

import type { ChartCategory } from "./trend-chart";

export function CohortBreakdown({
  title,
  totals,
  categories,
  presentation,
}: {
  title: string;
  totals: Record<string, number>;
  categories: ChartCategory[];
  presentation: "ring" | "bar";
}) {
  const total = categories.reduce((sum, category) => sum + (totals[category.key] ?? 0), 0);
  if (total === 0)
    return (
      <section aria-label={title} className="rounded-xl border bg-muted/40 p-4">
        <h4 className="text-sm font-semibold">{title}</h4>
        <p className="mt-3 text-sm text-muted-foreground">
          No records to distribute in this period.
        </p>
      </section>
    );
  const slices = categories.map((category) => ({
    ...category,
    count: totals[category.key] ?? 0,
    share: ((totals[category.key] ?? 0) / total) * 100,
  }));
  return (
    <section aria-label={title} className="min-w-0 rounded-xl border bg-muted/40 p-4">
      <h4 className="text-sm font-semibold">{title}</h4>
      <p className="mt-1 text-xs leading-5 text-muted-foreground">
        Share of the displayed records.
      </p>
      {presentation === "ring" ? (
        <svg
          viewBox="0 0 120 120"
          role="img"
          aria-label={`${title} distribution`}
          className="mx-auto my-4 h-40 w-40"
        >
          <circle cx="60" cy="60" r="42" fill="none" stroke="var(--hms-track)" strokeWidth="12" />
          {slices.map((slice, index) => (
            <circle
              key={slice.key}
              cx="60"
              cy="60"
              r="42"
              fill="none"
              stroke={slice.color}
              strokeWidth="12"
              pathLength="100"
              strokeDasharray={`${slice.share} ${100 - slice.share}`}
              strokeDashoffset={-slices.slice(0, index).reduce((sum, item) => sum + item.share, 0)}
              transform="rotate(-90 60 60)"
            >
              <title>
                {slice.label}: {slice.share.toFixed(1)}%
              </title>
            </circle>
          ))}
          <text
            x="60"
            y="62"
            textAnchor="middle"
            fill="var(--foreground)"
            fontSize="12"
            fontWeight="600"
          >
            Mix
          </text>
        </svg>
      ) : (
        <div
          role="img"
          aria-label={`${title} distribution`}
          className="my-5 flex h-3 overflow-hidden rounded-full bg-(--hms-track)"
        >
          {slices.map((slice) => (
            <div key={slice.key} style={{ width: `${slice.share}%`, background: slice.color }} />
          ))}
        </div>
      )}
      <table aria-label={`${title} breakdown`} className="w-full text-left text-xs">
        <thead>
          <tr className="text-muted-foreground">
            <th scope="col" className="pb-2 font-medium">
              Type
            </th>
            <th scope="col" className="pb-2 text-right font-medium">
              Count
            </th>
            <th scope="col" className="pb-2 pl-3 text-right font-medium">
              Share
            </th>
          </tr>
        </thead>
        <tbody>
          {slices.map((slice) => (
            <tr key={slice.key} className="border-t">
              <th scope="row" className="py-3 pr-2 font-normal">
                <span
                  className="mr-2 inline-block h-2 w-2 rounded-full"
                  style={{ background: slice.color }}
                />
                {slice.label}
              </th>
              <td className="text-right tabular-nums">{slice.count.toLocaleString()}</td>
              <td className="pl-3 text-right tabular-nums">{slice.share.toFixed(1)}%</td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}
