import type { ReactNode } from "react";
import { Info } from "lucide-react";
import { Popover, PopoverContent, PopoverTrigger } from "@helivanta/ui";
import type { ModuleStatus } from "@/app/platform-catalog";

// Help text opens on click, tap or keyboard, never on hover alone: hover does not
// exist on touch screens, and a popover keeps the definition until it is dismissed
// (WCAG 1.4.13). The trigger's text is only the term, so the sentence reads unchanged.
export function Term({ children, definition }: { children: ReactNode; definition: ReactNode }) {
  return (
    <Popover>
      <PopoverTrigger className="cursor-help rounded-sm text-inherit underline decoration-muted-foreground decoration-dotted underline-offset-4 hover:decoration-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-foreground">
        {children}
      </PopoverTrigger>
      <PopoverContent className="w-72 text-sm leading-relaxed">{definition}</PopoverContent>
    </Popover>
  );
}

// Wording from docs/product/public-platform-catalog.md, which owns these labels.
export const statusMeaning: Record<ModuleStatus, string> = {
  Available: "Working today for the part described.",
  "Foundation available":
    "The core workflow works today. The extensions listed with it are planned.",
  Planned: "Not a working module yet. It is on the roadmap.",
  Future: "Not a working module yet. A future platform capability.",
};

export function StatusGuide() {
  return (
    <Popover>
      <PopoverTrigger
        aria-label="What the availability labels mean"
        className="-my-3 inline-flex size-11 items-center justify-center rounded-full text-muted-foreground hover:text-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-foreground"
      >
        <Info aria-hidden="true" className="size-4" />
      </PopoverTrigger>
      <PopoverContent className="w-80">
        <p className="text-sm font-semibold">What the labels mean</p>
        <dl className="mt-3 space-y-3 text-sm leading-relaxed">
          {(Object.keys(statusMeaning) as ModuleStatus[]).map((status) => (
            <div key={status}>
              <dt className="font-medium">{status}</dt>
              <dd className="text-muted-foreground">{statusMeaning[status]}</dd>
            </div>
          ))}
        </dl>
      </PopoverContent>
    </Popover>
  );
}
