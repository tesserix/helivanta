"use client";

import { useRef, useState } from "react";
import { motion } from "framer-motion";
import { ArrowRight, FileCheck2, UserCheck, Waypoints } from "lucide-react";
import { cn } from "@tesserix/web";
import {
  AiAssistedMark,
  Arrowhead,
  BoardCard,
  CardBody,
  CardHeader,
  StatusPill,
} from "./board-card";
import { Availability } from "./module-status";
import {
  agents,
  aiNote,
  scenarioSteps,
  scenarios,
  sources,
  type AgentId,
  type NodeId,
  type Scenario,
  type SourceId,
} from "./ai-board-data";
import { PlayToggle, StoryPicker, useStoryPlayer } from "./story-player";

const DRAW_S = 0.8;
const AI = "var(--home-oversight)";
const CARE = "var(--home-care)";
const COLUMNS =
  "grid grid-cols-[minmax(0,1fr)_1.5rem_6.5rem_1.5rem_minmax(0,1.15fr)_1.5rem_minmax(0,1fr)]";

function nodeLabel(node: NodeId, scenario: Scenario): string {
  if (node === "supervisor") return "Supervisor agent";
  if (node === "person") return scenario.approver.name;
  if (node === "record") return "Hospital record";
  return node in sources ? sources[node as SourceId].label : agents[node as AgentId].label;
}

export function AiBoard() {
  const [scenarioIndex, setScenarioIndex] = useState(0);
  const boardRef = useRef<HTMLDivElement>(null);
  const scenario = scenarios[scenarioIndex];
  const steps = scenarioSteps(scenario);
  const player = useStoryPlayer(boardRef, steps.length, () =>
    setScenarioIndex((scenarioIndex + 1) % scenarios.length),
  );
  const current = player.animated ? player.step : undefined;

  function choose(index: number) {
    setScenarioIndex(index);
    player.setStep(0);
  }

  return (
    <div>
      <StoryPicker
        label="AI scenarios"
        items={scenarios.map((item) => ({ ...item, colour: AI }))}
        selected={scenarioIndex}
        onSelect={choose}
        progress={
          player.animated
            ? { step: player.step, length: steps.length, playing: player.playing }
            : undefined
        }
      />
      <div className="mt-8 grid gap-x-12 gap-y-10 xl:grid-cols-[minmax(0,1fr)_21rem]">
        <div className="hidden min-w-0 md:block">
          <figure aria-label="Five hospital events on the left, each passing through the supervisor agent in the middle to one specialist agent. The agent drafts the work, and the named person on the right reviews and approves it before it enters the hospital record.">
            <div ref={boardRef}>
              <Board
                active={scenarioIndex}
                reached={current ?? steps.length - 1}
                animated={current !== undefined}
                playing={player.playing}
              />
            </div>
          </figure>
          <div className="mt-4 flex items-center justify-between gap-6">
            <ul aria-hidden="true" className="flex flex-wrap gap-x-6 gap-y-2 text-[13px]">
              <li className="flex items-center gap-2 text-(--home-muted)">
                <span className="h-3 w-5 rounded border border-dashed border-(--home-oversight)" />
                Agents draft
              </li>
              <li className="flex items-center gap-2 text-(--home-muted)">
                <span className="h-3 w-5 rounded border border-(--home-care)" />
                People decide
              </li>
              <li className="flex items-center gap-2 text-(--home-muted)">
                <FileCheck2 className="size-3.5 text-(--home-ink)" />
                Every action recorded
              </li>
            </ul>
            {player.animated && (
              <PlayToggle paused={player.paused} onToggle={player.togglePaused} />
            )}
          </div>
        </div>
        <Narration scenario={scenario} currentStep={current} />
      </div>
    </div>
  );
}

type BoardProps = { active: number; reached: number; animated: boolean; playing: boolean };

// One lane per scenario keeps every route a straight line that never crosses another lane.
function Board({ active, reached, animated, playing }: BoardProps) {
  const routing = reached <= 1;
  return (
    <div
      aria-hidden="true"
      className="rounded-2xl border border-(--home-line) bg-(--home-board) bg-[radial-gradient(var(--home-line)_1px,transparent_1px)] bg-size-[28px_28px] p-5 lg:p-6"
    >
      <div className={cn(COLUMNS, "pb-3 text-xs text-(--home-muted)")}>
        <p>Hospital event</p>
        <p className="col-start-5">Specialist agent drafts</p>
        <p className="col-start-7">Named person approves</p>
      </div>
      <div className={cn(COLUMNS, "auto-rows-fr gap-y-3")}>
        <BoardCard
          style={{ gridRow: "1 / span 5", gridColumn: 3 }}
          ring={routing ? AI : undefined}
          surface="bg-(--home-hub)"
        >
          <div className="flex flex-1 flex-col items-center justify-center px-2 text-center">
            <span className="relative flex size-12 items-center justify-center">
              <motion.span
                className="absolute inset-0 rounded-full border border-dashed border-(--home-oversight)/80"
                animate={playing ? { rotate: 360 } : { rotate: 0 }}
                transition={
                  playing ? { duration: 24, ease: "linear", repeat: Infinity } : { duration: 0 }
                }
              />
              <Waypoints className="size-5 text-(--home-oversight)" />
            </span>
            <p className="mt-2 text-[13px] leading-tight font-semibold text-(--home-ink)">
              Supervisor agent
            </p>
            <p className="mt-1 text-xs leading-tight text-(--home-muted)">Routes every task</p>
          </div>
        </BoardCard>
        {scenarios.map((scenario, lane) => (
          <Lane
            key={scenario.id}
            scenario={scenario}
            row={lane + 1}
            reached={lane === active ? reached : undefined}
            animated={animated}
          />
        ))}
      </div>
    </div>
  );
}

