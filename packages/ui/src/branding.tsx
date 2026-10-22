"use client";
import { useState, type ReactNode } from "react";
import {
  ArrowLeft,
  Building2,
  FlaskConical,
  HeartPulse,
  LockKeyhole,
  Pill,
  UsersRound,
} from "lucide-react";
import Link from "next/link";
import { type Branding } from "@helivanta/api";

export function TesserixFooter() {
  return (
    <footer className="border-t px-6 py-4 text-center text-xs text-muted-foreground">
      <span>
        Powered by{" "}
        <a
          href="https://tesserix.app"
          className="font-semibold text-foreground hover:underline"
          rel="noopener noreferrer"
        >
          Tesserix
        </a>
      </span>
      <span className="mx-2" aria-hidden="true">
        ·
      </span>
      <span>© {new Date().getFullYear()} Tesserix. All rights reserved.</span>
    </footer>
  );
}
export function BrandLogo({
  branding,
  compact = false,
}: {
  branding: Branding;
  compact?: boolean;
}) {
  const [failed, setFailed] = useState(false);
  if (branding.logo_url && !failed)
    return (
      // eslint-disable-next-line @next/next/no-img-element -- Load tenant images only in the browser; never proxy user URLs through an optimizer.
      <img
        src={branding.logo_url}
        alt={`${branding.display_name} logo`}
        referrerPolicy="no-referrer"
        onError={() => setFailed(true)}
        className={compact ? "h-8 w-8 object-contain" : "h-10 max-w-36 object-contain"}
      />
    );
  return (
    <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-primary text-primary-foreground">
      <HeartPulse className="h-5 w-5" aria-hidden="true" />
    </span>
  );
}
export function BrandMark({ branding }: { branding: Branding }) {
  return (
    <span className="inline-flex min-w-0 items-center gap-3">
      <BrandLogo key={branding.logo_url} branding={branding} />
      <span className="min-w-0 text-lg font-semibold tracking-tight [overflow-wrap:anywhere]">
        {branding.display_name}
      </span>
    </span>
  );
}
export function LoginSurface({ branding, children }: { branding: Branding; children: ReactNode }) {
  return (
    <div
      data-theme="default"
      className="login-page flex min-h-svh flex-col bg-background text-foreground"
    >
      <div data-brand-theme={branding.theme} className="flex flex-1 flex-col">
        <header className="mx-auto flex w-full max-w-[1440px] items-center justify-between gap-6 px-6 py-6 sm:px-10">
          <BrandMark branding={branding} />
          <Link
            href="/"
            className="inline-flex items-center gap-2 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground"
          >
            <ArrowLeft size={14} aria-hidden="true" /> Back to Helivanta
          </Link>
        </header>
        <div className="mx-auto grid w-full max-w-[1440px] flex-1 gap-8 px-6 pb-8 sm:px-10 lg:grid-cols-2 lg:gap-12">
          {branding.login_layout === "split" && (
            <aside className="login-story relative hidden flex-col justify-center overflow-hidden rounded-[2rem] p-10 lg:flex xl:p-14">
              <p className="flex items-center gap-2 text-xs font-semibold uppercase tracking-[0.18em] text-(--hms-accent-strong)">
                <span className="h-1.5 w-1.5 rounded-full bg-primary" /> Connected care, made
                simpler
              </p>
              <h2 className="mt-7 max-w-lg text-[clamp(2rem,3.4vw,3.5rem)] font-medium leading-[1.1] tracking-[-0.045em]">
                A little less complexity.
                <br />
                <span className="text-(--hms-accent-strong)">A lot more care.</span>
              </h2>
              <p className="mt-5 max-w-sm text-base leading-relaxed text-muted-foreground">
                {branding.welcome_text}
              </p>
              <div
                className="relative my-10 rounded-2xl border border-border bg-card p-6 shadow-sm"
                aria-label="Connected hospital, pharmacy and laboratory teams"
              >
                <div className="flex items-center gap-3 border-b border-border pb-5">
                  <span className="rounded-xl bg-(--hms-accent-dim) p-3 text-(--hms-accent-strong)">
                    <Building2 size={22} aria-hidden="true" />
                  </span>
                  <div>
                    <p className="text-sm font-semibold">Your organisation</p>
                    <p className="mt-1 text-xs text-muted-foreground">One connected workspace</p>
                  </div>
                </div>
                <div className="mt-5 grid grid-cols-3 gap-3">
                  {[
                    { icon: UsersRound, label: "Care teams" },
                    { icon: Pill, label: "Pharmacy" },
                    { icon: FlaskConical, label: "Laboratory" },
                  ].map(({ icon: Icon, label }) => (
                    <div
                      key={label}
                      className="flex flex-col items-center gap-3 rounded-xl bg-background px-2 py-5 text-center"
                    >
                      <Icon size={20} className="text-(--hms-accent-strong)" aria-hidden="true" />
                      <span className="text-xs font-medium">{label}</span>
                    </div>
                  ))}
                </div>
              </div>
              <p className="text-xs leading-relaxed text-muted-foreground">
                Built around your people. Personalised for your organisation.
              </p>
            </aside>
          )}
          <main
            className={`flex min-w-0 flex-col items-center justify-center py-10 sm:py-14 ${branding.login_layout === "centered" ? "lg:col-span-2" : ""}`}
          >
            <div className="login-content w-full max-w-[400px]">
              <span className="mb-7 inline-flex h-12 w-12 items-center justify-center rounded-2xl border border-border bg-card text-(--hms-accent-strong)">
                <LockKeyhole size={21} aria-hidden="true" />
              </span>
              {children}
              <p className="mt-8 text-center text-xs leading-relaxed text-muted-foreground">
                Need access? Contact your organisation's administrator.
              </p>
            </div>
          </main>
        </div>
      </div>
      <TesserixFooter />
    </div>
  );
}
