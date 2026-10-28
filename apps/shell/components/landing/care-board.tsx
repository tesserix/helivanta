"use client";

import { Fragment, useRef, useState, type CSSProperties } from "react";
import { motion } from "framer-motion";
import { ArrowRight, HeartPulse } from "lucide-react";
import { cn } from "@tesserix/web";
import { Availability } from "./module-status";
import { stories, teams, type Flow, type Story, type TeamId } from "./care-board-data";
import { PlayToggle, StoryPicker, useStoryPlayer } from "./story-player";

const DRAW_S = 0.7;
const WIDTH = 1000;
const HEIGHT = 660;
const HUB = { x: 500, y: 300 };

const flowColour: Record<Flow, string> = {
  care: "var(--home-care)",
  stock: "var(--home-stock)",
  oversight: "var(--home-oversight)",
};

const flowName: Record<Flow, string> = {
  care: "Patient care",
  stock: "Medicines and stock",
  oversight: "Approvals and staffing",
};

const places: Record<TeamId, { x: number; y: number }> = {
  governance: { x: 120, y: 150 },
  leadership: { x: 120, y: 290 },
  heads: { x: 120, y: 430 },
  wards: { x: 880, y: 85 },
  pharmacy: { x: 880, y: 170 },
  lab: { x: 880, y: 255 },
  store: { x: 880, y: 340 },
  maintenance: { x: 880, y: 425 },
  emergency: { x: 880, y: 510 },
  hr: { x: 300, y: 600 },
  finance: { x: 500, y: 600 },
  purchase: { x: 700, y: 600 },
};

const laneLabels = [
  { label: "Management", x: 35, y: 95 },
  { label: "Departments and services", x: 795, y: 30 },
  { label: "Administration", x: 35, y: 600 },
];

const teamIds = Object.keys(places) as TeamId[];

function at(x: number, y: number): CSSProperties {
  return { left: `${(x / WIDTH) * 100}%`, top: `${(y / HEIGHT) * 100}%` };
}

function route(id: TeamId, towardHub: boolean): string {
  const { x, y } = places[id];
  const lane = teams[id].lane;
  const [sx, sy, ax, ay, bx, by] =
    lane === "management"
      ? [x + 85, y, x + 225, y, HUB.x - 170, HUB.y]
      : lane === "departments"
        ? [x - 85, y, x - 225, y, HUB.x + 170, HUB.y]
        : [x, y - 30, x, y - 130, HUB.x, HUB.y + 160];
  return towardHub
    ? `M${sx} ${sy} C${ax} ${ay} ${bx} ${by} ${HUB.x} ${HUB.y}`
    : `M${HUB.x} ${HUB.y} C${bx} ${by} ${ax} ${ay} ${sx} ${sy}`;
}

export function CareBoard() {
  const [storyIndex, setStoryIndex] = useState(0);
  const boardRef = useRef<HTMLDivElement>(null);
  const story = stories[storyIndex];
  const player = useStoryPlayer(boardRef, story.steps.length, () =>
    setStoryIndex((storyIndex + 1) % stories.length),
  );
  const { step: stepIndex, animated, inView } = player;

  function choose(index: number) {
    setStoryIndex(index);
    player.setStep(0);
  }

  return (
    <div style={{ "--flow": flowColour[story.flow] } as CSSProperties}>
      <StoryPicker
        label="Hospital stories"
        items={stories.map((item) => ({ ...item, colour: flowColour[item.flow] }))}
        selected={storyIndex}
        onSelect={choose}
        progress={
          animated
            ? { step: stepIndex, length: story.steps.length, playing: player.playing }
            : undefined
        }
      />
      <div className="mt-8 grid gap-x-12 gap-y-10 lg:grid-cols-[minmax(0,1fr)_21rem]">
        <div className="hidden min-w-0 md:block">
          <figure aria-label="Management on the left, departments on the right, administration below, and Helivanta in the middle. Each request travels from the team that sends it, through Helivanta, to the team that acts on it.">
            <div ref={boardRef}>
              <Board
                story={story}
                stepIndex={stepIndex}
                animated={animated}
                replay={inView ? "seen" : "unseen"}
              />
            </div>
          </figure>
          <div className="mt-4 flex items-center justify-between gap-6">
            <ul aria-hidden="true" className="flex flex-wrap gap-x-6 gap-y-2 text-xs">
              {(Object.keys(flowName) as Flow[]).map((flow) => (
                <li key={flow} className="flex items-center gap-2 text-(--home-muted)">
                  <span
                    className="h-0.5 w-6 rounded-full"
                    style={{ backgroundColor: flowColour[flow] }}
                  />
                  {flowName[flow]}
                </li>
              ))}
            </ul>
            {animated && <PlayToggle paused={player.paused} onToggle={player.togglePaused} />}
          </div>
        </div>
        <Narration story={story} currentStep={animated ? stepIndex : undefined} />
      </div>
    </div>
  );
}