const WAITING = "Waiting for a draft";
const REVIEWING = "Reviewing the draft";

type LaneProps = { scenario: Scenario; row: number; reached?: number; animated: boolean };

function Lane({ scenario, row, reached, animated }: LaneProps) {
  const focus = reached !== undefined;
  const motionKey = `${scenario.id}-${reached}`;
  const wire = (index: number) =>
    !focus || index > reached ? "idle" : index < reached || !animated ? "done" : "drawing";
  const approved = reached === 3;
  const reviewing = reached === 2;
  const status = !focus || approved ? scenario.outcome : reviewing ? REVIEWING : WAITING;
  const lane = { row, className: focus ? "opacity-100" : "opacity-45" };

  return (
    <>
      <LaneCard
        {...lane}
        column={1}
        title={sources[scenario.source].label}
        detail={sources[scenario.source].detail}
        ring={reached === 0 ? AI : undefined}
      />
      <Wire row={row} column={2} state={wire(0)} motionKey={motionKey} />
      <Wire row={row} column={4} state={wire(1)} motionKey={motionKey} />
      <LaneCard
        {...lane}
        column={5}
        dashed
        title={agents[scenario.agent].label}
        detail={agents[scenario.agent].detail}
        ring={reached === 1 || reviewing ? AI : undefined}
      />
      <Wire row={row} column={6} state={wire(2)} motionKey={motionKey} />
      <LaneCard
        {...lane}
        column={7}
        title={scenario.approver.name}
        detail={scenario.approver.role}
        icon={<UserCheck className="size-3.5 shrink-0 text-(--home-care)" />}
        ring={reviewing || approved ? CARE : undefined}
      >
        <motion.div
          key={motionKey}
          className="mt-2"
          initial={focus && animated ? { opacity: 0 } : false}
          animate={{ opacity: 1 }}
          transition={{ delay: approved ? DRAW_S / 2 : 0, duration: 0.3 }}
        >
          <StatusPill
            tone={approved ? "done" : reviewing ? "reviewing" : "waiting"}
            label={status}
            reserve={[WAITING, REVIEWING, scenario.outcome]}
          />
          <AiAssistedMark shown={approved} />
        </motion.div>
      </LaneCard>
    </>
  );
}

type LaneCardProps = {
  row: number;
  column: number;
  title: string;
  detail: string;
  className: string;
  dashed?: boolean;
  ring?: string;
  icon?: React.ReactNode;
  children?: React.ReactNode;
};

function LaneCard({
  row,
  column,
  title,
  detail,
  className,
  dashed,
  ring,
  icon,
  children,
}: LaneCardProps) {
  return (
    <BoardCard
      style={{ gridRow: row, gridColumn: column }}
      className={className}
      ring={ring}
      dashed={dashed}
    >
      <CardHeader icon={icon} title={title} />
      <CardBody>
        {detail}
        {children}
      </CardBody>
    </BoardCard>
  );
}

type WireProps = {
  row: number;
  column: number;
  state: "idle" | "drawing" | "done";
  motionKey: string;
};

function Wire({ row, column, state, motionKey }: WireProps) {
  return (
    <div
      style={{ gridRow: row, gridColumn: column }}
      className="relative mr-1 h-px self-center bg-(--home-line)"
    >
      {state === "drawing" && (
        <motion.span
          key={motionKey}
          className="absolute inset-0 origin-left"
          style={{ backgroundColor: AI, boxShadow: `0 0 10px ${AI}` }}
          initial={{ scaleX: 0 }}
          animate={{ scaleX: 1 }}
          transition={{ duration: DRAW_S, ease: "easeInOut" }}
        />
      )}
      {state === "done" && <span className="absolute inset-0 bg-(--home-oversight)/60" />}
      <Arrowhead colour={state === "done" ? AI : "var(--home-line)"} />
    </div>
  );
}

function Narration({ scenario, currentStep }: { scenario: Scenario; currentStep?: number }) {
  return (
    <div>
      <h3 className="text-2xl leading-tight font-semibold font-stretch-[85%] text-(--home-ink)">
        {scenario.title}
      </h3>
      <p className="mt-2 text-(--home-muted)">{scenario.summary}</p>
      <ol aria-label="Steps in this scenario" className="mt-6 space-y-1">
        {scenarioSteps(scenario).map((step, index) => (
          <li
            key={`${scenario.id}-${index}`}
            aria-current={index === currentStep ? "step" : undefined}
            className={cn(
              "grid grid-cols-[1.5rem_1fr] gap-x-2 rounded-r-lg border-l-2 py-2.5 pr-3 pl-3 transition-colors duration-300",
              index === currentStep
                ? index === 3
                  ? "border-(--home-care) bg-(--home-surface)"
                  : "border-(--home-oversight) bg-(--home-surface)"
                : "border-transparent",
            )}
          >
            <span aria-hidden="true" className="text-base text-(--home-muted) tabular-nums">
              {index + 1}
            </span>
            <div>
              <p className="flex flex-wrap items-center gap-x-1.5 text-base font-semibold text-(--home-ink)">
                <span>{nodeLabel(step.from, scenario)}</span>
                <ArrowRight aria-hidden="true" className="size-3.5 text-(--home-oversight)" />
                <span className="sr-only">to</span>
                <span>{nodeLabel(step.to, scenario)}</span>
              </p>
              <p className="mt-1 text-base leading-relaxed text-(--home-muted)">{step.text}</p>
            </div>
          </li>
        ))}
      </ol>
      <div className="mt-8">
        <Availability modules={scenario.modules} note={aiNote} />
      </div>
    </div>
  );
}
