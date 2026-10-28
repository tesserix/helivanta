import type { CSSProperties, ReactNode } from "react";
import { Check } from "lucide-react";
import { cn } from "@tesserix/web";

type BoardCardProps = {
  style: CSSProperties;
  className?: string;
  ring?: string;
  dashed?: boolean;
  surface?: string;
  children: ReactNode;
};

// An outer bezel holds the card, and lights up when the story reaches it.
export function BoardCard({ style, className, ring, dashed, surface, children }: BoardCardProps) {
  return (
    <div
      style={{
        ...style,
        borderColor: ring,
        boxShadow: ring ? `0 0 26px -10px ${ring}` : undefined,
      }}
      className={cn(
        "rounded-[14px] border border-(--home-line)/60 p-[3px] transition-[opacity,border-color,box-shadow] duration-500",
        className,
      )}
    >
      <div
        className={cn(
          "flex h-full flex-col overflow-hidden rounded-[11px] border border-(--home-line)",
          dashed && "border-dashed",
          surface ?? "bg-(--home-surface)",
        )}
      >
        {children}
      </div>
    </div>
  );
}

type CardHeaderProps = { title: string; icon?: ReactNode; count?: number };

export function CardHeader({ title, icon, count }: CardHeaderProps) {
  return (
    <div className="flex items-start gap-2 px-3.5 py-2.5">
      {icon && <span className="flex h-[1.125rem] items-center">{icon}</span>}
      <p className="min-w-0 text-[13px] leading-[1.125rem] font-semibold text-(--home-ink)">
        {title}
      </p>
      {count !== undefined && (
        <span className="ml-auto rounded-md bg-(--home-line) px-1.5 text-[11px] leading-[1.125rem] font-medium text-(--home-ink) tabular-nums">
          {count}
        </span>
      )}
    </div>
  );
}

export function CardBody({ children }: { children: ReactNode }) {
  return (
    <div className="flex-1 border-t border-(--home-line) px-3.5 py-2.5 text-[11px] leading-snug text-(--home-muted)">
      {children}
    </div>
  );
}

type StatusPillProps = {
  tone: "waiting" | "reviewing" | "done";
  label: string;
  reserve: readonly string[];
};

const PILL =
  "col-start-1 row-start-1 flex max-w-full items-center gap-1.5 rounded-full border px-2 py-0.5 text-[11px] leading-tight";

// Unread copies of every label share the pill's cell, so a label that wraps never grows the board mid-story.
export function StatusPill({ tone, label, reserve }: StatusPillProps) {
  return (
    <span className="grid items-start justify-items-start">
      {reserve.map((text) => (
        <span
          key={text}
          aria-hidden="true"
          data-label={text}
          className={cn(PILL, "invisible after:content-[attr(data-label)]")}
        >
          <Check className="size-3 shrink-0" />
        </span>
      ))}
      <span
        className={cn(
          PILL,
          tone === "done" && "border-(--home-care)/60 text-(--home-care)",
          tone === "reviewing" && "border-(--home-oversight)/60 text-(--home-ink)",
          tone === "waiting" && "border-(--home-line) text-(--home-muted)",
        )}
      >
        {tone === "done" && <Check className="size-3 shrink-0" />}
        {tone === "reviewing" && (
          <span className="size-1.5 shrink-0 animate-pulse rounded-full bg-(--home-oversight)" />
        )}
        {label}
      </span>
    </span>
  );
}

export function Arrowhead({ colour }: { colour: string }) {
  return (
    <span
      className="absolute top-1/2 -right-1 -translate-y-1/2 border-y-4 border-l-[6px] border-y-transparent transition-colors duration-300"
      style={{ borderLeftColor: colour }}
    />
  );
}

// The line is held empty until approval so the board keeps its height as the story plays.
export function AiAssistedMark({ shown }: { shown: boolean }) {
  return (
    <span className="mt-1.5 block min-h-[1lh] text-[11px] text-(--home-muted)">
      {shown && "Marked AI-assisted"}
    </span>
  );
}
