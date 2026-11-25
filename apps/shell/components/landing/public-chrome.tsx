import Link from "next/link";
import { Button, HelivantaMark } from "@helivanta/ui";
import { homeWidth } from "@/app/page-width";

// The public pages' nav (#1837 D4). The section links are the home page's
// chapters, so from /roadmap they lead back to /.
export function PublicNav({ home }: { home: boolean }) {
  const anchor = home ? "" : "/";
  return (
    <header className="border-b border-border">
      <nav
        aria-label="Main navigation"
        className={`${homeWidth} flex flex-wrap items-center justify-between gap-5 py-5`}
      >
        <Link
          href="/"
          aria-label="Helivanta home"
          className="flex items-center gap-3 text-xl font-bold tracking-[-0.03em]"
        >
          {/* The lockup (#1835 D3): mark, then the word in the page's Anek face;
                the outlined wordmark is for images, where no font is loaded. */}
          <HelivantaMark className="size-8" />
          <span className="home-wordmark">helivanta</span>
        </Link>
        <div className="hidden items-center gap-7 text-sm text-muted-foreground lg:flex">
          <a href={`${anchor}#how`} className="hover:text-foreground">
            How it works
          </a>
          <a href={`${anchor}#why`} className="hover:text-foreground">
            Why Helivanta
          </a>
          <a href={`${anchor}#platform`} className="hover:text-foreground">
            Platform
          </a>
          <Link href="/roadmap" className="hover:text-foreground">
            Roadmap
          </Link>
        </div>
        <div className="flex items-center gap-4 text-sm font-medium whitespace-nowrap sm:gap-5">
          <Link
            href="/ambulance"
            className="inline-flex min-h-11 items-center text-(--hms-amber) underline underline-offset-4"
          >
            Emergency help
          </Link>
          <a href="/login?start=1" className="hover:underline">
            Sign in
          </a>
          {/* The hero's identical action sits directly below on small screens. */}
          <Button asChild className="max-sm:hidden">
            <Link href="/workspace">Start your workspace</Link>
          </Button>
        </div>
      </nav>
    </header>
  );
}

// Above the shared footer on the public pages only; the workspace chrome
// uses TesserixFooter too and never shows a roadmap link (#1837 D4).
export function PublicLinks() {
  return (
    <nav aria-label="Public pages" className={`${homeWidth} flex flex-wrap gap-6 py-6 text-sm`}>
      <Link href="/roadmap" className="hover:underline">
        Roadmap
      </Link>
      <Link href="/roadmap#trust" className="hover:underline">
        Trust
      </Link>
      <Link href="/ambulance" className="hover:underline">
        Emergency help
      </Link>
    </nav>
  );
}
