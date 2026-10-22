"use client";

import Link from "next/link";

export type PageTab = { key: string; label: string; href: string };

/**
 * PageTabs puts fixed states of one list across the top of a page (#1295
 * D7). Each tab is a link, so it works with the keyboard, the back button
 * and a shared URL. Tabs carry no counts: the open tab's total is in the
 * pager, and a count on another tab would be a separate read that could
 * disagree with it.
 */
export function PageTabs({
  label,
  tabs,
  current,
}: {
  label: string;
  tabs: PageTab[];
  current: string;
}) {
  return (
    <nav aria-label={label} className="flex gap-1 border-b px-3">
      {tabs.map((tab) => {
        const active = tab.key === current;
        return (
          <Link
            key={tab.key}
            href={tab.href}
            aria-current={active ? "page" : undefined}
            className={`relative px-3 py-3 text-sm transition-colors ${
              active
                ? "font-semibold text-foreground"
                : "font-medium text-muted-foreground hover:text-foreground"
            }`}
          >
            {tab.label}
            {/* Its own element, not a border colour: @tesserix/web's unlayered
                base rule forces every border to var(--border) (styles.css). */}
            {active && (
              <span
                data-tab-indicator
                aria-hidden="true"
                className="absolute inset-x-3 -bottom-px h-0.5 rounded-full bg-(--hms-accent)"
              />
            )}
          </Link>
        );
      })}
    </nav>
  );
}
