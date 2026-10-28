"use client";

import { useRef } from "react";
import { motion } from "framer-motion";
import { Check } from "lucide-react";
import { cn } from "@tesserix/web";
import { moduleStatus } from "@/app/platform-catalog";
import { ModuleStatusLabel } from "./module-status";
import { journeyNote, patient, stations } from "./patient-journey-data";
import { PlayToggle, useStoryPlayer } from "./story-player";

const WALK_S = 1.1;

const along = (index: number) => `${(index / (stations.length - 1)) * 100}%`;

export function PatientJourney() {
  const boardRef = useRef<HTMLDivElement>(null);
  const player = useStoryPlayer(boardRef, stations.length);
  const current = player.animated ? player.step : undefined;

  return (
    <div>
      <div className="flex items-end justify-between gap-6">
        <p className="max-w-xl text-(--home-muted)">{patient.summary}</p>
        {player.animated && (
          <div className="max-md:hidden">
            <PlayToggle paused={player.paused} onToggle={player.togglePaused} />
          </div>
        )}
      </div>
      <figure
        aria-label="One patient walks through the hospital, station by station. Each station adds an entry to one shared record, written by the team that did the work."
        className="mt-6 hidden md:block"
      >
        <div ref={boardRef}>
          <Corridor reached={current ?? stations.length - 1} animated={current !== undefined} />
        </div>
      </figure>
      <ol
        aria-label="Stations on the journey"
        className="mt-8 grid gap-3 sm:grid-cols-2 lg:grid-cols-4"
      >
        {stations.map((station, index) => (
          <li
            key={station.id}
            aria-current={index === current ? "step" : undefined}
            className={cn(
              "flex flex-col rounded-xl border p-4 transition-colors duration-500",
              index === current
                ? "border-(--home-care) bg-(--home-surface)"
                : "border-(--home-line)",
            )}
          >
            <p className="flex items-baseline gap-2 font-semibold">
              <span aria-hidden="true" className="text-sm text-(--home-muted) tabular-nums">
                {index + 1}
              </span>
              {station.label}
            </p>
            <p className="mt-0.5 text-xs text-(--home-care)">Written by {station.owner}</p>
            <p className="mt-2 flex-1 text-sm leading-relaxed text-(--home-muted)">
              {station.text}
            </p>
            <div className="mt-3">
              <ModuleStatusLabel status={moduleStatus(station.module)} />
            </div>
          </li>
        ))}
      </ol>
      <p className="mt-4 max-w-3xl text-sm leading-relaxed text-(--home-muted)">{journeyNote}</p>
    </div>
  );
}

