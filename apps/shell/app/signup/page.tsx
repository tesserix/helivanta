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
            <h1 className="text-4xl leading-tight font-medium tracking-tight sm:text-5xl">
              Bring your organisation into Helivanta.
            </h1>
            <p className="mt-6 max-w-md leading-relaxed text-muted-foreground">
              Start with your hospital or independent pharmacy. You can add your team after you sign
              in.
            </p>
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
