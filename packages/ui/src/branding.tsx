"use client";
import { useState, type ReactNode } from "react";
import Link from "next/link";
import { type Branding } from "@helivanta/api";
import { HelivantaMark } from "./brand-mark.generated";

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
  onInk = false,
}: {
  branding: Branding;
  compact?: boolean;
  // A tenant logo is drawn for a light surface; on ink it sits on a card chip
  // so a dark logo stays visible (#1836 D5).
  onInk?: boolean;
}) {
  const [failed, setFailed] = useState(false);
  if (branding.logo_url && !failed) {
    const logo = (
      // eslint-disable-next-line @next/next/no-img-element -- Load tenant images only in the browser; never proxy user URLs through an optimizer.
      <img
        src={branding.logo_url}
        alt={`${branding.display_name} logo`}
        referrerPolicy="no-referrer"
        onError={() => setFailed(true)}
        className={compact ? "h-8 w-8 object-contain" : "h-10 max-w-36 object-contain"}
      />
    );
    return onInk ? <span className="shrink-0 rounded-lg bg-card p-1.5">{logo}</span> : logo;
  }
  // No logo: the Helivanta mark, on the surface itself (#1835 D6).
  return <HelivantaMark onInk={onInk} className="size-9 shrink-0" />;
}
export function BrandMark({ branding, onInk = false }: { branding: Branding; onInk?: boolean }) {
  return (
    <span className="inline-flex min-w-0 items-center gap-3">
      <BrandLogo key={branding.logo_url} branding={branding} onInk={onInk} />
      <span className="min-w-0 text-lg font-semibold tracking-tight [overflow-wrap:anywhere]">
        {branding.display_name}
      </span>
    </span>
  );
}
export function LoginSurface({
  branding,
  homeHref,
  entry = false,
  children,
}: {
  branding: Branding;
  // Only the platform's own login links its mark home; a hospital's logo
  // never leads to Helivanta's marketing page (#1431 D2).
  homeHref?: string;
  // Entry pages (the sign-in landing, the first signup step) may show the
  // branding story beside the card. Every task screen is one centred card
  // with nothing beside or below it (#1767 D1, D2).
  entry?: boolean;
  children: ReactNode;
}) {
  const story = entry && branding.login_layout === "split";
  // On the ink panel the ring must be the rail's: the canvas ring is ultramarine,
  // which does not show on ink (#1836 D5).
  const mark = (onInk: boolean) =>
    homeHref ? (
      <Link
        href={homeHref}
        aria-label={`${branding.display_name} home`}
        className={`rounded-lg focus-visible:outline-2 focus-visible:outline-offset-4 ${onInk ? "focus-visible:outline-(--sidebar-ring)" : "focus-visible:outline-ring"}`}
      >
        <BrandMark branding={branding} onInk={onInk} />
      </Link>
    ) : (
      <BrandMark branding={branding} onInk={onInk} />
    );
  return (
    <div
      data-theme="default"
      className="login-page flex min-h-svh flex-col bg-background text-foreground"
    >
      <div data-brand-theme={branding.theme} className="flex flex-1 flex-col">
        {/* With the ink panel the brand lives in the panel from lg, so it is on
            screen once (#1836 D5); below lg the panel is hidden and this shows. */}
        <header
          className={`mx-auto flex w-full max-w-[1440px] items-center justify-between gap-6 px-6 py-6 sm:px-10 ${story ? "lg:hidden" : ""}`}
        >
          {mark(false)}
        </header>
        <div
          className={`mx-auto grid w-full max-w-[1440px] flex-1 gap-8 px-6 pb-8 sm:px-10 lg:grid-cols-2 lg:gap-12 ${story ? "lg:max-w-none lg:grid-cols-[minmax(0,5fr)_minmax(0,7fr)] lg:gap-0 lg:px-0 lg:pb-0" : ""}`}
        >
          {story && (
            <aside className="login-story hidden min-h-svh flex-col justify-between gap-10 p-10 lg:flex xl:p-14">
              {mark(true)}
              <div>
                <h2 className="max-w-lg text-[clamp(2rem,3.4vw,3rem)] leading-[1.1] font-semibold tracking-tight">
                  A little less complexity.
                  <br />A lot more care.
                </h2>
                <p className="mt-5 max-w-sm text-base leading-relaxed text-muted-foreground">
                  {branding.welcome_text}
                </p>
              </div>
            </aside>
          )}
          <div className={`flex min-w-0 flex-col ${story ? "" : "lg:col-span-2"}`}>
            <main className="flex flex-1 flex-col items-center justify-center py-10 sm:py-14">
              <div className="login-content w-full max-w-[400px]">{children}</div>
            </main>
            {story && <TesserixFooter />}
          </div>
        </div>
      </div>
      {entry && !story && <TesserixFooter />}
    </div>
  );
}
