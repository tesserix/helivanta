"use client";

import { useRef } from "react";
import { motion } from "framer-motion";
import { Check, Clock, FileClock, ShieldX, TriangleAlert, Wrench } from "lucide-react";
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
  CERTIFICATE_WINDOW_DAYS,
  findingLabel,
  findingOf,
  maintenanceModules,
  maintenanceNote,
  register,
  routeItems,
  routes,
  site,
  stepTitle,
  steps,
  tally,
  type Finding,
  type RegisterItem,
  type RouteId,
} from "./maintenance-watch-data";
import { useStoryPlayer } from "./story-player";

const SCAN_S = 2.4;
const REGISTER =
  "grid grid-cols-[minmax(0,1.6fr)_minmax(6rem,1fr)_minmax(7.5rem,1fr)_minmax(10.5rem,1fr)] items-center gap-x-3";

const findingStyle: Record<Finding, ChipStyle> = {
  current: { colour: CARE, icon: Check, className: "border-(--chip) text-(--chip)" },
  "due-soon": { colour: FOUND, icon: Clock, className: "border-(--chip) text-(--chip)" },
  overdue: {
    colour: FOUND,
    icon: TriangleAlert,
    className: "border-(--chip) bg-(--chip) text-(--home-canvas)",
  },
  expiring: {
    colour: FOUND,
    icon: FileClock,
    className: "border-dashed border-(--chip) text-(--chip)",
  },
  failed: {
    colour: AI,
    icon: ShieldX,
    className: "border-(--chip) bg-(--chip) text-(--home-canvas)",
  },
};

function dueText(item: RegisterItem): string {
  if (item.dueInDays < 0) return `${-item.dueInDays} days overdue`;
  if (item.evidence === "task") return `In ${item.dueInDays} days`;
  return item.dueInDays <= CERTIFICATE_WINDOW_DAYS
    ? `Expires in ${item.dueInDays} days`
    : `Valid ${Math.round(item.dueInDays / 30)} months`;
}

function summary(): string {
  const counts = tally();
  return `${register.length} items checked: ${(Object.keys(counts) as Finding[])
    .map((finding) => `${counts[finding]} ${findingLabel[finding].toLowerCase()}`)
    .join(", ")}.`;
}

export function MaintenanceWatch() {
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
        <figure aria-label="A maintenance register of eight electrical, structural, equipment, fire safety, lift and medical gas items is checked automatically, and each item is marked current, due soon, overdue, certificate expiring or failed check. Overdue and due work goes to a maintenance planning agent and an expiring certificate to a compliance agent, whose drafts a named person approves. A failed safety check goes straight to a biomedical engineer, and the equipment stays out of service until it passes a retest.">
          <div ref={boardRef}>
            <div
              aria-hidden="true"
              className="rounded-2xl border border-(--home-line) bg-(--home-board) p-5 lg:p-6"
            >
              <Register
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
                cadence="Nightly and after each inspection"
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
                    items: items.map((item) => item.asset),
                  };
                }}
              />
            </div>
          </div>
        </figure>
        <Legend animated={player.animated} paused={player.paused} onToggle={player.togglePaused} />
      </div>
      <Narration
        label="Steps in the maintenance check"
        steps={steps}
        current={current}
        titleOf={stepTitle}
        summary={summary()}
        modules={maintenanceModules}
        note={maintenanceNote}
      />
    </div>
  );
}

type RegisterProps = { checked: boolean; scanning: boolean; focus?: RouteId };

function Register({ checked, scanning, focus }: RegisterProps) {
  const counts = tally();
  const focused = routes.find((route) => route.id === focus);

  return (
    <div>
      <p className="flex items-center gap-2 text-sm font-semibold text-(--home-ink)">
        <Wrench className="size-4 text-(--home-stock)" />
        {site}
        <span className="font-normal text-(--home-muted)">maintenance register</span>
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
          <p className="text-xs text-(--home-muted)">Not checked yet</p>
        )}
      </div>
      <div
        className={cn(
          REGISTER,
          "mt-4 border-b border-(--home-line) pb-2 text-xs text-(--home-muted)",
        )}
      >
        <p>Item and check</p>
        <p>Area</p>
        <p>Due</p>
        <p>Finding</p>
      </div>
      <div className="relative overflow-hidden">
        <ol aria-label="Register" className="divide-y divide-(--home-line)/60">
          {register.map((item, index) => {
            const finding = findingOf(item);
            return (
              <li
                key={item.id}
                className={cn(
                  REGISTER,
                  "py-2 transition-opacity duration-500",
                  focused && !focused.findings.includes(finding) && "opacity-35",
                )}
              >
                <p className="min-w-0 text-[13px] leading-tight font-semibold text-(--home-ink)">
                  {item.asset}
                  <span className="block text-xs font-normal text-(--home-muted)">
                    {item.check}
                  </span>
                </p>
                <p className="text-[13px] leading-tight text-(--home-muted)">{item.area}</p>
                <p className="text-[13px] leading-tight text-(--home-ink) tabular-nums">
                  {dueText(item)}
                  {item.lastResult === "fail" && (
                    <span className="block text-xs text-(--home-oversight)">
                      Failed last test
                    </span>
                  )}
                </p>
                <div>
                  {checked ? (
                    <motion.span
                      className="inline-block"
                      initial={scanning ? { opacity: 0, scale: 0.85 } : false}
                      animate={{ opacity: 1, scale: 1 }}
                      transition={{
                        delay: scanning ? ((index + 0.5) / register.length) * SCAN_S : 0,
                        duration: 0.25,
                      }}
                    >
                      <FindingChip style={findingStyle[finding]} label={findingLabel[finding]} />
                    </motion.span>
                  ) : (
                    <span className="inline-block h-5 w-24 rounded-full border border-dashed border-(--home-line)" />
                  )}
                </div>
              </li>
            );
          })}
        </ol>
        {scanning && (
          <motion.div
            key="register-scan"
            className="pointer-events-none absolute inset-x-0 h-12 border-b border-(--home-stock) bg-linear-to-b from-transparent to-(--home-stock)/15"
            initial={{ top: "-18%" }}
            animate={{ top: "82%" }}
            transition={{ duration: SCAN_S, ease: "linear" }}
          />
        )}
      </div>
    </div>
  );
}
