import type { ReactNode } from "react";
import { LogOut } from "lucide-react";
import { ZONES, activeZone } from "./zones";

// Two-rail chrome ported from tesserix-home's AdminSidebar: a 4rem icon
// rail switching zones + a 14rem panel listing the active zone's pages.
// No hooks — active state comes in as a prop, so this stays a server
// component and works identically in every zone app.
export function HmsShell({ active, children }: { active: string; children: ReactNode }) {
  const zone = activeZone(active);

  return (
    <div className="flex min-h-screen">
      {/* Left rail: zone switcher */}
      <aside className="flex w-16 shrink-0 flex-col items-center border-r border-sidebar-border bg-sidebar">
        <div className="flex h-16 items-center justify-center">
          <a
            href="/"
            aria-label="HMS home"
            className="flex h-9 w-9 items-center justify-center rounded-lg text-lg font-semibold text-sidebar-primary"
          >
            H
          </a>
        </div>
        <nav aria-label="Zones" className="flex flex-1 flex-col items-center gap-2 py-4">
          {ZONES.map((z) => {
            const isActive = z.key === zone.key;
            return (
              <a
                key={z.key}
                href={z.href}
                title={z.label}
                aria-label={z.label}
                aria-current={isActive ? "page" : undefined}
                className={`flex h-10 w-10 items-center justify-center rounded-lg transition-colors ${
                  isActive
                    ? "bg-sidebar-accent text-sidebar-accent-foreground"
                    : "text-sidebar-foreground/70 hover:bg-sidebar-accent/50 hover:text-sidebar-foreground"
                }`}
              >
                <z.icon className="h-5 w-5" aria-hidden="true" />
              </a>
            );
          })}
        </nav>
        <div className="flex flex-col items-center pb-4">
          <a
            href="/logout"
            title="Sign out"
            aria-label="Sign out"
            className="flex h-10 w-10 items-center justify-center rounded-lg text-sidebar-foreground/70 transition-colors hover:bg-sidebar-accent/50 hover:text-sidebar-foreground"
          >
            <LogOut className="h-4 w-4" aria-hidden="true" />
          </a>
        </div>
      </aside>

      {/* Secondary panel: active zone's pages */}
      <aside className="flex w-56 shrink-0 flex-col border-r border-sidebar-border bg-sidebar">
        <div className="flex h-16 items-center px-5">
          <h2 className="text-sm font-semibold text-sidebar-foreground">{zone.label}</h2>
        </div>
        <div className="border-t border-sidebar-border" />
        <nav aria-label={zone.label} className="flex flex-col gap-1 px-3 py-4">
          {zone.pages.map((pageLink) => {
            const isActive = active === pageLink.href;
            return (
              <a
                key={pageLink.href}
                href={pageLink.href}
                aria-current={isActive ? "page" : undefined}
                className={`rounded-lg px-3 py-2 text-sm font-medium transition-colors ${
                  isActive
                    ? "bg-sidebar-accent text-sidebar-foreground"
                    : "text-sidebar-foreground/70 hover:bg-sidebar-accent/50 hover:text-sidebar-foreground"
                }`}
              >
                {pageLink.label}
              </a>
            );
          })}
        </nav>
      </aside>

      {/* Content */}
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
