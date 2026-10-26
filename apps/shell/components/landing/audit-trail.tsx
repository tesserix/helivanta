"use client";

import { useRef, type CSSProperties } from "react";
import { motion } from "framer-motion";
import { Banknote, Eye, Lock, Package, Undo2 } from "lucide-react";
import { cn } from "@tesserix/web";
import { Availability } from "./module-status";
import {
  trail,
  trailModules,
  trailNote,
  type TrailEntry,
  type TrailLane,
} from "./audit-trail-data";
import { PlayToggle, useStoryPlayer } from "./story-player";

const laneColour: Record<TrailLane, string> = {
  stock: "var(--night-stock)",
  money: "var(--night-oversight)",
  audit: "var(--night-ink)",
};

const laneName: Record<TrailLane, string> = {
  stock: "Stock",
  money: "Money",
  audit: "Audit",
};

const ROW = "grid grid-cols-[1fr_3.5rem_1fr] items-center";

function entryNumber(id: string): number {
  return trail.findIndex((entry) => entry.id === id) + 1;
}

export function AuditTrail() {
  const boardRef = useRef<HTMLDivElement>(null);
  const player = useStoryPlayer(boardRef, trail.length);
  const current = player.animated ? player.step : undefined;

  return (
    <div className="grid gap-x-12 gap-y-10 lg:grid-cols-[minmax(0,1fr)_21rem]">
      <div className="hidden min-w-0 md:block">
        <figure aria-label="Stock entries on the left and money entries on the right join one append-only trail in the middle. A correction adds a new entry and the original stays. An auditor reads the whole trail without changing it.">
          <div ref={boardRef}>
            <Ledger reached={current ?? trail.length - 1} animated={current !== undefined} />
          </div>
        </figure>
        <div className="mt-4 flex items-center justify-between gap-6">
          <ul aria-hidden="true" className="flex flex-wrap gap-x-6 gap-y-2 text-xs">
            {(Object.keys(laneName) as TrailLane[]).map((lane) => (
              <li key={lane} className="flex items-center gap-2 text-(--night-muted)">
                <span
                  className="size-2 rounded-full"
                  style={{ backgroundColor: laneColour[lane] }}
                />
                {laneName[lane]}
              </li>
            ))}
          </ul>
          {player.animated && <PlayToggle paused={player.paused} onToggle={player.togglePaused} />}
        </div>
      </div>
      <div>
        <ol aria-label="Entries in the trail" className="space-y-1">
          {trail.map((entry, index) => (
            <li
              key={entry.id}
              aria-current={index === current ? "step" : undefined}
              style={{ "--lane": laneColour[entry.lane] } as CSSProperties}
              className={cn(
                "grid grid-cols-[1.5rem_1fr] gap-x-2 rounded-r-lg border-l-2 py-2.5 pr-3 pl-3 transition-colors duration-300",
                index === current ? "border-(--lane) bg-(--night-surface)" : "border-transparent",
              )}
            >
              <span aria-hidden="true" className="text-sm text-(--night-muted) tabular-nums">
                {index + 1}
              </span>
              <div>
                <p className="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm font-semibold text-(--night-ink)">
                  {entry.actor}: {entry.title}
                  {entry.readOnly && <ReadOnlyTag />}
                </p>
                <p className="mt-1 text-sm leading-relaxed text-(--night-muted)">{entry.text}</p>
              </div>
            </li>
          ))}
        </ol>
        <div className="mt-8">
          <Availability modules={trailModules} note={trailNote} />
        </div>
      </div>
    </div>
  );
}

function ReadOnlyTag() {
  return (
    <span className="inline-flex items-center gap-1 rounded-full border border-(--night-line) px-2 py-0.5 text-[11px] font-medium text-(--night-ink)">
      <Lock aria-hidden="true" className="size-3" />
      Read only
    </span>
  );
}

