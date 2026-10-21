import type { Metadata } from "next";
import { HeartPulse } from "lucide-react";
import { TesserixFooter } from "@helivanta/ui";
import { platformGroups } from "./platform-catalog";

export const metadata: Metadata = {
  title: "Helivanta | Connected care, thoughtfully designed",
  description:
    "A shared workspace for hospital teams, pharmacies and laboratories. Explore Helivanta and bring your care network together.",
};

export default function LandingPage() {
  return (
    <div className="public-site min-h-screen bg-background text-foreground selection:bg-teal-200">
      <a
        href="#main"
        className="sr-only focus:not-sr-only focus:absolute focus:z-50 focus:bg-white focus:p-4"
      >
        Skip to content
      </a>
      <header className="border-b border-border">
        <nav
          aria-label="Main navigation"
          className="mx-auto flex max-w-7xl flex-wrap items-center justify-between gap-5 px-6 py-6 lg:px-10"
        >
          <a
            href="/"
            aria-label="Helivanta home"
            className="flex items-center gap-3 text-xl font-semibold tracking-tight"
          >
            <span
              aria-hidden="true"
              className="flex h-9 w-9 items-center justify-center rounded-xl bg-primary text-2xl text-white"
            >
              <HeartPulse size={20} aria-hidden="true" />
            </span>
            helivanta<span className="text-teal-600">.</span>
          </a>
          <div className="hidden items-center gap-7 text-sm md:flex">
            <a href="#platform" className="hover:underline">
              Platform
            </a>
            <a href="#care-network" className="hover:underline">
              Care network
            </a>
            <a href="#trust" className="hover:underline">
              Trust
            </a>
          </div>
          <div className="flex items-center gap-5 text-sm font-medium">
            <a
              href="/ambulance"
              className="inline-flex min-h-11 items-center text-teal-800 underline underline-offset-4"
            >
              Emergency help
            </a>
            <a href="/login" className="hover:underline">
              Sign in
            </a>
            <a
              href="/signup"
              className="rounded-full bg-primary px-5 py-3 text-white hover:bg-(--hms-accent-strong)"
            >
              Start your workspace <span aria-hidden="true">↗</span>
            </a>
          </div>
        </nav>
      </header>
      <main id="main">
        <section className="mx-auto grid max-w-7xl items-center gap-14 px-6 py-20 lg:grid-cols-[1.15fr_1fr] lg:px-10 lg:py-28">
          <div>
            <p className="mb-7 flex items-center gap-3 text-xs font-semibold uppercase tracking-[0.2em]">
              <span className="h-2 w-2 rounded-full bg-teal-600" />
              Built around your care team
            </p>
            <h1 className="max-w-2xl text-5xl leading-[1.08] font-medium tracking-[-0.055em] sm:text-6xl lg:text-7xl">
              Better connected teams.
              <br />
              <span className="font-serif italic text-(--hms-accent)">More room for care.</span>
            </h1>
            <p className="mt-7 max-w-lg text-lg leading-relaxed text-muted-foreground">
              One thoughtful workspace for your hospitals, pharmacies and laboratories. Bring people
              and everyday workflows together, with your organisation at the centre.
            </p>
            <div className="mt-9 flex flex-wrap items-center gap-6">
              <a
                href="/signup"
                className="rounded-full bg-primary px-7 py-4 text-sm font-semibold text-white hover:bg-(--hms-accent-strong)"
              >
                Start your workspace <span aria-hidden="true">↗</span>
              </a>
              <a
                href="#platform"
                className="text-sm font-semibold underline decoration-border underline-offset-8"
              >
                Explore the platform
              </a>
            </div>
            <p className="mt-6 text-xs text-muted-foreground">
              Your organisation. Your identity. One connected experience.
            </p>
          </div>
          <div
            className="relative rounded-[2rem] border border-border bg-accent p-6 sm:p-9"
            aria-label="Illustration of the Helivanta workspace"
          >
            <div className="mb-8 flex items-center justify-between text-xs">
              <span className="font-semibold uppercase tracking-widest">Your care ecosystem</span>
              <span className="rounded-full border border-border px-3 py-1">
                Product preview
              </span>
            </div>
            <div className="rounded-2xl border border-white bg-white/90 p-6 shadow-[0_16px_40px_-24px_var(--hms-shadow)]">
              <div className="flex items-center gap-3 border-b border-border pb-5">
                <span
                  className="flex h-11 w-11 items-center justify-center rounded-xl bg-primary text-2xl text-white"
                  aria-hidden="true"
                >
                  +
                </span>
                <div>
                  <p className="font-semibold">A workspace of your own</p>
                  <p className="mt-1 text-xs text-muted-foreground">
                    Your brand, across your care network
                  </p>
                </div>
              </div>
              <div className="mt-6 grid gap-3">
                {["Hospital teams", "PharmaConnect", "Laboratory"].map((label, i) => (
                  <div
                    key={label}
                    className="flex items-center gap-4 rounded-xl border border-border px-4 py-4"
                  >
                    <span className="font-mono text-xs text-muted-foreground">0{i + 1}</span>
                    <span className="flex-1 text-sm font-medium">{label}</span>
                    <span aria-hidden="true" className="text-muted-foreground">
                      ↗
                    </span>
                  </div>
                ))}
              </div>
              <div className="mt-6 rounded-xl bg-muted p-4 text-xs leading-relaxed text-muted-foreground">
                Separate organisations. Multiple hospitals.
                <br />
                <strong className="font-medium text-secondary-foreground">
                  Access that follows your team's permissions.
                </strong>
              </div>
            </div>
            <p className="mt-6 text-center text-xs text-muted-foreground">
              A shared interface, designed for distinct responsibilities.
            </p>
          </div>
        </section>
        <section
          id="platform"
          aria-labelledby="platform-heading"
          className="border-y border-border bg-white px-6 py-20 lg:px-10"
        >
          <div className="mx-auto max-w-7xl">
            <p className="text-xs font-semibold uppercase tracking-[0.2em] text-muted-foreground">
              The platform
            </p>
            <div className="mt-4 flex flex-wrap items-end justify-between gap-6">
              <h2
                id="platform-heading"
                className="max-w-xl text-3xl font-medium tracking-tight sm:text-4xl"
              >
                Different responsibilities.
                <br />A familiar place to work.
              </h2>
              <p className="max-w-sm text-sm leading-relaxed text-muted-foreground">
                A consistent experience across your organisation, with dedicated workflows for each
                team.
              </p>
            </div>
            <p className="mt-6 max-w-3xl text-sm leading-relaxed text-muted-foreground">
              Explore the full platform and its roadmap. Availability varies by module: foundations
              cover the workflows described below, while planned and future capabilities are not yet
              available. Access depends on your organisation and permissions.
            </p>
            <div className="mt-10 divide-y divide-border border-y border-border">
              {platformGroups.map((group, index) => (
                <details key={group.name} open={index === 0} className="group py-6">
                  <summary className="flex cursor-pointer list-none items-center gap-5 rounded-lg py-2 focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-primary [&::-webkit-details-marker]:hidden">
                    <span
                      className={`flex h-11 w-11 shrink-0 items-center justify-center rounded-xl font-mono text-xs ${group.color}`}
                    >
                      0{index + 1}
                    </span>
                    <span className="min-w-0 flex-1">
                      <span className="block text-lg font-semibold sm:text-xl">{group.name}</span>
                      <span className="mt-1 block text-sm leading-relaxed text-muted-foreground">
                        {group.description}
                      </span>
                    </span>
                    <span
                      aria-hidden="true"
                      className="text-xl transition-transform group-open:rotate-45"
                    >
                      +
                    </span>
                  </summary>
                  <div className="grid gap-4 pt-6 sm:grid-cols-2 lg:grid-cols-3">
                    {group.modules.map((module) => (
                      <article
                        key={module.name}
                        aria-label={module.name}
                        className={`flex flex-col rounded-2xl p-6 ${group.color}`}
                      >
                        <p className="text-xs font-medium">{module.product}</p>
                        <h3 className="mt-4 text-lg font-semibold tracking-tight">{module.name}</h3>
                        <p className="mt-3 flex-1 text-sm leading-relaxed">{module.description}</p>
                        <p className="mt-6 border-t border-current/15 pt-4 text-xs font-semibold">
                          {module.status}
                        </p>
                      </article>
                    ))}
                  </div>
                </details>
              ))}
            </div>
          </div>
        </section>
        <section id="care-network" className="mx-auto max-w-7xl px-6 py-20 lg:px-10">
          <p className="text-xs font-semibold uppercase tracking-[0.2em] text-muted-foreground">
            The care network
          </p>
          <h2 className="mt-4 text-3xl font-medium tracking-tight sm:text-4xl">
            Space for every kind of care.
          </h2>
          <p className="mt-4 max-w-xl text-muted-foreground">
            Independent hospitals, multi-hospital organisations and pharmacy teams. This directory
            will feature participating organisations with their permission.
          </p>
          <div className="mt-9 grid gap-5 md:grid-cols-3">
            {[
              {
                name: "Hospital networks",
                detail: "Independent hospitals and multi-hospital organisations.",
              },
              {
                name: "Pharmacy teams",
                detail: "Independent and hospital pharmacies, connected to care.",
              },
              {
                name: "Laboratory teams",
                detail: "Diagnostic teams working alongside your care network.",
              },
            ].map(({ name, detail }) => (
              <article
                key={name}
                className="rounded-2xl border border-border bg-white p-7"
              >
                <p className="text-xs text-muted-foreground">Designed for your team</p>
                <h3 className="mt-5 text-lg font-semibold">{name}</h3>
                <p className="mt-3 text-sm leading-relaxed text-muted-foreground">{detail}</p>
              </article>
            ))}
          </div>
        </section>
        <section
          id="trust"
          className="bg-(--hms-band) px-6 py-20 text-(--hms-band-ink) lg:px-10"
        >
          <div className="mx-auto max-w-7xl">
            <p className="text-xs font-semibold uppercase tracking-[0.2em] text-(--hms-band-muted)">
              Trust, with transparency
            </p>
            <h2 className="mt-4 max-w-2xl text-3xl font-medium tracking-tight sm:text-4xl">
              Care deserves a clear standard.
            </h2>
            <p className="mt-5 max-w-2xl leading-relaxed text-(--hms-band-muted)">
              Our trust centre will make independent assurance and supporting documentation easy to
              find. The items below are placeholders, not claims of certification or compliance.
            </p>
            <div className="mt-10 grid gap-5 md:grid-cols-3">
              {["SOC 2 Type II", "ISO/IEC 27001", "HIPAA safeguards"].map((label, i) => (
                <article
                  key={label}
                  className="rounded-2xl border border-(--hms-band-line) p-7"
                >
                  <p className="text-xs uppercase tracking-widest text-(--hms-band-muted)">
                    {i === 2 ? "Compliance documentation" : "Certification placeholder"}
                  </p>
                  <h3 className="mt-5 text-xl font-medium">{label}</h3>
                  <p className="mt-5 inline-block rounded-full border border-(--hms-band-line) px-3 py-1 text-xs">
                    {i === 2 ? "Documentation pending" : "Not yet certified"}
                  </p>
                </article>
              ))}
            </div>
          </div>
        </section>
        <section
          id="reviews"
          className="mx-auto grid max-w-7xl gap-10 px-6 py-20 md:grid-cols-2 lg:px-10"
        >
          <div>
            <p className="text-xs font-semibold uppercase tracking-[0.2em] text-muted-foreground">
              Voices from care
            </p>
            <h2 className="mt-4 text-3xl font-medium tracking-tight">
              Real experiences.
              <br />
              Shared with permission.
            </h2>
            <p className="mt-5 max-w-md text-sm leading-relaxed text-muted-foreground">
              No customer endorsements or ratings are published yet. Verified testimonials and
              reviews will appear here after customer approval.
            </p>
          </div>
          <div className="grid gap-4">
            {["Customer testimonials", "Verified reviews"].map((label) => (
              <div
                key={label}
                className="rounded-2xl border border-dashed border-border p-6"
              >
                <h3 className="font-medium">{label}</h3>
                <p className="mt-2 text-sm text-muted-foreground">Awaiting approved content</p>
              </div>
            ))}
          </div>
        </section>
        <section className="mx-6 mb-16 rounded-3xl bg-accent px-6 py-14 text-center lg:mx-auto lg:max-w-7xl">
          <h2 className="text-3xl font-medium tracking-tight">Bring your care network together.</h2>
          <p className="mt-4 text-muted-foreground">Make room for a more connected working day.</p>
          <a
            href="/signup"
            className="mt-7 inline-block rounded-full bg-primary px-7 py-4 text-sm font-semibold text-white hover:bg-(--hms-accent-strong)"
          >
            Start your workspace <span aria-hidden="true">↗</span>
          </a>
        </section>
      </main>
      <TesserixFooter />
    </div>
  );
}
