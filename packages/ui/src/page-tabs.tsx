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
            className={`-mb-px border-b-2 px-3 py-3 text-sm font-medium transition-colors ${
              active
                ? "border-(--hms-accent) text-foreground"
                : "border-transparent text-muted-foreground hover:text-foreground"
            }`}
          >
            {tab.label}
          </Link>
        );
      })}
    </nav>
  );
}
