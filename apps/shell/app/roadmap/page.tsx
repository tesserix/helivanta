import type { Metadata } from "next";
import { TesserixFooter } from "@helivanta/ui";
import { AiBoard } from "@/components/landing/ai-board";
import { MaintenanceWatch } from "@/components/landing/maintenance-watch";
import { PlatformGrid } from "@/components/landing/platform-grid";
import { PublicLinks, PublicNav } from "@/components/landing/public-chrome";
import { subHeading } from "@/components/landing/public-type";
import { StoryAct, StoryChapter } from "@/components/landing/story-chapter";
import { TrustSection } from "@/components/landing/trust-section";
import { anek } from "../anek";
import { homeWidth } from "../page-width";

export const metadata: Metadata = {
  title: "Helivanta | Roadmap",
  description:
    "What Helivanta plans next: maintenance and compliance checks, AI that drafts for people to approve, every platform module with its status, and the trust centre.",
};

// Everything that is not available yet, with its status, one click from the
// home page (#1837 D1, D3, D4). Nothing here is a claim that it exists.
export default function RoadmapPage() {
  return (
    <div
      className={`${anek.variable} home-slate min-h-screen bg-background text-foreground selection:bg-(--hms-accent)/30`}
    >
      <a
        href="#main"
        className="sr-only focus:not-sr-only focus:absolute focus:z-50 focus:bg-foreground focus:p-4 focus:text-background"
      >
        Skip to content
      </a>
      <PublicNav home={false} />
      <main id="main">
        <section className={`${homeWidth} pt-16 pb-4 lg:pt-20`}>
          <h1 className="text-5xl leading-[0.95] font-semibold tracking-tight font-stretch-[75%] sm:text-7xl">
            Roadmap
          </h1>
        </section>
        <StoryAct
          upcoming
          title="What comes next"
          summary="Not available yet. Shown so you can see where Helivanta is going."
        />
        <StoryChapter
          id="next"
          number={5}
          tone="plain"
          upcoming
          title="Checks that run themselves, and AI that drafts"
          lede="Maintenance and compliance checks, and AI agents that draft routine work, are planned. In both, a named person makes the decision."
        >
          <section id="maintenance" aria-labelledby="maintenance-heading" className="scroll-mt-28">
            <h3 id="maintenance-heading" className={subHeading}>
              Maintenance and compliance check themselves
            </h3>
            <p className="mt-3 max-w-2xl leading-relaxed text-muted-foreground">
              Electrical, structural, equipment, fire, lift and medical gas checks run against one
              register. Overdue work and expiring certificates get drafted for approval, and a
              failed safety check goes straight to an engineer.
            </p>
            <div className="mt-8">
              <MaintenanceWatch />
            </div>
          </section>
          <section
            id="ai"
            aria-labelledby="ai-heading"
            className="mt-16 scroll-mt-28 border-t border-dashed border-border pt-16"
          >
            <h3 id="ai-heading" className={subHeading}>
              AI drafts. People decide.
            </h3>
            <p className="mt-3 max-w-2xl leading-relaxed text-muted-foreground">
              A supervisor agent hands each task to one specialist agent. The agent drafts the work,
              and a named person reviews, edits and approves it before anything reaches the record.
            </p>
            <div className="mt-8">
              <AiBoard />
            </div>
          </section>
        </StoryChapter>

        <PlatformGrid scope="all" />
        <TrustSection />
      </main>
      <PublicLinks />
      <TesserixFooter />
    </div>
  );
}
