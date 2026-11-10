import type { Metadata } from "next";
import { TesserixFooter } from "@helivanta/ui";
import { SignupVerification } from "./verification";

export const metadata: Metadata = {
  title: "Verify your workspace login | Helivanta",
  referrer: "no-referrer",
};
export default function SignupVerificationPage() {
  return (
    <div className="public-site flex min-h-screen flex-col bg-background text-foreground">
      <header className="mx-auto w-full max-w-6xl px-6 py-8">
        <a href="/" className="text-xl font-semibold tracking-tight">
          helivanta.
        </a>
      </header>
      <main className="mx-auto flex w-full max-w-xl flex-1 items-center px-6 py-14">
        <section
          aria-label="Workspace login verification"
          className="w-full rounded-lg border border-border bg-card p-8 sm:p-10"
        >
          <SignupVerification />
        </section>
      </main>
      <TesserixFooter />
    </div>
  );
}