function Ledger({ reached, animated }: { reached: number; animated: boolean }) {
  const shown = trail.slice(0, reached + 1);
  const latest = animated ? trail[reached] : undefined;
  const corrected = new Set(shown.flatMap((entry) => (entry.corrects ? [entry.corrects] : [])));

  return (
    <div
      aria-hidden="true"
      className="relative overflow-hidden rounded-2xl border border-(--night-line) bg-(--night-board) px-6 pt-5 pb-6"
    >
      <div className={cn(ROW, "text-xs font-medium text-(--night-muted)")}>
        <p className="flex items-center gap-2">
          <Package className="size-4 text-(--night-stock)" />
          Stock
        </p>
        <p className="text-center">Trail</p>
        <p className="flex items-center justify-end gap-2">
          Money
          <Banknote className="size-4 text-(--night-oversight)" />
        </p>
      </div>
      <div className="relative mt-4">
        <div className="absolute inset-y-0 left-1/2 w-px -translate-x-1/2 bg-(--night-line)" />
        <ol aria-label="Append-only trail" className="relative space-y-2">
          {shown.map((entry, index) => (
            <Row
              key={entry.id}
              entry={entry}
              index={index}
              fresh={latest?.id === entry.id}
              animated={animated}
              kept={corrected.has(entry.id)}
              highlighted={latest?.corrects === entry.id}
            />
          ))}
        </ol>
        <div className="mt-2 space-y-2">
          {trail.slice(shown.length).map((entry) => (
            <div key={entry.id} className={cn(ROW, "h-13")}>
              <span className="col-start-2 mx-auto size-7 rounded-full border border-dashed border-(--night-line) bg-(--night-board)" />
            </div>
          ))}
        </div>
        {latest?.readOnly && (
          <motion.div
            key="audit-scan"
            className="pointer-events-none absolute inset-x-0 h-24 bg-linear-to-b from-transparent via-(--night-oversight)/20 to-transparent"
            initial={{ top: "-25%" }}
            animate={{ top: "100%" }}
            transition={{ duration: 2.6, ease: "linear" }}
          />
        )}
      </div>
    </div>
  );
}

type RowProps = {
  entry: TrailEntry;
  index: number;
  fresh: boolean;
  animated: boolean;
  kept: boolean;
  highlighted: boolean;
};

function Row({ entry, index, fresh, animated, kept, highlighted }: RowProps) {
  const enter = entry.lane === "stock" ? -16 : entry.lane === "money" ? 16 : 0;
  const card = (
    <div
      className={cn(
        "w-full max-w-md rounded-lg border bg-(--night-surface) px-3 py-2 transition-[border-color,box-shadow] duration-500",
        fresh && "border-(--lane) shadow-[0_0_24px_-10px_var(--lane)]",
        !fresh && highlighted && "border-dashed border-(--lane)",
        !fresh && !highlighted && "border-(--night-line)",
      )}
    >
      <p className="flex items-center justify-between gap-2 text-[11px] text-(--night-muted)">
        {entry.actor}
        {entry.readOnly && <ReadOnlyTag />}
      </p>
      <p className="mt-0.5 flex items-baseline justify-between gap-3 text-[13px]">
        <span className="font-semibold text-(--night-ink)">{entry.title}</span>
        <span className="text-(--lane) tabular-nums">{entry.figure}</span>
      </p>
      {entry.corrects && (
        <p className="mt-1 flex items-center gap-1 text-[11px] text-(--night-muted)">
          <Undo2 className="size-3" />
          Reverses part of entry {entryNumber(entry.corrects)}
        </p>
      )}
      {kept && (
        <p className="mt-1 flex items-center gap-1 text-[11px] text-(--night-muted)">
          <Lock className="size-3" />
          Kept as recorded
        </p>
      )}
    </div>
  );
  const node = (
    <span className="col-start-2 mx-auto flex size-7 items-center justify-center rounded-full border border-(--lane) bg-(--night-board) text-[11px] text-(--lane) tabular-nums">
      {entry.readOnly ? <Eye className="size-3.5" /> : index + 1}
    </span>
  );
  const link = <span className="h-px w-5 shrink-0 bg-(--lane)/60" />;

  return (
    <motion.li
      style={{ "--lane": laneColour[entry.lane] } as CSSProperties}
      initial={animated && fresh ? { opacity: 0, x: enter, y: enter ? 0 : 8 } : false}
      animate={{ opacity: 1, x: 0, y: 0 }}
      transition={{ duration: 0.45, ease: "easeOut" }}
      className={ROW}
    >
      {entry.lane === "audit" ? (
        <div className="col-span-3 flex justify-center">{card}</div>
      ) : entry.lane === "stock" ? (
        <>
          <div className="flex items-center justify-end">
            {card}
            {link}
          </div>
          {node}
        </>
      ) : (
        <>
          {node}
          <div className="col-start-3 flex items-center">
            {link}
            {card}
          </div>
        </>
      )}
    </motion.li>
  );
}
