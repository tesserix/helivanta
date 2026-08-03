"use client";

import type { ReactNode } from "react";

// Zone hrefs are absolute paths — cross-zone navigation is a hard
// navigation by design (spec D3), so plain <a> tags, not next/link.
const NAV = [
  { href: "/", label: "Dashboard" },
  { href: "/medicore/opd", label: "OPD" },
  { href: "/medicore/ipd", label: "IPD" },
  { href: "/pharmacy", label: "Pharmacy" },
  { href: "/lab", label: "Lab" },
];

export function HmsShell({ active, children }: { active: string; children: ReactNode }) {
  return (
    <div className="flex min-h-screen">
      <aside className="w-56 shrink-0 border-r bg-sidebar text-sidebar-foreground">
        <div className="px-4 py-5 text-lg font-semibold">HMS</div>
        <nav className="flex flex-col gap-1 px-2">
          {NAV.map((item) => (
            <a
              key={item.href}
              href={item.href}
              aria-current={active === item.href ? "page" : undefined}
              className={`rounded-md px-3 py-2 text-sm ${
                active === item.href
                  ? "bg-sidebar-accent font-medium text-sidebar-accent-foreground"
                  : "hover:bg-sidebar-accent/50"
              }`}
            >
              {item.label}
            </a>
          ))}
        </nav>
      </aside>
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-14 items-center justify-between border-b px-6">
          <span className="text-sm text-muted-foreground">Hospital Management System</span>
          <a href="/logout" className="text-sm underline-offset-4 hover:underline">
            Sign out
          </a>
        </header>
        <main className="flex-1 p-6">{children}</main>
      </div>
    </div>
  );
}
