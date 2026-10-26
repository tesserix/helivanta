"use client";

import { useRef } from "react";
import { motion } from "framer-motion";
import { CalendarClock, Check, Lock, Package, PackageX, TrendingDown } from "lucide-react";
import { cn } from "@tesserix/web";
import {
  AI,
  CARE,
  CheckFlow,
  FOUND,
  FindingChip,
  Legend,
  Narration,
  type ChipStyle,
} from "./check-flow";
import {
  findingLabel,
  findingOf,
  outlet,
  routeItems,
  routes,
  shelf,
  stepTitle,
  steps,
  stockModules,
  stockNote,
  tally,
  type Finding,
  type RouteId,
} from "./stock-watch-data";
import { useStoryPlayer } from "./story-player";

const SCAN_S = 2.4;
const SHELF = "grid grid-cols-[minmax(0,1fr)_6rem_5rem_6rem_9.75rem] items-center gap-x-3";

const units = new Intl.NumberFormat("en");

const findingStyle: Record<Finding, ChipStyle> = {
  "in-stock": { colour: CARE, icon: Check, className: "border-(--chip) text-(--chip)" },
  low: { colour: FOUND, icon: TrendingDown, className: "border-(--chip) text-(--chip)" },
  out: {
    colour: FOUND,
    icon: PackageX,
    className: "border-(--chip) bg-(--chip) text-(--night-canvas)",
  },
  expiring: {
    colour: FOUND,
    icon: CalendarClock,
    className: "border-dashed border-(--chip) text-(--chip)",
  },
  quarantine: { colour: AI, icon: Lock, className: "border-(--chip) text-(--chip)" },
};

function expiryText(days?: number): string {
  if (days === undefined) return "None on hand";
  return days < 60 ? `${days} days` : `${Math.round(days / 30)} months`;
}

function summary(): string {
  const counts = tally();
  return `${shelf.length} medicines checked: ${(Object.keys(counts) as Finding[])
    .map((finding) => `${counts[finding]} ${findingLabel[finding].toLowerCase()}`)
    .join(", ")}.`;
}

export function StockWatch() {
  const boardRef = useRef<HTMLDivElement>(null);
  const player = useStoryPlayer(boardRef, steps.length);
  const current = player.animated ? player.step : undefined;
  const reached = current ?? steps.length - 1;
  const animated = current !== undefined;
  const step = steps[reached];
  const focus = animated ? step.route : undefined;

  return (
    <div className="grid gap-x-12 gap-y-10 xl:grid-cols-[minmax(0,1fr)_21rem]">
      <div className="hidden min-w-0 md:block">
        <figure aria-label="A pharmacy shelf of six medicines is checked automatically, and each medicine is marked in stock, running low, out of stock, expiring within 30 days or in quarantine. Medicines to order and a short-dated batch go to a stock forecasting agent, whose draft a named person approves. A batch in quarantine goes straight to a pharmacist.">
          <div ref={boardRef}>
            <div
              aria-hidden="true"
              className="rounded-2xl border border-(--night-line) bg-(--night-board) p-5 lg:p-6"
            >
              <Shelf
                checked={reached >= 1}
                scanning={animated && step.phase === "check"}
                focus={focus}
              />
              <CheckFlow
                steps={steps}
                routes={routes}
                reached={reached}
                animated={animated}
                playing={player.playing}
                focus={focus}
                cadence="Nightly and after each dispense"
                findingOf={(route) => {
                  const items = routeItems(route);
                  const finding = findingOf(items[0]);
                  const Icon = findingStyle[finding].icon;
                  return {
                    icon: (
                      <Icon
                        className="size-3.5 shrink-0"
                        style={{ color: findingStyle[finding].colour }}
                      />
                    ),
                    items: items.map((item) => item.medicine),
                  };
                }}
              />
            </div>
          </div>
        </figure>
        <Legend animated={player.animated} paused={player.paused} onToggle={player.togglePaused} />
      </div>
      <Narration
        label="Steps in the stock check"
        steps={steps}
        current={current}
        titleOf={stepTitle}
        summary={summary()}
        modules={stockModules}
        note={stockNote}
      />
    </div>
  );
}

