import type { Metadata } from "next";
import { Button, TesserixFooter } from "@helivanta/ui";

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
              Connect your hospital and pharmacy teams under one organisation, with your own
              branding and access controls.
            </p>
            <ol className="mt-9 space-y-5 text-sm">
              <li>01 — Set up your organisation</li>
              <li>02 — Choose your workspace address</li>
              <li>03 — Add hospitals and invite your team</li>
            </ol>
          </div>
          <section
            aria-labelledby="signup-status"
            className="self-center rounded-lg border border-border bg-card p-8 sm:p-10"
          >
            <p className="inline-block rounded-md bg-accent px-3 py-1 text-xs text-(--hms-band-line)">
              Onboarding preview
            </p>
            <h2 id="signup-status" className="mt-6 text-2xl font-medium">
              Self-service signup is coming soon
            </h2>
            <p className="mt-4 text-sm leading-relaxed text-muted-foreground">
              New workspace registration is not available yet. Visit Tesserix to get in touch about
              onboarding, or sign in if your organisation already has access.
            </p>
            <Button asChild size="lg" className="mt-7 w-full">
              <a href="https://tesserix.app">Visit Tesserix</a>
            </Button>
            <a
              href="/login?start=1"
              className="mt-5 block text-center text-sm font-medium underline underline-offset-4"
            >
              Sign in to an existing workspace
            </a>
          </section>
        </div>
      </main>
      <TesserixFooter />
    </div>
  );
}
