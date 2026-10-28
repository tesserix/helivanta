"use client";

import type { CSSProperties, ReactNode } from "react";
import { motion } from "framer-motion";
import { ScanSearch, UserCheck, type LucideIcon } from "lucide-react";
import { cn } from "@tesserix/web";
import type { ModuleName } from "@/app/platform-catalog";
import type { FlowRoute, FlowStep } from "./check-flow-model";
import {
  AiAssistedMark,
  Arrowhead,
  BoardCard,
  CardBody,
  CardHeader,
  StatusPill,
} from "./board-card";
import { Availability } from "./module-status";
import { PlayToggle } from "./story-player";

export const DRAW_S = 0.7;
export const AI = "var(--home-oversight)";
export const CARE = "var(--home-care)";
export const FOUND = "var(--home-stock)";
const WAITING = "Waiting";
const REVIEWING = "Reviewing the draft";
const FLOW =
  "grid grid-cols-[7.5rem_1.5rem_minmax(0,0.9fr)_1.5rem_minmax(0,1.1fr)_1.5rem_minmax(0,1fr)]";

export type ChipStyle = { colour: string; icon: LucideIcon; className: string };

export function FindingChip({
  style,
  label,
  count,
}: {
  style: ChipStyle;
  label: string;
  count?: number;
}) {
  const Icon = style.icon;
  return (
    <span
      style={{ "--chip": style.colour } as CSSProperties}
      className={cn(
        "inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-[11px] font-medium whitespace-nowrap",
        style.className,
      )}
    >
      <Icon className="size-3 shrink-0" />
      {count !== undefined && <span className="tabular-nums">{count}</span>}
      {label}
    </span>
  );
}

function stepColour(step: FlowStep): string {
  if (step.phase === "draft") return AI;
  if (step.phase === "approve") return CARE;
  return FOUND;
}

type NarrationProps<S extends FlowStep> = {
  label: string;
  steps: readonly S[];
  current?: number;
  titleOf: (step: S) => string;
  summary: string;
  modules: readonly ModuleName[];
  note: string;
};

export function Narration<S extends FlowStep>({
  label,
  steps,
  current,
  titleOf,
  summary,
  modules,
  note,
}: NarrationProps<S>) {
  return (
    <div>
      <ol aria-label={label} className="space-y-1">
        {steps.map((step, index) => (
          <li
            key={step.id}
            aria-current={index === current ? "step" : undefined}
            style={{ "--lane": stepColour(step) } as CSSProperties}
            className={cn(
              "grid grid-cols-[1.5rem_1fr] gap-x-2 rounded-r-lg border-l-2 py-2.5 pr-3 pl-3 transition-colors duration-300",
              index === current ? "border-(--lane) bg-(--home-surface)" : "border-transparent",
            )}
          >
            <span aria-hidden="true" className="text-sm text-(--home-muted) tabular-nums">
              {index + 1}
            </span>
            <div>
              <p className="text-sm font-semibold text-(--home-ink)">{titleOf(step)}</p>
              <p className="mt-1 text-sm leading-relaxed text-(--home-muted)">{step.text}</p>
              {step.phase === "check" && (
                <p className="mt-1 text-sm leading-relaxed text-(--home-ink)">{summary}</p>
              )}
            </div>
          </li>
        ))}
      </ol>
      <div className="mt-8">
        <Availability modules={modules} note={note} />
      </div>
    </div>
  );
}

type LegendProps = { animated: boolean; paused: boolean; onToggle: () => void };

export function Legend({ animated, paused, onToggle }: LegendProps) {
  return (
    <div className="mt-4 flex items-center justify-between gap-6">
      <ul aria-hidden="true" className="flex flex-wrap gap-x-6 gap-y-2 text-xs">
        <li className="flex items-center gap-2 text-(--home-muted)">
          <ScanSearch className="size-3.5 text-(--home-stock)" />
          Checks run by themselves
        </li>
        <li className="flex items-center gap-2 text-(--home-muted)">
          <span className="h-3 w-5 border border-dashed border-(--home-oversight)" />
          Agents draft
        </li>
        <li className="flex items-center gap-2 text-(--home-muted)">
          <span className="h-3 w-5 rounded border border-(--home-care)" />
          People decide
        </li>
      </ul>
      {animated && <PlayToggle paused={paused} onToggle={onToggle} />}
    </div>
  );
}

export type LaneFinding = { icon: ReactNode; items: readonly string[] };

type FlowProps<R extends FlowRoute> = {
  steps: readonly FlowStep[];
  routes: readonly R[];
  reached: number;
  animated: boolean;
  playing: boolean;
  focus?: string;
  cadence: string;
  findingOf: (route: R) => LaneFinding;
};

