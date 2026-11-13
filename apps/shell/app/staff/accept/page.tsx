import type { Metadata } from "next";
import { TesserixFooter } from "@helivanta/ui";
import { StaffAcceptance } from "./acceptance";

export const metadata: Metadata = {
  title: "Staff account setup | Helivanta",
  referrer: "no-referrer",
};

export default function StaffAcceptancePage() {
  return (
    <div className="public-site flex min-h-screen flex-col bg-background text-foreground">
      <header className="mx-auto w-full max-w-6xl px-6 py-8">
        <a href="/" className="text-xl font-semibold tracking-tight">
          helivanta.
        </a>
      </header>
      <main className="mx-auto flex w-full max-w-xl flex-1 items-center px-6 py-12">
        <section
          aria-label="Staff account setup"
          className="w-full rounded-xl border border-border bg-card p-8 shadow-sm sm:p-10"
        >
          <p className="mb-5 text-sm font-medium text-muted-foreground">Staff account setup</p>
          <StaffAcceptance />
        </section>
      </main>
      <TesserixFooter />
    </div>
  );
}