type ShelfProps = { checked: boolean; scanning: boolean; focus?: RouteId };

function Shelf({ checked, scanning, focus }: ShelfProps) {
  const counts = tally();
  const focused = routes.find((route) => route.id === focus);

  return (
    <div>
      <p className="flex items-center gap-2 text-sm font-semibold text-(--night-ink)">
        <Package className="size-4 text-(--night-stock)" />
        {outlet}
        <span className="font-normal text-(--night-muted)">stock register</span>
      </p>
      <div className="mt-3 flex h-6 items-center">
        {checked ? (
          <ul className="flex gap-1.5">
            {(Object.keys(counts) as Finding[]).map((finding, index) => (
              <motion.li
                key={finding}
                initial={scanning ? { opacity: 0, y: 4 } : false}
                animate={{ opacity: 1, y: 0 }}
                transition={{ delay: scanning ? SCAN_S + index * 0.08 : 0, duration: 0.3 }}
              >
                <FindingChip
                  style={findingStyle[finding]}
                  label={findingLabel[finding]}
                  count={counts[finding]}
                />
              </motion.li>
            ))}
          </ul>
        ) : (
          <p className="text-xs text-(--night-muted)">Not checked yet</p>
        )}
      </div>
      <div
        className={cn(
          SHELF,
          "mt-4 border-b border-(--night-line) pb-2 text-[11px] text-(--night-muted)",
        )}
      >
        <p>Medicine</p>
        <p className="text-right">Can dispense</p>
        <p className="text-right">Reorder at</p>
        <p className="text-right">Earliest expiry</p>
        <p>Check</p>
      </div>
      <div className="relative overflow-hidden">
        <ol aria-label="Shelf" className="divide-y divide-(--night-line)/60">
          {shelf.map((item, index) => {
            const finding = findingOf(item);
            return (
              <li
                key={item.id}
                className={cn(
                  SHELF,
                  "py-2 transition-opacity duration-500",
                  focused && !focused.findings.includes(finding) && "opacity-35",
                )}
              >
                <p className="min-w-0 text-[13px] leading-tight font-semibold text-(--night-ink)">
                  {item.medicine}
                  <span className="block text-[11px] font-normal text-(--night-muted)">
                    {item.unit}
                  </span>
                </p>
                <p className="text-right text-[13px] leading-tight text-(--night-ink) tabular-nums">
                  {units.format(item.dispensable)}
                  {item.quarantined > 0 && (
                    <span className="block text-[11px] text-(--night-oversight)">
                      +{units.format(item.quarantined)} held
                    </span>
                  )}
                </p>
                <p className="text-right text-[13px] text-(--night-muted) tabular-nums">
                  {units.format(item.reorderAt)}
                </p>
                <p className="text-right text-[13px] text-(--night-muted) tabular-nums">
                  {expiryText(item.expiresInDays)}
                </p>
                <div>
                  {checked ? (
                    <motion.span
                      className="inline-block"
                      initial={scanning ? { opacity: 0, scale: 0.85 } : false}
                      animate={{ opacity: 1, scale: 1 }}
                      transition={{
                        delay: scanning ? ((index + 0.5) / shelf.length) * SCAN_S : 0,
                        duration: 0.25,
                      }}
                    >
                      <FindingChip style={findingStyle[finding]} label={findingLabel[finding]} />
                    </motion.span>
                  ) : (
                    <span className="inline-block h-5 w-24 rounded-full border border-dashed border-(--night-line)" />
                  )}
                </div>
              </li>
            );
          })}
        </ol>
        {scanning && (
          <motion.div
            key="shelf-scan"
            className="pointer-events-none absolute inset-x-0 h-12 border-b border-(--night-stock) bg-linear-to-b from-transparent to-(--night-stock)/15"
            initial={{ top: "-18%" }}
            animate={{ top: "82%" }}
            transition={{ duration: SCAN_S, ease: "linear" }}
          />
        )}
      </div>
    </div>
  );
}
