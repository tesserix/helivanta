"use client";

import { Button } from "@tesserix/web";
import { ChevronLeft, ChevronRight } from "lucide-react";
import type { ReactNode, Ref } from "react";

type Step = { disabled: boolean; onClick: () => void };

/**
 * The open record's header (#1303 D2/D6): its name, Previous / Next through
 * the list's rows, and Close. The heading takes focus when a record is
 * viewed, so it is focusable without being a tab stop.
 */
export function DetailHeader({
  title,
  subtitle,
  headingRef,
  noun,
  previous,
  next,
  stepping = true,
  closeLabel,
  onClose,
}: {
  title: ReactNode;
  subtitle?: ReactNode;
  headingRef?: Ref<HTMLHeadingElement>;
  /** "bill" names the buttons "Previous bill" and "Next bill". */
  noun: string;
  previous: Step;
  next: Step;
  /** False where the record has no list to step through (Receiving). */
  stepping?: boolean;
  closeLabel: string;
  onClose: () => void;
}) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-4">
      <div className="min-w-0">
        <h3
          ref={headingRef}
          tabIndex={-1}
          className="scroll-mt-24 break-words text-lg font-semibold outline-none"
        >
          {title}
        </h3>
        {subtitle && <p className="mt-1 text-sm text-muted-foreground">{subtitle}</p>}
      </div>
      <div className="flex items-center gap-2">
        {stepping && (
          <>
            <Button
              variant="outline"
              size="sm"
              aria-label={`Previous ${noun}`}
              disabled={previous.disabled}
              onClick={previous.onClick}
            >
              <ChevronLeft className="size-4" />
            </Button>
            <Button
              variant="outline"
              size="sm"
              aria-label={`Next ${noun}`}
              disabled={next.disabled}
              onClick={next.onClick}
            >
              <ChevronRight className="size-4" />
            </Button>
          </>
        )}
        <Button variant="outline" onClick={onClose}>
          {closeLabel}
        </Button>
      </div>
    </div>
  );
}
