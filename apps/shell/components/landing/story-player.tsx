"use client";

import {
  useEffect,
  useEffectEvent,
  useState,
  useSyncExternalStore,
  type CSSProperties,
  type RefObject,
} from "react";
import { motion, useReducedMotion } from "framer-motion";
import { Pause, Play } from "lucide-react";
import { cn } from "@tesserix/web";

export const STEP_MS = 3400;

const noSubscription = () => () => {};

// The server cannot know the visitor's motion preference, so it renders every board still.
function useHydrated() {
  return useSyncExternalStore(
    noSubscription,
    () => true,
    () => false,
  );
}

// A board the layout hides has no box, while one scrolled away keeps its size.
function useBoardVisibility(ref: RefObject<Element | null>) {
  const [visibility, setVisibility] = useState({ inView: false, rendered: false });

  useEffect(() => {
    const board = ref.current;
    if (!board) return;
    const observer = new IntersectionObserver(
      (entries) => {
        const entry = entries[entries.length - 1];
        setVisibility({
          inView: entry.isIntersecting,
          rendered: entry.isIntersecting || entry.boundingClientRect.width > 0,
        });
      },
      { threshold: 0.5 },
    );
    observer.observe(board);
    return () => observer.disconnect();
  }, [ref]);

  return visibility;
}

export function useStoryPlayer(
  ref: RefObject<Element | null>,
  length: number,
  onFinish?: () => void,
) {
  const [step, setStep] = useState(0);
  const [paused, setPaused] = useState(false);
  const { inView, rendered } = useBoardVisibility(ref);
  const hydrated = useHydrated();
  const prefersReducedMotion = useReducedMotion();
  const animated = hydrated && rendered && prefersReducedMotion !== true;
  const playing = animated && !paused && inView;
  const finish = useEffectEvent(() => onFinish?.());

  useEffect(() => {
    if (!playing) return;
    const timer = setTimeout(() => {
      if (step + 1 < length) {
        setStep(step + 1);
      } else {
        finish();
        setStep(0);
      }
    }, STEP_MS);
    return () => clearTimeout(timer);
  }, [playing, step, length]);

  return {
    step,
    setStep,
    paused,
    togglePaused: () => setPaused(!paused),
    animated,
    playing,
    inView,
  };
}

type StoryPickerProps = {
  label: string;
  items: readonly { id: string; title: string; colour: string }[];
  selected: number;
  onSelect: (index: number) => void;
  progress?: { step: number; length: number; playing: boolean };
};

// The active chip fills toward the next story so visitors can see the board moves on by itself.
export function StoryPicker({ label, items, selected, onSelect, progress }: StoryPickerProps) {
  return (
    <div
      role="group"
      aria-label={label}
      className="-mx-6 flex scroll-px-6 gap-2 overflow-x-auto px-6 pb-2 sm:mx-0 sm:flex-wrap sm:overflow-visible sm:px-0 sm:pb-0"
    >
      {items.map((item, index) => (
        <button
          key={item.id}
          type="button"
          aria-pressed={index === selected}
          onClick={() => onSelect(index)}
          style={{ "--story": item.colour } as CSSProperties}
          className={cn(
            "relative inline-flex min-h-11 shrink-0 items-center gap-2 overflow-hidden rounded-lg border px-4 text-sm whitespace-nowrap transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-foreground",
            index === selected
              ? "border-(--story) bg-(--story)/15 text-foreground"
              : "border-border text-muted-foreground hover:border-muted-foreground hover:text-foreground",
          )}
        >
          <span aria-hidden="true" className="size-2 rounded-full bg-(--story)" />
          {item.title}
          {progress && index === selected && (
            <span
              aria-hidden="true"
              data-testid="story-progress"
              className="absolute inset-x-4 bottom-1 h-0.5 overflow-hidden rounded-full bg-(--story)/20"
            >
              <motion.span
                className="block h-full rounded-full bg-(--story)"
                initial={{ width: `${(progress.step / progress.length) * 100}%` }}
                animate={{
                  width: `${((progress.step + (progress.playing ? 1 : 0)) / progress.length) * 100}%`,
                }}
                transition={{
                  duration: progress.playing ? STEP_MS / 1000 : 0.2,
                  ease: "linear",
                }}
              />
            </span>
          )}
        </button>
      ))}
    </div>
  );
}

type PlayToggleProps = { paused: boolean; onToggle: () => void };

export function PlayToggle({ paused, onToggle }: PlayToggleProps) {
  const label = paused ? "Play animation" : "Pause animation";
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      onClick={onToggle}
      className="inline-flex size-9 shrink-0 items-center justify-center rounded-full border border-border text-muted-foreground transition-colors hover:border-muted-foreground hover:text-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-foreground"
    >
      {paused ? (
        <Play aria-hidden="true" className="size-3.5 fill-current" />
      ) : (
        <Pause aria-hidden="true" className="size-3.5 fill-current" />
      )}
    </button>
  );
}
