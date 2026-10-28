import type { Metadata } from "next";
import Link from "next/link";
import localFont from "next/font/local";
import { HeartPulse } from "lucide-react";
import { TesserixFooter } from "@helivanta/ui";
import { AiBoard } from "@/components/landing/ai-board";
import { AuditTrail } from "@/components/landing/audit-trail";
import { CareBoard } from "@/components/landing/care-board";
import { ChapterBar, type ChapterGroup } from "@/components/landing/chapter-bar";
import { MaintenanceWatch } from "@/components/landing/maintenance-watch";
import { ModuleStatusLabel } from "@/components/landing/module-status";
import { PatientJourney } from "@/components/landing/patient-journey";
import { StockWatch } from "@/components/landing/stock-watch";
import { StoryAct, StoryChapter } from "@/components/landing/story-chapter";
import { homeWidth } from "./page-width";
import { platformGroups } from "./platform-catalog";

const anek = localFont({
  src: "./fonts/anek-latin.woff2",
  weight: "100 800",
  display: "swap",
  declarations: [{ prop: "font-stretch", value: "75% 125%" }],
});

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
      "Today: Patient management, visits, pharmacy and laboratory are available as foundations.",
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
    summary:
      "Facilities under the National Health Mission spend against sanctioned budgets and report up through the district.",
    levels: [
      { name: "State health department", detail: "Directorate and State Health Society" },
      {
        name: "District health society",
        detail: "Usually chaired by the District Collector, with the CMO, CMHO or Civil Surgeon",
      },
      {
        name: "District hospital",
        detail: "Medical Superintendent, with the hospital's Rogi Kalyan Samiti",
      },
      { name: "Block, CHC, PHC and sub-health centre", detail: "Care closest to home" },
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

const trustItems = [
  { label: "SOC 2 Type II", kind: "Certification placeholder", state: "Not yet certified" },
  { label: "ISO/IEC 27001", kind: "Certification placeholder", state: "Not yet certified" },
  { label: "HIPAA safeguards", kind: "Compliance documentation", state: "Documentation pending" },
];

const primaryAction =
  "inline-flex min-h-11 items-center rounded-full bg-(--home-care) px-6 text-sm font-semibold text-(--home-canvas) hover:bg-(--home-care-hover) focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-(--home-ink)";

const sectionHeading =
  "text-4xl leading-[1.02] font-semibold tracking-tight font-stretch-[80%] sm:text-5xl";

const subHeading = "text-2xl leading-tight font-semibold tracking-tight font-stretch-[85%]";

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
  {
    label: "What comes next",
    chapters: [{ id: "next", number: 5, label: "Maintenance and AI" }],
  },
];