function Corridor({ reached, animated }: { reached: number; animated: boolean }) {
  const arriving = animated && reached > 0;
  const walk = { duration: arriving ? WALK_S : 0, ease: "easeInOut" } as const;
  const entries = stations.slice(0, reached + 1);

  return (
    <div
      aria-hidden="true"
      className="rounded-2xl border border-(--home-line) bg-(--home-board) bg-[radial-gradient(var(--home-line)_1px,transparent_1px)] bg-size-[28px_28px]"
    >
      <div className="relative mx-4 grid grid-cols-8 pt-16 pb-8">
        <div className="absolute top-[calc(4rem+1.625rem)] right-[6.25%] left-[6.25%] -translate-y-1/2 border-t-2 border-dashed border-(--home-line)">
          <motion.div
            className="absolute -top-0.5 left-0 h-0.5 rounded-full bg-(--home-care)/70"
            initial={false}
            animate={{ width: along(reached) }}
            transition={walk}
          />
          {animated && (
            <motion.div
              className="absolute -top-px size-0"
              initial={false}
              animate={{ left: along(reached) }}
              transition={walk}
            >
              {/* Clears the 1.625rem station radius with a 0.625rem gap. */}
              <span className="absolute bottom-[2.25rem] left-0 flex -translate-x-1/2 flex-col items-center">
                <span className="rounded-full bg-(--home-care) px-2.5 py-0.5 text-[11px] font-semibold whitespace-nowrap text-(--home-canvas) shadow-[0_4px_14px_-4px_var(--home-care)]">
                  {patient.name}
                </span>
                <span className="size-0 border-x-[5px] border-t-[5px] border-x-transparent border-t-(color:--home-care)" />
              </span>
              <span className="absolute size-3 -translate-1/2 rounded-full bg-(--home-ink) shadow-[0_0_14px_4px_var(--home-care)]" />
            </motion.div>
          )}
        </div>
        {stations.map((station, index) => {
          const Icon = station.icon;
          const here = animated && index === reached;
          const state = here ? "current" : index <= reached ? "done" : "ahead";
          const delay = { transitionDelay: here && arriving ? `${WALK_S}s` : "0s" };
          return (
            <div
              key={station.id}
              data-station={station.id}
              data-state={state}
              className="flex flex-col items-center px-1 text-center"
            >
              <div
                style={delay}
                className={cn(
                  "relative z-10 flex size-13 items-center justify-center rounded-full border bg-(--home-surface) transition-[border-color,background-color,box-shadow] duration-500",
                  state === "ahead" ? "border-(--home-line)" : "border-(--home-care)",
                  here &&
                    "bg-[color-mix(in_oklab,var(--home-care)_18%,var(--home-surface))] shadow-[0_0_26px_-4px_var(--home-care)]",
                )}
              >
                {here && (
                  <motion.span
                    className="absolute inset-0 rounded-full border border-(--home-care)"
                    initial={{ scale: 1, opacity: 0 }}
                    animate={{ scale: 1.45, opacity: [0, 0.7, 0] }}
                    transition={{
                      delay: arriving ? WALK_S : 0,
                      duration: 1.6,
                      repeat: Infinity,
                      ease: "easeOut",
                    }}
                  />
                )}
                <Icon
                  style={delay}
                  className={cn(
                    "size-5 transition-colors duration-500",
                    state === "ahead" ? "text-(--home-muted)" : "text-(--home-care)",
                  )}
                />
                {state === "done" && (
                  <span className="absolute -right-0.5 -bottom-0.5 flex size-4.5 items-center justify-center rounded-full border-2 border-(--home-board) bg-(--home-care) text-(--home-canvas)">
                    <Check strokeWidth={3.5} className="size-2.5" />
                  </span>
                )}
              </div>
              <p className="mt-3 text-[13px] leading-tight font-semibold text-(--home-ink)">
                {station.label}
              </p>
              <p className="mt-0.5 text-[11px] leading-tight text-(--home-muted)">
                {station.owner}
              </p>
            </div>
          );
        })}
      </div>
      <div className="border-t border-(--home-line) px-6 py-5">
        <div className="flex items-baseline justify-between gap-4">
          <p className="text-sm font-semibold text-(--home-ink)">Shared patient record</p>
          <p className="text-xs text-(--home-muted) tabular-nums">
            {patient.name}, {patient.number}
          </p>
        </div>
        <div className="mt-3 grid grid-flow-col grid-cols-2 grid-rows-4 gap-x-6 gap-y-2">
          <ol aria-label="Shared patient record" className="contents">
            {entries.map((station, index) => (
              <motion.li
                key={station.id}
                initial={animated && index === reached ? { opacity: 0, x: -8 } : false}
                animate={{ opacity: 1, x: 0 }}
                transition={{ delay: arriving ? WALK_S : 0, duration: 0.35 }}
                className={cn(
                  "flex items-center justify-between gap-3 rounded-lg border px-3 py-1.5 text-[13px]",
                  animated && index === reached
                    ? "border-(--home-care) bg-(--home-care)/10"
                    : "border-(--home-line) bg-(--home-surface)",
                )}
              >
                <span className="truncate text-(--home-ink)">{station.record}</span>
                <span className="shrink-0 text-[11px] text-(--home-care)">{station.owner}</span>
              </motion.li>
            ))}
          </ol>
          {stations.slice(entries.length).map((station) => (
            <div
              key={station.id}
              className="rounded-lg border border-dashed border-(--home-line)/70 py-1.5"
            >
              &nbsp;
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
