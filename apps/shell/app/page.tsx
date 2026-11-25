import type { Metadata } from "next";
import Link from "next/link";
import { Button, TesserixFooter } from "@helivanta/ui";
import { AuditTrail } from "@/components/landing/audit-trail";
import { CareBoard } from "@/components/landing/care-board";
import { ChapterBar, type ChapterGroup } from "@/components/landing/chapter-bar";
import { PatientJourney } from "@/components/landing/patient-journey";
import { StockWatch } from "@/components/landing/stock-watch";
import { StoryAct, StoryChapter } from "@/components/landing/story-chapter";
import { Term } from "@/components/landing/term";
import { PlatformGrid } from "@/components/landing/platform-grid";
import { PublicLinks, PublicNav } from "@/components/landing/public-chrome";
import { RoadmapLinks } from "@/components/landing/roadmap-links";
import { sectionHeading } from "@/components/landing/public-type";
import { anek } from "./anek";
import { homeWidth } from "./page-width";

export const metadata: Metadata = {
  title: "Helivanta | One connected hospital, from boardroom to bedside",
  description:
    "See how hospital management, administration and departments such as pharmacy, laboratory and stores hand work to each other through Helivanta.",
};

const reasons = [
  {
    title: "Every record has one owner",
    body: "The doctor writes the prescription once. The pharmacy dispenses from it and the laboratory reports against the order, so nobody copies a patient's details from one register to the next.",
    today:
      "Today: Pharmacy setup, low-stock warnings and stocktakes are in this release. Patient records, visits and the laboratory are on the roadmap.",
  },
  {
    title: "Access follows the job",
    body: "Designed so a nurse posted to the ICU sees ICU patients, and loses that access when the posting ends. Managers get the numbers they answer for, not every patient file.",
    today:
      "Today: Staff access is scoped to each hospital. Access that follows a posting is planned.",
  },
  {
    title: "Your hospital's rules, on our foundations",
    body: "Spending limits, who signs and in what order stay your hospital's own, set on top of Helivanta's standard workflows instead of built from scratch.",
    today:
      "Today: Hospitals work with Helivanta's standard workflows. Configurable approval chains are planned.",
  },
  {
    title: "One product for every kind of hospital",
    body: "A state health department and a private hospital group use the same product, set up for how each is governed, funded and staffed.",
    today:
      "Today: Organisations, hospitals and branding are available as foundations. Sector templates are planned.",
  },
];

const sectors = [
  {
    id: "sector-government",
    name: "Government",
    summary: (
      <>
        Facilities under the{" "}
        <Term definition="The national programme that funds public health services through state and district health societies.">
          National Health Mission
        </Term>{" "}
        spend against sanctioned budgets and report up through the district.
      </>
    ),
    levels: [
      { name: "State health department", detail: "Directorate and State Health Society" },
      {
        name: "District health society",
        detail: (
          <>
            Usually chaired by the District Collector, with the{" "}
            <Term definition="The district's senior health officer. The title varies by state.">
              CMO, CMHO
            </Term>{" "}
            or Civil Surgeon
          </>
        ),
      },
      {
        name: "District hospital",
        detail: (
          <>
            Medical Superintendent, with the hospital&apos;s{" "}
            <Term definition="A governance committee attached to a public hospital, not a department. Its governing body oversees the hospital's untied funds.">
              Rogi Kalyan Samiti
            </Term>
          </>
        ),
      },
      {
        name: (
          <>
            Block,{" "}
            <Term definition="Community Health Centre and Primary Health Centre: public facilities run at block level, below the district hospital.">
              CHC, PHC
            </Term>{" "}
            and sub-health centre
          </>
        ),
        detail: "Care closest to home",
      },
    ],
  },
  {
    id: "sector-private",
    name: "Private",
    summary:
      "Single hospitals, groups and trusts approve spending through their own matrix of amounts and roles.",
    levels: [
      { name: "Board or trustees", detail: "Owners, promoters or a trust" },
      { name: "Group or regional office", detail: "For groups running several hospitals" },
      { name: "Hospital", detail: "CEO or Medical Director, with department heads" },
    ],
  },
];

// The story's chapters, grouped by whether they are available today (#1413 D4, D5).
const chapterGroups: readonly ChapterGroup[] = [
  {
    label: "Available today",
    chapters: [
      { id: "how", number: 1, label: "Work moves" },
      { id: "patients", number: 2, label: "One patient" },
      { id: "audit", number: 3, label: "One trail" },
      { id: "stock", number: 4, label: "Stock checks" },
    ],
  },
];

