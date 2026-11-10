import type { Metadata } from "next";
import { TesserixFooter } from "@helivanta/ui";

import { SignupForm } from "./signup-form";

export const metadata: Metadata = { title: "Start your workspace | Helivanta" };

export default function SignupPage() {
  return (
    <div className="public-site flex min-h-screen flex-col bg-background text-foreground">
      <header className="mx-auto w-full max-w-6xl px-6 py-8">
        <a href="/" className="text-xl font-semibold tracking-tight">
          helivanta.
        </a>
      </header>
      <main className="mx-auto flex w-full max-w-6xl flex-1 items-center px-6 py-14">
        <div className="grid w-full gap-12 md:grid-cols-2">
          <div>
            <p className="text-xs font-semibold uppercase tracking-[0.2em] text-muted-foreground">
              A workspace of your own
            </p>
            <h1 className="mt-5 text-4xl leading-tight font-medium tracking-tight sm:text-5xl">
              Bring your organisation
              <br />
              into Helivanta.
            </h1>
            <p className="mt-6 max-w-md leading-relaxed text-muted-foreground">
              Start with your hospital or independent pharmacy, then configure your team and access.
            </p>
            <ol className="mt-9 space-y-5 text-sm">
              <li>01 — Set up your organisation</li>
              <li>02 — Verify your email and create a login</li>
              <li>03 — Sign in and set up your team</li>
            </ol>
          </div>
          <section
            aria-label="Workspace registration"
            className="self-center rounded-lg border border-border bg-card p-8 sm:p-10"
          >
            <SignupForm />
          </section>
        </div>
      </main>
      <TesserixFooter />
    </div>
  );
}
