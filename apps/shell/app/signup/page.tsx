import type { Metadata } from "next";
import { TesserixFooter } from "@helivanta/ui";

export const metadata: Metadata = { title: "Start your workspace | Helivanta" };

export default function SignupPage() {
  return (
    <div className="public-site flex min-h-screen flex-col bg-(--site-canvas) text-(--site-ink)">
      <header className="mx-auto w-full max-w-6xl px-6 py-8">
        <a href="/" className="text-xl font-semibold tracking-tight">
          helivanta.
        </a>
      </header>
      <main className="mx-auto flex w-full max-w-6xl flex-1 items-center px-6 py-14">
        <div className="grid w-full gap-12 md:grid-cols-2">
          <div>
            <p className="text-xs font-semibold uppercase tracking-[0.2em] text-(--site-eyebrow)">
              A workspace of your own
            </p>
            <h1 className="mt-5 text-4xl leading-tight font-medium tracking-tight sm:text-5xl">
              Bring your organisation
              <br />
              into Helivanta.
            </h1>
            <p className="mt-6 max-w-md leading-relaxed text-(--site-muted)">
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
            className="self-center rounded-3xl border border-(--site-border) bg-white p-8 sm:p-10"
          >
            <p className="inline-block rounded-full bg-(--site-tint) px-3 py-1 text-xs text-(--site-signup-badge)">
              Onboarding preview
            </p>
            <h2 id="signup-status" className="mt-6 text-2xl font-medium">
              Self-service signup is coming soon
            </h2>
            <p className="mt-4 text-sm leading-relaxed text-(--site-muted)">
              New workspace registration is not available yet. Visit Tesserix to get in touch about
              onboarding, or sign in if your organisation already has access.
            </p>
            <a
              href="https://tesserix.app"
              className="mt-7 block rounded-full bg-(--site-primary) px-6 py-3 text-center text-sm font-semibold text-white hover:bg-(--site-primary-hover)"
            >
              Visit Tesserix
            </a>
            <a
              href="/login"
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