export default function LandingPage() {
  return (
    <div
      className={`${anek.className} home-slate min-h-screen bg-(--home-canvas) text-(--home-ink) selection:bg-(--home-care)/30`}
    >
      <a
        href="#main"
        className="sr-only focus:not-sr-only focus:absolute focus:z-50 focus:bg-(--home-ink) focus:p-4 focus:text-(--home-canvas)"
      >
        Skip to content
      </a>
      <header className="border-b border-(--home-line)">
        <nav
          aria-label="Main navigation"
          className={`${homeWidth} flex flex-wrap items-center justify-between gap-5 py-5`}
        >
          <Link
            href="/"
            aria-label="Helivanta home"
            className="flex items-center gap-3 text-xl font-semibold tracking-tight"
          >
            <span
              aria-hidden="true"
              className="flex size-9 items-center justify-center rounded-xl bg-(--home-care) text-(--home-canvas)"
            >
              <HeartPulse size={20} />
            </span>
            <span>
              helivanta<span className="text-(--home-care)">.</span>
            </span>
          </Link>
          <div className="hidden items-center gap-7 text-sm text-(--home-muted) lg:flex">
            <a href="#how" className="hover:text-(--home-ink)">
              How it works
            </a>
            <a href="#why" className="hover:text-(--home-ink)">
              Why Helivanta
            </a>
            <a href="#platform" className="hover:text-(--home-ink)">
              Platform
            </a>
            <a href="#trust" className="hover:text-(--home-ink)">
              Trust
            </a>
          </div>
          <div className="flex items-center gap-4 text-sm font-medium whitespace-nowrap sm:gap-5">
            <Link
              href="/ambulance"
              className="inline-flex min-h-11 items-center text-(--home-stock) underline underline-offset-4"
            >
              Emergency help
            </Link>
            <a href="/login" className="hover:underline">
              Sign in
            </a>
            {/* The hero's identical action sits directly below on small screens. */}
            <Link href="/workspace" className={`${primaryAction} max-sm:hidden`}>
              Start your workspace
            </Link>
          </div>
        </nav>
      </header>
      <main id="main">
        <section
          className={`${homeWidth} grid gap-10 pt-16 pb-12 lg:grid-cols-[1.4fr_1fr] lg:items-end lg:pt-20`}
        >
          <h1 className="text-5xl leading-[0.95] font-semibold tracking-tight font-stretch-[75%] sm:text-7xl lg:text-8xl">
            From boardroom to bedside, one connected hospital.
          </h1>
          <div>
            <p className="text-lg leading-relaxed text-(--home-muted)">
              Helivanta is a hospital management system where departments, administration and
              management work from the same records. Watch a request leave a ward and reach the
              pharmacy, the store and the people who sign it off.
            </p>
            <div className="mt-8 flex flex-wrap items-center gap-6">
              <Link href="/workspace" className={primaryAction}>
                Start your workspace
              </Link>
              <a href="#how" className="text-sm font-semibold underline underline-offset-8">
                See how it works
              </a>
            </div>
          </div>
        </section>

        <ChapterBar groups={chapterGroups} />

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
          lede="A check flags what is low, out, short-dated or held in quarantine. Agents draft the order or the transfer, and nothing moves until a named person approves it."
        >
          <StockWatch />
        </StoryChapter>

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
            <p className="mt-3 max-w-2xl leading-relaxed text-(--home-muted)">
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
            className="mt-16 scroll-mt-28 border-t border-dashed border-(--home-line) pt-16"
          >
            <h3 id="ai-heading" className={subHeading}>
              AI drafts. People decide.
            </h3>
            <p className="mt-3 max-w-2xl leading-relaxed text-(--home-muted)">
              A supervisor agent hands each task to one specialist agent. The agent drafts the work,
              and a named person reviews, edits and approves it before anything reaches the record.
            </p>
            <div className="mt-8">
              <AiBoard />
            </div>
          </section>
        </StoryChapter>

        <section
          id="why"
          aria-labelledby="why-heading"
          className="scroll-mt-6 border-t border-(--home-line) bg-(--home-surface) py-24"
        >
          <div className={homeWidth}>
            <h2 id="why-heading" className={sectionHeading}>
              Why hospitals try Helivanta
            </h2>
            <div className="mt-12 grid gap-x-16 gap-y-12 md:grid-cols-2">
              {reasons.map((reason) => (
                <article key={reason.title} className="border-t border-(--home-line) pt-6">
                  <h3 className="text-2xl font-semibold font-stretch-[85%]">{reason.title}</h3>
                  <p className="mt-3 max-w-prose leading-relaxed text-(--home-muted)">
                    {reason.body}
                  </p>
                  <p className="mt-5 border-l-2 border-(--home-care) pl-3 text-sm leading-relaxed">
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
            <p className="mt-5 max-w-2xl leading-relaxed text-(--home-muted)">
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
                <p className="mt-2 max-w-md text-sm leading-relaxed text-(--home-muted)">
                  {sector.summary}
                </p>
                <ol className="mt-8 border-l border-(--home-line)">
                  {sector.levels.map((level) => (
                    <li key={level.name} className="relative pb-7 pl-6 last:pb-0">
                      <span
                        aria-hidden="true"
                        className="absolute top-1.5 -left-1.25 size-2.5 rounded-full border-2 border-(--home-oversight) bg-(--home-canvas)"
                      />
                      <p className="font-semibold">{level.name}</p>
                      <p className="mt-1 text-sm text-(--home-muted)">{level.detail}</p>
                    </li>
                  ))}
                </ol>
              </article>
            ))}
          </div>
          <p className="mt-12 max-w-2xl border-l-2 border-(--home-oversight) pl-3 text-sm leading-relaxed xl:col-start-1 xl:row-start-2 xl:self-end">
            Government and private sector setup is planned. Today, every organisation starts from
            the same model of organisations, hospitals and teams.
          </p>
        </section>

        <section
          id="platform"
          aria-labelledby="platform-heading"
          className="scroll-mt-6 border-t border-(--home-line) bg-(--home-surface) py-24"
        >
          <div className={homeWidth}>
            <h2 id="platform-heading" className={sectionHeading}>
              Different responsibilities.
              <br />A familiar place to work.
            </h2>
            <p className="mt-5 max-w-3xl leading-relaxed text-(--home-muted)">
              Explore the full platform and its roadmap. Availability varies by module: foundations
              cover the workflows described below, while planned and future capabilities are not yet
              available. Access depends on your organisation and permissions.
            </p>
            <div className="mt-12">
              {platformGroups.map((group) => (
                <div
                  key={group.name}
                  className="grid gap-8 border-t border-(--home-line) py-10 lg:grid-cols-[17rem_1fr]"
                >
                  <div>
                    <h3 className="text-xl font-semibold font-stretch-[85%]">{group.name}</h3>
                    <p className="mt-2 text-sm leading-relaxed text-(--home-muted)">
                      {group.description}
                    </p>
                  </div>
                  <div className="grid gap-x-10 gap-y-8 sm:grid-cols-2">
                    {group.modules.map((module) => (
                      <article key={module.name} aria-label={module.name}>
                        <div className="flex items-start justify-between gap-4">
                          <h4 className="font-semibold">{module.name}</h4>
                          <ModuleStatusLabel status={module.status} />
                        </div>
                        <p className="mt-1 text-xs text-(--home-muted)">{module.product}</p>
                        <p className="mt-3 text-sm leading-relaxed text-(--home-muted)">
                          {module.description}
                        </p>
                      </article>
                    ))}
                  </div>
                </div>
              ))}
            </div>
          </div>
        </section>

        <section
          id="trust"
          aria-labelledby="trust-heading"
          className={`${homeWidth} scroll-mt-6 py-24`}
        >
          <h2 id="trust-heading" className={`${sectionHeading} max-w-2xl`}>
            Care deserves a clear standard.
          </h2>
          <p className="mt-5 max-w-2xl leading-relaxed text-(--home-muted)">
            Our trust centre will make independent assurance and supporting documentation easy to
            find. The items below are placeholders, not claims of certification or compliance.
          </p>
          <div className="mt-10 grid gap-5 md:grid-cols-3">
            {trustItems.map((item) => (
              <article
                key={item.label}
                className="rounded-2xl border border-dashed border-(--home-line) p-7"
              >
                <p className="text-xs text-(--home-muted)">{item.kind}</p>
                <h3 className="mt-4 text-xl font-semibold">{item.label}</h3>
                <p className="mt-5 inline-block rounded-full border border-(--home-line) px-3 py-1 text-xs text-(--home-muted)">
                  {item.state}
                </p>
              </article>
            ))}
          </div>
        </section>

        <section id="reviews" aria-labelledby="reviews-heading" className={`${homeWidth} pb-24`}>
          <div className="grid gap-10 border-t border-(--home-line) pt-24 md:grid-cols-2">
            <div>
              <h2 id="reviews-heading" className={sectionHeading}>
                Real experiences, shared with permission.
              </h2>
              <p className="mt-5 max-w-md text-sm leading-relaxed text-(--home-muted)">
                No customer endorsements or ratings are published yet. Verified testimonials and
                reviews will appear here after customer approval.
              </p>
            </div>
            <div className="grid gap-4">
              {["Customer testimonials", "Verified reviews"].map((label) => (
                <div
                  key={label}
                  className="rounded-2xl border border-dashed border-(--home-line) p-6"
                >
                  <h3 className="font-semibold">{label}</h3>
                  <p className="mt-2 text-sm text-(--home-muted)">Awaiting approved content</p>
                </div>
              ))}
            </div>
          </div>
        </section>

        <section className={`${homeWidth} mb-20`}>
          <div className="rounded-3xl border border-(--home-line) bg-(--home-surface) px-6 py-16 text-center">
            <h2 className={sectionHeading}>Bring your hospital onto one connected record.</h2>
            <p className="mt-4 text-(--home-muted)">
              Create your organisation&apos;s workspace to get started.
            </p>
            <Link href="/workspace" className={`${primaryAction} mt-8`}>
              Start your workspace
            </Link>
          </div>
        </section>
      </main>
      <TesserixFooter />
    </div>
  );
}
