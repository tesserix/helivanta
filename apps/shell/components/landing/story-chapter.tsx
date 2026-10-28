import type { ReactNode } from "react";
import { homeWidth } from "@/app/page-width";

// Server components: @tesserix/web's cn() is a client export, so classes are joined here.
const classes = (...parts: (string | false)[]) => parts.filter(Boolean).join(" ");

type StoryChapterProps = {
  id: string;
  number: number;
  title: string;
  lede: string;
  // Bands alternate so each chapter has a visible start and end (#1413 D2).
  tone: "plain" | "band";
  // A chapter of capabilities that are not available yet is outlined, never filled.
  upcoming?: boolean;
  children: ReactNode;
};

export function StoryChapter({
  id,
  number,
  title,
  lede,
  tone,
  upcoming = false,
  children,
}: StoryChapterProps) {
  const headingId = `${id}-heading`;
  return (
    <section
      id={id}
      aria-labelledby={headingId}
      className={classes(
        "scroll-mt-28 py-16 sm:py-20",
        tone === "band" && "border-y border-(--home-line) bg-(--home-surface)",
      )}
    >
      <div className={homeWidth}>
        <div className="grid gap-x-6 gap-y-4 sm:grid-cols-[2.5rem_minmax(0,1fr)]">
          <span
            aria-hidden="true"
            className={classes(
              "flex size-10 items-center justify-center rounded-lg text-base font-semibold tabular-nums",
              upcoming
                ? "border border-dashed border-(--home-muted) text-(--home-ink)"
                : "bg-(--home-ink) text-(--home-canvas)",
            )}
          >
            {number}
          </span>
          <div>
            <h2
              id={headingId}
              className="text-3xl leading-tight font-semibold tracking-tight font-stretch-[85%] sm:text-4xl"
            >
              {title}
            </h2>
            <p className="mt-3 max-w-2xl leading-relaxed text-(--home-muted)">{lede}</p>
          </div>
        </div>
        <div className="mt-10">{children}</div>
      </div>
    </section>
  );
}

type StoryActProps = { title: string; summary: string; upcoming?: boolean };

// An act groups chapters by whether they are available today (#1413 D4).
export function StoryAct({ title, summary, upcoming = false }: StoryActProps) {
  return (
    <div className={`${homeWidth} pt-16`}>
      <div
        className={classes(
          "flex flex-wrap items-baseline gap-x-4 gap-y-1 border-t-2 pt-4",
          upcoming ? "border-dashed border-(--home-muted)" : "border-(--home-ink)",
        )}
      >
        <p className="text-sm font-semibold text-(--home-ink)">{title}</p>
        <p className="text-sm text-(--home-muted)">{summary}</p>
      </div>
    </div>
  );
}