type BoardProps = { story: Story; stepIndex: number; animated: boolean; replay: string };

function Board({ story, stepIndex, animated, replay }: BoardProps) {
  const current = animated ? story.steps[stepIndex] : undefined;
  const trail = animated ? story.steps.slice(0, stepIndex) : story.steps;
  const visited = new Set(trail.flatMap((step) => [step.from, step.to]));
  const motionKey = `${story.id}-${stepIndex}-${replay}`;
  const arrive = { delay: DRAW_S * 2 + 0.15, duration: 0.3 };

  return (
    <div
      aria-hidden="true"
      className="relative aspect-[1000/660] rounded-2xl border border-(--home-line) bg-(--home-board) bg-[radial-gradient(var(--home-line)_1px,transparent_1px)] bg-size-[28px_28px]"
    >
      <svg viewBox={`0 0 ${WIDTH} ${HEIGHT}`} fill="none" className="absolute inset-0 size-full">
        <defs>
          <filter id="care-board-glow" x="-20%" y="-20%" width="140%" height="140%">
            <feGaussianBlur stdDeviation="6" />
          </filter>
        </defs>
        {teamIds.map((id) => (
          <path key={id} d={route(id, true)} stroke="var(--home-line)" strokeWidth={1.5} />
        ))}
        {trail.map((step, index) => (
          <g
            key={`${story.id}-${index}`}
            stroke="var(--flow)"
            strokeOpacity={animated ? 0.4 : 0.8}
            strokeWidth={2}
          >
            <path d={route(step.from, true)} />
            <path d={route(step.to, false)} />
          </g>
        ))}
        {current && (
          <g key={motionKey} stroke="var(--flow)" strokeLinecap="round">
            {[route(current.from, true), route(current.to, false)].map((d, leg) => (
              <Fragment key={d}>
                {[
                  { width: 9, opacity: 0.45, filter: "url(#care-board-glow)" },
                  { width: 2.5, opacity: 1, filter: undefined },
                ].map((stroke) => (
                  <motion.path
                    key={stroke.width}
                    d={d}
                    strokeWidth={stroke.width}
                    strokeOpacity={stroke.opacity}
                    filter={stroke.filter}
                    initial={{ pathLength: 0 }}
                    animate={{ pathLength: 1 }}
                    transition={{
                      duration: DRAW_S,
                      delay: leg * (DRAW_S + 0.15),
                      ease: "easeInOut",
                    }}
                  />
                ))}
              </Fragment>
            ))}
            <motion.circle
              cx={HUB.x}
              cy={HUB.y}
              r={64}
              strokeWidth={2}
              initial={{ scale: 1, opacity: 0 }}
              animate={{ scale: 1.55, opacity: [0, 0.9, 0] }}
              transition={{ delay: DRAW_S, duration: 1 }}
            />
          </g>
        )}
        <circle
          cx={HUB.x}
          cy={HUB.y}
          r={64}
          fill="var(--home-hub)"
          stroke="var(--home-line)"
          strokeWidth={1.5}
        />
        <circle
          cx={HUB.x}
          cy={HUB.y}
          r={54}
          stroke="var(--flow)"
          strokeOpacity={0.7}
          strokeDasharray="2 6"
        />
      </svg>

      <div
        style={at(HUB.x, HUB.y)}
        className="absolute flex -translate-x-1/2 -translate-y-1/2 flex-col items-center text-center"
      >
        <HeartPulse className="size-5 text-(--flow)" />
        <p className="mt-1 text-sm font-semibold text-(--home-ink)">Helivanta</p>
        <p className="text-[11px] leading-tight text-(--home-muted)">Shared records</p>
      </div>

      {laneLabels.map((lane) => (
        <p
          key={lane.label}
          style={at(lane.x, lane.y)}
          className="absolute -translate-y-1/2 text-xs font-medium text-(--home-muted)"
        >
          {lane.label}
        </p>
      ))}

      {teamIds.map((id) => {
        const sending = current?.from === id;
        const receiving = current?.to === id;
        return (
          <div
            key={id}
            style={at(places[id].x, places[id].y)}
            className={cn(
              "absolute w-[17%] -translate-x-1/2 -translate-y-1/2 rounded-lg border bg-(--home-surface) px-3 py-2 transition-[border-color,box-shadow] duration-500",
              sending && "border-(--flow) shadow-[0_0_28px_-8px_var(--flow)]",
              !sending && visited.has(id) && (animated ? "border-(--flow)/45" : "border-(--flow)"),
              !sending && !visited.has(id) && "border-(--home-line)",
            )}
          >
            <p className="text-[13px] leading-tight font-semibold text-(--home-ink)">
              {teams[id].label}
            </p>
            <p className="mt-0.5 text-[11px] leading-tight text-(--home-muted)">
              {teams[id].detail}
            </p>
            {receiving && (
              <motion.span
                key={motionKey}
                className="pointer-events-none absolute -inset-px rounded-lg border-2 border-(--flow) shadow-[0_0_28px_-8px_var(--flow)]"
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                transition={arrive}
              />
            )}
          </div>
        );
      })}

      {current && (
        <motion.div
          key={motionKey}
          style={at(HUB.x, 110)}
          className="absolute w-[30%] -translate-x-1/2 -translate-y-1/2 text-center"
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          transition={{ duration: 0.4 }}
        >
          <p className="text-[11px] text-(--home-muted)">
            Step {stepIndex + 1} of {story.steps.length}
          </p>
          <p className="mt-1 text-[13px] leading-snug text-(--home-ink)">{current.text}</p>
        </motion.div>
      )}
    </div>
  );
}

