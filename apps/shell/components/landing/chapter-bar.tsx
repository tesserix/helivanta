"use client";

import { useEffect, useState } from "react";
import { cn } from "@tesserix/web";
import { homeWidth } from "@/app/page-width";

export type ChapterLink = { id: string; number: number; label: string };
export type ChapterGroup = { label: string; chapters: readonly ChapterLink[] };

// Sticky story navigation (#1413 D5). Without JavaScript the links are plain anchors.
export function ChapterBar({ groups }: { groups: readonly ChapterGroup[] }) {
  const [current, setCurrent] = useState<string>();

  useEffect(() => {
    const ids = groups.flatMap((group) => group.chapters.map((chapter) => chapter.id));
    const sections = ids
      .map((id) => document.getElementById(id))
      .filter((section): section is HTMLElement => section !== null);
    // A chapter is current while it crosses the band just below the bar.
    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) if (entry.isIntersecting) setCurrent(entry.target.id);
      },
      { rootMargin: "-30% 0px -65% 0px" },
    );
    for (const section of sections) observer.observe(section);
    return () => observer.disconnect();
  }, [groups]);

  return (
    <nav
      aria-label="Story chapters"
      className="sticky top-0 z-20 border-b border-(--home-line) bg-(--home-board)/90 backdrop-blur-md"
    >
      <div className={cn(homeWidth, "flex items-center overflow-x-auto")}>
        {groups.map((group, index) => (
          <div
            key={group.label}
            className={cn(
              "flex shrink-0 items-center",
              index > 0 && "ml-4 border-l border-(--home-line) pl-4",
            )}
          >
            <p className="pr-2 text-[13px] font-semibold whitespace-nowrap text-(--home-muted)">
              {group.label}
            </p>
            <ul className="flex">
              {group.chapters.map((chapter) => {
                const active = chapter.id === current;
                return (
                  <li key={chapter.id}>
                    <a
                      href={`#${chapter.id}`}
                      aria-current={active ? "true" : undefined}
                      className={cn(
                        "flex min-h-12 items-center gap-2 border-b-2 px-3 text-sm whitespace-nowrap focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-(--home-ink)",
                        active
                          ? "border-(--home-ink) font-semibold text-(--home-ink)"
                          : "border-transparent text-(--home-muted) hover:text-(--home-ink)",
                      )}
                    >
                      <span
                        aria-hidden="true"
                        className={cn(
                          "flex size-5 items-center justify-center rounded-full text-xs font-semibold tabular-nums",
                          active
                            ? "bg-(--home-ink) text-(--home-canvas)"
                            : "border border-(--home-muted)",
                        )}
                      >
                        {chapter.number}
                      </span>
                      {chapter.label}
                    </a>
                  </li>
                );
              })}
            </ul>
          </div>
        ))}
      </div>
    </nav>
  );
}