export function CheckFlow<R extends FlowRoute>({
  steps,
  routes,
  reached,
  animated,
  playing,
  focus,
  cadence,
  findingOf,
}: FlowProps<R>) {
  const checking = animated && steps[reached].phase === "check";
  return (
    <div className="mt-6 border-t border-(--home-line) pt-5">
      <div className={cn(FLOW, "pb-3 text-xs text-(--home-muted)")}>
        <p className="col-start-3">Finding</p>
        <p className="col-start-5">Agent drafts</p>
        <p className="col-start-7">Named person decides</p>
      </div>
      <div className={cn(FLOW, "auto-rows-fr gap-y-3")}>
        <BoardCard
          style={{ gridRow: `1 / span ${routes.length}`, gridColumn: 1 }}
          ring={checking ? FOUND : undefined}
          surface="bg-(--home-hub)"
        >
          <div className="flex flex-1 flex-col items-center justify-center px-3 py-4 text-center">
            <span className="relative flex size-12 items-center justify-center">
              <motion.span
                className="absolute inset-0 rounded-full border border-dashed border-(--home-stock)/80"
                animate={playing && reached >= 1 ? { rotate: 360 } : { rotate: 0 }}
                transition={
                  playing && reached >= 1
                    ? { duration: 24, ease: "linear", repeat: Infinity }
                    : { duration: 0 }
                }
              />
              <ScanSearch className="size-5 text-(--home-stock)" />
            </span>
            <p className="mt-2.5 text-[13px] leading-tight font-semibold text-(--home-ink)">
              Automatic check
            </p>
            <p className="mt-1 text-[11px] leading-snug text-(--home-muted)">{cadence}</p>
          </div>
        </BoardCard>
        {routes.map((route, lane) => (
          <Lane
            key={route.id}
            steps={steps}
            route={route}
            finding={findingOf(route)}
            row={lane + 1}
            reached={reached}
            animated={animated}
            dimmed={focus !== undefined && focus !== route.id}
          />
        ))}
      </div>
    </div>
  );
}

type WireState = "idle" | "drawing" | "done";

type LaneProps = {
  steps: readonly FlowStep[];
  route: FlowRoute;
  finding: LaneFinding;
  row: number;
  reached: number;
  animated: boolean;
  dimmed: boolean;
};

function Lane({ steps, route, finding, row, reached, animated, dimmed }: LaneProps) {
  const draftAt = steps.findIndex((step) => step.route === route.id);
  const approveAt = steps.findLastIndex((step) => step.route === route.id);
  const found = reached >= 1;
  const drafted = route.agent !== undefined && reached >= draftAt;
  const approved = reached >= approveAt;
  const reviewing = drafted && !approved;
  const motionKey = `${route.id}-${reached}`;
  const wire = (at: number): WireState =>
    reached < at ? "idle" : animated && reached === at ? "drawing" : "done";
  const handOff = route.agent ? AI : CARE;
  const opacity = dimmed ? "opacity-45" : "opacity-100";
  const place = (column: number): CSSProperties => ({ gridRow: row, gridColumn: column });

  return (
    <div role="group" aria-label={route.title} className="contents">
      <Wire row={row} column={2} state={wire(1)} colour={FOUND} motionKey={motionKey} />
      <BoardCard
        style={place(3)}
        className={opacity}
        ring={found && animated && reached === 1 ? FOUND : undefined}
      >
        <CardHeader
          icon={finding.icon}
          title={route.title}
          count={found ? finding.items.length : undefined}
        />
        <CardBody>{found ? finding.items.join(", ") : "Nothing found yet"}</CardBody>
      </BoardCard>
      <Wire row={row} column={4} state={wire(draftAt)} colour={handOff} motionKey={motionKey} />
      {route.agent ? (
        <BoardCard
          style={place(5)}
          className={opacity}
          dashed
          ring={reviewing || (animated && reached === draftAt) ? AI : undefined}
        >
          <CardHeader title={route.agent.label} />
          <CardBody>{route.agent.detail}</CardBody>
        </BoardCard>
      ) : (
        <BoardCard style={place(5)} className={opacity} dashed surface="bg-transparent">
          <CardHeader title="No agent" />
          <CardBody>{route.noAgent}</CardBody>
        </BoardCard>
      )}
      <Wire
        row={row}
        column={6}
        state={wire(approveAt)}
        colour={handOff}
        delay={draftAt === approveAt ? DRAW_S : 0}
        motionKey={motionKey}
      />
      <BoardCard
        style={place(7)}
        className={opacity}
        ring={reviewing || approved ? CARE : undefined}
      >
        <CardHeader
          icon={<UserCheck className="size-3.5 shrink-0 text-(--home-care)" />}
          title={route.approver.name}
        />
        <CardBody>
          {route.approver.role}
          <motion.div
            key={motionKey}
            className="mt-2"
            initial={animated && reached === approveAt ? { opacity: 0 } : false}
            animate={{ opacity: 1 }}
            transition={{ delay: approved ? DRAW_S : 0, duration: 0.3 }}
          >
            <StatusPill
              tone={approved ? "done" : reviewing ? "reviewing" : "waiting"}
              label={approved ? route.outcome : reviewing ? REVIEWING : WAITING}
              reserve={[WAITING, REVIEWING, route.outcome]}
            />
            {route.agent && <AiAssistedMark shown={approved} />}
          </motion.div>
        </CardBody>
      </BoardCard>
    </div>
  );
}

type WireProps = {
  row: number;
  column: number;
  state: WireState;
  colour: string;
  delay?: number;
  motionKey: string;
};

function Wire({ row, column, state, colour, delay = 0, motionKey }: WireProps) {
  return (
    <div
      style={{ gridRow: row, gridColumn: column }}
      className="relative mr-1 h-px self-center bg-(--home-line)"
    >
      {state === "drawing" && (
        <motion.span
          key={motionKey}
          className="absolute inset-0 origin-left"
          style={{ backgroundColor: colour, boxShadow: `0 0 10px ${colour}` }}
          initial={{ scaleX: 0 }}
          animate={{ scaleX: 1 }}
          transition={{ delay, duration: DRAW_S, ease: "easeInOut" }}
        />
      )}
      {state === "done" && (
        <span className="absolute inset-0 opacity-70" style={{ backgroundColor: colour }} />
      )}
      <Arrowhead colour={state === "done" ? colour : "var(--home-line)"} />
    </div>
  );
}