export default function LandingPage() {
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
      <PublicNav home />
      <main id="main">
        <section
          className={`${homeWidth} grid gap-10 pt-16 pb-12 lg:grid-cols-[1.4fr_1fr] lg:items-end lg:pt-20`}
        >
          <h1 className="text-5xl leading-[0.95] font-semibold tracking-tight font-stretch-[75%] sm:text-7xl lg:text-8xl">
            From boardroom to bedside, one connected hospital.
          </h1>
          <div>
            <p className="text-lg leading-relaxed text-muted-foreground">
              Helivanta is a hospital management system where departments, administration and
              management work from the same records. Watch a request leave a ward and reach the
              pharmacy, the store and the people who sign it off.
            </p>
            <div className="mt-8 flex flex-wrap items-center gap-6">
              <Button asChild size="lg">
                <Link href="/workspace">Start your workspace</Link>
              </Button>
              <a href="#how" className="text-sm font-semibold underline underline-offset-8">
                See how it works
              </a>
            </div>
          </div>
        </section>

        <ChapterBar groups={chapterGroups} />

        {/* Chapters 1 to 4: what exists; anything planned links to the roadmap (#1837). */}
        <RoadmapLinks>
          <StoryAct
            title="Available today"
            summary="Foundations you can use now. Each chapter marks what is still planned."
          />
          <StoryChapter
            id="how"
            number={1}
            tone="plain"
            title="How work moves through Helivanta"
            lede="Pick a story. Each request lights up the team that sends it, passes through Helivanta, and reaches the team that acts on it."
          >
            <CareBoard />
          </StoryChapter>
          <StoryChapter
            id="patients"
            number={2}
            tone="band"
            title="One patient, one record, every department"
            lede="Reception, the doctor, the laboratory, the pharmacy and billing each add their own entry to the same record, from arrival to going home."
          >
            <PatientJourney />
          </StoryChapter>
          <StoryChapter
            id="audit"
            number={3}
            tone="plain"
            title="Every tablet and every payment, on one trail"
            lede="Stock and money land on one trail that only grows. A correction adds an entry instead of overwriting one, so an auditor sees what changed, who changed it and why."
          >
            <AuditTrail />
          </StoryChapter>
          <StoryChapter
            id="stock"
            number={4}
            tone="band"
            title="Pharmacy stock checks itself"
            lede={
              <>
                A check flags what is low, out, short-dated or held in{" "}
                <Term definition="A batch held back until a pharmacist checks it and releases it for dispensing.">
                  quarantine
                </Term>
                . Agents draft the order or the transfer, and nothing moves until a named person
                approves it.
              </>
            }
          >
            <StockWatch />
          </StoryChapter>
        </RoadmapLinks>

        <section
          id="why"
          aria-labelledby="why-heading"
          className="scroll-mt-6 border-t border-border bg-muted py-24"
        >
          <div className={homeWidth}>
            <h2 id="why-heading" className={sectionHeading}>
              Why hospitals try Helivanta
            </h2>
            <div className="mt-12 grid gap-x-16 gap-y-12 md:grid-cols-2">
              {reasons.map((reason) => (
                <article key={reason.title} className="border-t border-border pt-6">
                  <h3 className="text-2xl font-semibold font-stretch-[85%]">{reason.title}</h3>
                  <p className="mt-3 max-w-prose leading-relaxed text-muted-foreground">
                    {reason.body}
                  </p>
                  <p className="mt-5 text-base leading-relaxed text-muted-foreground">
                    {reason.today}
                  </p>
                </article>
              ))}
            </div>
          </div>
        </section>

        <section
          id="sectors"
          aria-labelledby="sectors-heading"
          className={`${homeWidth} grid gap-x-16 py-24 xl:grid-cols-[minmax(0,1fr)_minmax(0,2fr)] xl:grid-rows-[auto_1fr]`}
        >
          <div>
            <h2 id="sectors-heading" className={`${sectionHeading} max-w-3xl`}>
              Built for government and private hospitals
            </h2>
            <p className="mt-5 max-w-2xl leading-relaxed text-muted-foreground">
              Who governs and how money is approved differ between sectors. How access is checked
              and who owns each record do not.
            </p>
          </div>
          <div className="mt-12 grid gap-12 md:grid-cols-2 xl:col-start-2 xl:row-span-2 xl:row-start-1 xl:mt-0">
            {sectors.map((sector) => (
              <article key={sector.id} aria-labelledby={sector.id}>
                <h3 id={sector.id} className="text-2xl font-semibold font-stretch-[85%]">
                  {sector.name}
                </h3>
                <p className="mt-2 max-w-md text-base leading-relaxed text-muted-foreground">
                  {sector.summary}
                </p>
                <ol className="mt-8 border-l border-border">
                  {sector.levels.map((level, rung) => (
                    // A ladder is fixed and ordered, so its rung is a stable key.
                    <li key={rung} className="relative pb-7 pl-6 last:pb-0">
                      <span
                        aria-hidden="true"
                        className="absolute top-1.5 -left-1.25 size-2.5 rounded-full border-2 border-(--hms-violet) bg-background"
                      />
                      <p className="font-semibold">{level.name}</p>
                      <p className="mt-1 text-base text-muted-foreground">{level.detail}</p>
                    </li>
                  ))}
                </ol>
              </article>
            ))}
          </div>
          <p className="mt-12 max-w-2xl text-base leading-relaxed text-muted-foreground xl:col-start-1 xl:row-start-2 xl:self-end">
            Government and private sector setup is planned. Today, every organisation starts from
            the same model of organisations, hospitals and teams.
          </p>
        </section>

        <PlatformGrid scope="available" />

        {/* The close (#1837 D5): the canvas, the heading, one control. The
            second route to a conversation arrives with the contact flow (#1355). */}
        <section aria-labelledby="close-heading" className={`${homeWidth} py-24`}>
          <h2 id="close-heading" className={`${sectionHeading} max-w-3xl`}>
            Bring your hospital onto one connected record.
          </h2>
          <p className="mt-4 text-muted-foreground">
            Create your organisation&apos;s workspace to get started.
          </p>
          <Button asChild size="lg" className="mt-8">
            <Link href="/workspace">Start your workspace</Link>
          </Button>
        </section>
      </main>
      <PublicLinks />
      <TesserixFooter />
    </div>
  );
}