function Narration({ story, currentStep }: { story: Story; currentStep: number | undefined }) {
  return (
    <div>
      <h3 className="text-2xl leading-tight font-semibold font-stretch-[85%] text-(--home-ink)">
        {story.title}
      </h3>
      <p className="mt-2 text-(--home-muted)">{story.summary}</p>
      <ol aria-label="Steps in this story" className="mt-6 space-y-1">
        {story.steps.map((step, index) => (
          <li
            key={`${story.id}-${index}`}
            aria-current={index === currentStep ? "step" : undefined}
            className={cn(
              "grid grid-cols-[1.5rem_1fr] gap-x-2 rounded-r-lg border-l-2 py-2.5 pr-3 pl-3 transition-colors duration-300",
              index === currentStep ? "border-(--flow) bg-(--home-surface)" : "border-transparent",
            )}
          >
            <span aria-hidden="true" className="text-sm text-(--home-muted) tabular-nums">
              {index + 1}
            </span>
            <div>
              <p className="flex flex-wrap items-center gap-x-1.5 text-sm font-semibold text-(--home-ink)">
                <span>{teams[step.from].label}</span>
                <ArrowRight aria-hidden="true" className="size-3.5 text-(--flow)" />
                <span className="sr-only">to</span>
                <span>{teams[step.to].label}</span>
              </p>
              <p className="mt-1 text-sm leading-relaxed text-(--home-muted)">{step.text}</p>
            </div>
          </li>
        ))}
      </ol>
      <div className="mt-8">
        <Availability modules={story.modules} note={story.note} />
      </div>
    </div>
  );
}
