"use client";

import { useEffect, useId, useRef, useState } from "react";
import type { Insights } from "@helivanta/api";

export type ChartCategory = { key: string; label: string; color: string };

export function TrendChart({
  title,
  data,
  categories,
  mode,
}: {
  title: string;
  data: Insights;
  categories: ChartCategory[];
  mode: "line" | "bar";
}) {
  const id = useId();
  const container = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(760);
  useEffect(() => {
    if (!container.current || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(([entry]) => {
      if (entry) setWidth(Math.max(240, Math.floor(entry.contentRect.width)));
    });
    observer.observe(container.current);
    return () => observer.disconnect();
  }, []);
  const height = 240,
    left = 48,
    right = 16,
    top = 20,
    bottom = 40;
  const plotWidth = width - left - right,
    plotHeight = height - top - bottom;
  const max = Math.max(
    1,
    ...data.series.flatMap((point) =>
      categories.map((category) => point.counts[category.key] ?? 0),
    ),
  );
  const ceiling = Math.ceil(max / 4) * 4;
  const x = (index: number) => left + ((index + 0.5) * plotWidth) / Math.max(1, data.series.length);
  const y = (count: number) => top + plotHeight - (count / ceiling) * plotHeight;
  const labelEvery = Math.max(
    1,
    Math.ceil(data.series.length / Math.max(2, Math.floor(plotWidth / 110))),
  );
  const format = (date: string) =>
    new Intl.DateTimeFormat(
      "en",
      data.interval === "year"
        ? { year: "numeric", timeZone: "UTC" }
        : data.interval === "month"
          ? { month: "short", year: "2-digit", timeZone: "UTC" }
          : { month: "short", day: "numeric", timeZone: "UTC" },
    ).format(new Date(`${date}T00:00:00Z`));
  return (
    <div ref={container}>
      <div className="mb-3 flex flex-wrap gap-x-5 gap-y-2 text-xs text-muted-foreground">
        {categories.map((category) => (
          <span key={category.key} className="inline-flex items-center gap-2">
            <span className="h-2.5 w-2.5 rounded-full" style={{ background: category.color }} />
            {category.label}
          </span>
        ))}
      </div>
      <svg
        viewBox={`0 0 ${width} ${height}`}
        role="img"
        aria-labelledby={id}
        className="h-60 w-full overflow-visible"
      >
        <title id={id}>{title} trend</title>
        <desc>
          Counts by {data.interval} in {data.timezone}. Exact values are in the data table below.
        </desc>
        {[0, 1, 2, 3, 4].map((step) => (
          <g key={step}>
            <line
              x1={left}
              x2={width - right}
              y1={y((ceiling * step) / 4)}
              y2={y((ceiling * step) / 4)}
              stroke="var(--border)"
              strokeDasharray="3 5"
            />
            <text
              x={left - 10}
              y={y((ceiling * step) / 4) + 4}
              textAnchor="end"
              fontSize="11"
              fill="var(--muted-foreground)"
            >
              {new Intl.NumberFormat("en", { notation: "compact" }).format((ceiling * step) / 4)}
            </text>
          </g>
        ))}
        {categories.map((category, categoryIndex) => (
          <g key={category.key}>
            {mode === "line" && (
              <polyline
                points={data.series
                  .map((point, index) => `${x(index)},${y(point.counts[category.key] ?? 0)}`)
                  .join(" ")}
                fill="none"
                stroke={category.color}
                strokeWidth="2.5"
                strokeLinejoin="round"
              />
            )}
            {data.series.map((point, index) => {
              const count = point.counts[category.key] ?? 0;
              const barWidth = Math.min(
                20,
                plotWidth / Math.max(1, data.series.length) / (categories.length + 1),
              );
              return mode === "bar" ? (
                <rect
                  key={point.date}
                  x={x(index) + (categoryIndex - categories.length / 2) * barWidth}
                  y={y(count)}
                  width={barWidth * 0.8}
                  height={y(0) - y(count)}
                  rx="2"
                  fill={category.color}
                >
                  <title>
                    {point.date}: {category.label} {count}
                  </title>
                </rect>
              ) : (
                <circle
                  key={point.date}
                  cx={x(index)}
                  cy={y(count)}
                  r={data.series.length > 100 ? 1.5 : 3}
                  fill={category.color}
                >
                  <title>
                    {point.date}: {category.label} {count}
                  </title>
                </circle>
              );
            })}
          </g>
        ))}
        {data.series.map(
          (point, index) =>
            index % labelEvery === 0 && (
              <text
                key={point.date}
                x={x(index)}
                y={height - 12}
                textAnchor="middle"
                fontSize="11"
                fill="var(--muted-foreground)"
              >
                {format(point.date)}
              </text>
            ),
        )}
      </svg>
      <details className="mt-3 border-t pt-3 text-sm">
        <summary className="cursor-pointer text-muted-foreground focus-visible:outline-2 focus-visible:outline-(--hms-accent)">
          View data table
        </summary>
        <div className="mt-3 max-h-72 overflow-auto">
          <table className="w-full text-left text-xs" aria-label={`${title} data`}>
            <thead>
              <tr>
                <th scope="col" className="p-2">
                  Bucket starting
                </th>
                {categories.map((c) => (
                  <th scope="col" key={c.key} className="p-2 text-right">
                    {c.label}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {data.series.map((point) => (
                <tr key={point.date} className="border-t">
                  <th scope="row" className="p-2 font-normal">
                    {point.date}
                  </th>
                  {categories.map((c) => (
                    <td key={c.key} className="p-2 text-right tabular-nums">
                      {(point.counts[c.key] ?? 0).toLocaleString()}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </details>
    </div>
  );
}
