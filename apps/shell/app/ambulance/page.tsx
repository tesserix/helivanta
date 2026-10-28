"use client";

import { Button, SelectControl } from "@helivanta/ui";
import { useState } from "react";
import { Ambulance, ArrowUpRight, Phone, ShieldCheck } from "lucide-react";
import {
  emergencyAccess,
  emergencyCopy as copy,
  type EmergencyCountry,
} from "@helivanta/emergency-access";

export default function AmbulancePage() {
  const [country, setCountry] = useState<EmergencyCountry>("IN");
  const access = emergencyAccess(country);
  const service = access.services[0];
  return (
    <div className="public-site min-h-screen bg-background text-foreground">
      <a
        href="#emergency-help"
        className="sr-only focus:not-sr-only focus:absolute focus:z-50 focus:bg-card focus:p-4"
      >
        Skip to emergency help
      </a>
      <header className="border-b border-border">
        <div className="mx-auto flex max-w-5xl flex-wrap items-center justify-between gap-4 px-5 py-5 sm:px-8">
          <a
            href="/"
            className="inline-flex min-h-11 items-center gap-2 font-semibold"
            aria-label={copy.home}
          >
            <Ambulance aria-hidden="true" className="size-6 text-primary" />
            {copy.brand}
          </a>
          <p className="text-sm text-muted-foreground">{copy.byline}</p>
        </div>
      </header>
      <main id="emergency-help" className="mx-auto max-w-5xl px-5 py-8 sm:px-8 sm:py-14">
        <div className="mb-8 max-w-2xl">
          <p className="mb-3 text-sm font-semibold text-primary">Emergency calling</p>
          <h1 className="text-4xl leading-tight font-semibold tracking-tight sm:text-5xl">
            {copy.title}
          </h1>
          <p className="mt-4 text-lg leading-relaxed text-muted-foreground">{copy.introduction}</p>
        </div>
        <div className="grid items-start gap-6 md:grid-cols-[1.2fr_1fr]">
          <section
            className="rounded-lg border border-border bg-card p-5 shadow-sm sm:p-8"
            aria-label="Emergency calling"
          >
            <label htmlFor="emergency-country" className="mb-2 block font-medium">
              {copy.countryLabel}
            </label>
            <SelectControl
              id="emergency-country"
              aria-label={copy.countryLabel}
              value={country}
              onValueChange={(next) => setCountry(next === "IN" ? "IN" : "OTHER")}
              className="mb-7 min-h-12 w-full text-base"
              options={[
                { value: "IN", label: copy.india },
                { value: "OTHER", label: copy.otherCountry },
              ]}
            />
            {service ? (
              <>
                <h2 className="text-xl font-semibold">{service.title}</h2>
                <p className="my-3 text-6xl font-semibold tracking-tight text-primary">
                  {service.number}
                </p>
                {/* The emergency dial is the one place a larger target is warranted (#1434 D2). */}
                <Button asChild size="xl" className="mt-5 w-full gap-3 font-semibold">
                  <a href={service.dialUrl}>
                    <Phone aria-hidden="true" className="size-5 shrink-0" />
                    {copy.callLabel}
                  </a>
                </Button>
                <p className="mt-4 text-sm leading-relaxed text-muted-foreground">
                  {copy.callNotice}
                </p>
              </>
            ) : (
              <p
                role="status"
                className="rounded-xl bg-(--hms-amber-tint) p-4 leading-relaxed text-(--hms-amber)"
              >
                {copy.unsupported}
              </p>
            )}
          </section>
          <div className="space-y-6">
            {access.regionalInformation.length > 0 ? (
              <section
                aria-labelledby="regional-numbers"
                className="rounded-lg border border-border p-5 sm:p-7"
              >
                <h2 id="regional-numbers" className="text-lg font-semibold">
                  {copy.regionalTitle}
                </h2>
                <dl className="mt-5 space-y-5">
                  {access.regionalInformation.map((regional) => (
                    <div key={regional.number} className="flex gap-4">
                      <dt className="text-2xl font-semibold">{regional.number}</dt>
                      <dd className="pt-1 text-sm leading-relaxed text-muted-foreground">
                        {regional.scope}
                      </dd>
                    </div>
                  ))}
                </dl>
                <p className="mt-5 text-sm leading-relaxed text-muted-foreground">
                  {copy.regionalNotice}
                </p>
              </section>
            ) : null}
            <aside className="rounded-lg bg-accent p-5 text-(--hms-accent-strong) sm:p-7">
              <ShieldCheck aria-hidden="true" className="mb-3 size-6" />
              <p className="font-medium">{copy.bookingNotice}</p>
              <p className="mt-3 text-sm leading-relaxed">{copy.privacyNotice}</p>
            </aside>
          </div>
        </div>
        <footer className="mt-10 border-t border-border pt-6 text-sm text-muted-foreground">
          <h2 className="font-medium text-foreground">{copy.evidenceTitle}</h2>
          {service ? (
            <a
              href={service.sourceUrl}
              referrerPolicy="no-referrer"
              className="mt-2 inline-flex min-h-11 items-center gap-1 underline underline-offset-4"
            >
              {copy.evidenceLabel}
              <ArrowUpRight className="size-4" aria-hidden="true" />
            </a>
          ) : null}
          <p className="mt-2">
            {copy.reviewedLabel}: <time dateTime={access.reviewedOn}>{access.reviewedOn}</time> ·{" "}
            {copy.revisionLabel}: {access.revision}
          </p>
        </footer>
      </main>
    </div>
  );
}
