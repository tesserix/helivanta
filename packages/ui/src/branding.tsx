"use client";
import { useState, type ReactNode } from "react";
import { HeartPulse } from "lucide-react";
import { type Branding } from "@helivanta/api";

export function TesserixFooter() {
  return (
    <footer className="border-t px-6 py-4 text-center text-xs text-muted-foreground">
      <span>
        Powered by{" "}
        <a
          href="https://tesserix.com"
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
    <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-primary text-white">
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
      data-brand-theme={branding.theme}
      className="flex min-h-screen flex-col bg-background text-foreground"
    >
      <div className="flex flex-1">
        {branding.login_layout === "split" && (
          <aside className="hidden w-[44%] flex-col justify-between bg-primary px-12 py-10 text-white lg:flex">
            <BrandMark branding={{ ...branding, logo_url: "" }} />
            <div className="max-w-lg">
              <span className="text-xs font-medium uppercase tracking-[0.22em] text-white/75">
                Your connected care workspace
              </span>
              <h2 className="mt-5 text-4xl font-semibold leading-tight tracking-tight">
                More time for people.
                <br />
                One place for your team.
              </h2>
              <p className="mt-6 text-base leading-relaxed text-white/80">
                {branding.welcome_text}
              </p>
              <div className="mt-10 flex flex-wrap gap-3 text-xs text-white/85">
                <span className="rounded-full border border-white/25 px-4 py-2">
                  Hospital teams
                </span>
                <span className="rounded-full border border-white/25 px-4 py-2">
                  Coordinated care
                </span>
              </div>
            </div>
            <p className="text-xs text-white/65">Hospital management, thoughtfully connected.</p>
          </aside>
        )}
        <main className="flex min-w-0 flex-1 flex-col items-center justify-center gap-8 px-5 py-12">
          <BrandMark branding={branding} />
          <div className="w-full max-w-md">{children}</div>
        </main>
      </div>
      <TesserixFooter />
    </div>
  );
}
