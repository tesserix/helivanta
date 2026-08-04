"use client";

import { useEffect, useState, type ReactNode } from "react";
import { ChevronsLeft, ChevronsRight, LogOut, PanelLeftClose, PanelLeftOpen } from "lucide-react";
import { ZONES, activeZone } from "./zones";

// Two-rail chrome in the tesserix-home AdminSidebar style. Both rails
// collapse independently: the zone rail toggles icons-only ↔ icons+labels,
// the page panel toggles open ↔ hidden. Preferences persist per browser.
// All links stay plain <a> — cross-zone navigation is a hard navigation
// by design (phase 1 spec D3).

const RAIL_KEY = "hms.rail.expanded";
const PANEL_KEY = "hms.panel.open";

function usePersistedFlag(key: string, fallback: boolean) {
  const [value, setValue] = useState(fallback);
  useEffect(() => {
    const stored = window.localStorage.getItem(key);
    if (stored !== null) setValue(stored === "1");
  }, [key]);
  const update = (next: boolean) => {
    setValue(next);
    window.localStorage.setItem(key, next ? "1" : "0");
  };
  return [value, update] as const;
}

export function HmsShell({ active, children }: { active: string; children: ReactNode }) {
  const zone = activeZone(active);
  const [railExpanded, setRailExpanded] = usePersistedFlag(RAIL_KEY, false);
  const [panelOpen, setPanelOpen] = usePersistedFlag(PANEL_KEY, true);

  const activePage = zone.pages.find((p) => p.href === active);

  return (
    <div className="flex h-screen overflow-hidden">
      {/* Zone rail: icons-only or icons+labels. Matches tesserix admin:
          one step darker than the page panel, faint border between the
          rails, none against the content. */}
      <aside
        className={`hms-sidebar-border flex shrink-0 flex-col border-r bg-(--sidebar-rail) transition-[width] duration-200 ease-out motion-reduce:transition-none ${
          railExpanded ? "w-56" : "w-16"
        }`}
      >
        <div className={`flex h-16 items-center ${railExpanded ? "px-5" : "justify-center"}`}>
          <a
            href="/"
            aria-label="HMS home"
            className="flex h-9 items-center gap-2 rounded-lg text-lg font-semibold text-sidebar-primary"
          >
            <span className="flex h-9 w-9 items-center justify-center">H</span>
            {railExpanded && <span className="text-sm font-semibold tracking-wide">HMS</span>}
          </a>
        </div>
        <div className="hms-sidebar-border mx-2 border-t" />
        <nav
          aria-label="Zones"
          className={`flex min-h-0 flex-1 flex-col gap-1 overflow-y-auto py-4 ${railExpanded ? "px-3" : "items-center px-0"}`}
        >
          {ZONES.map((z) => {
            const isActive = z.key === zone.key;
            return (
              <a
                key={z.key}
                href={z.href}
                title={railExpanded ? undefined : z.label}
                aria-label={z.label}
                aria-current={isActive ? "page" : undefined}
                className={`flex h-10 items-center rounded-lg transition-colors ${
                  railExpanded ? "w-full gap-3 px-3" : "w-10 justify-center"
                } ${
                  isActive
                    ? "bg-sidebar-accent text-sidebar-accent-foreground"
                    : "text-sidebar-foreground/70 hover:bg-sidebar-accent/50 hover:text-sidebar-foreground"
                }`}
              >
                <z.icon className="h-5 w-5 shrink-0" aria-hidden="true" />
                {railExpanded && <span className="truncate text-sm font-medium">{z.label}</span>}
              </a>
            );
          })}
        </nav>
        <div
          className={`flex flex-col gap-1 py-3 ${
            railExpanded ? "px-3" : "items-center px-0"
          }`}
        >
          <a
            href="/logout"
            title={railExpanded ? undefined : "Sign out"}
            aria-label="Sign out"
            className={`flex h-10 items-center rounded-lg text-sidebar-foreground/70 transition-colors hover:bg-sidebar-accent/50 hover:text-sidebar-foreground ${
              railExpanded ? "w-full gap-3 px-3" : "w-10 justify-center"
            }`}
          >
            <LogOut className="h-4 w-4 shrink-0" aria-hidden="true" />
            {railExpanded && <span className="text-sm font-medium">Sign out</span>}
          </a>
          <button
            type="button"
            onClick={() => setRailExpanded(!railExpanded)}
            aria-expanded={railExpanded}
            aria-label={railExpanded ? "Collapse zone rail" : "Expand zone rail"}
            title={railExpanded ? "Collapse" : "Expand"}
            className={`flex h-10 items-center rounded-lg text-sidebar-foreground/70 transition-colors hover:bg-sidebar-accent/50 hover:text-sidebar-foreground ${
              railExpanded ? "w-full gap-3 px-3" : "w-10 justify-center"
            }`}
          >
            {railExpanded ? (
              <>
                <ChevronsLeft className="h-4 w-4 shrink-0" aria-hidden="true" />
                <span className="text-sm font-medium">Collapse</span>
              </>
            ) : (
              <ChevronsRight className="h-4 w-4" aria-hidden="true" />
            )}
          </button>
        </div>
      </aside>

      {/* Page panel: open or hidden */}
      <aside
        className={`flex shrink-0 flex-col overflow-hidden bg-sidebar transition-[width] duration-200 ease-out motion-reduce:transition-none ${
          panelOpen ? "w-56" : "w-0"
        }`}
        aria-hidden={!panelOpen}
      >
        <div className="flex h-16 w-56 items-center justify-between pl-5 pr-3">
          <h2 className="text-sm font-semibold text-sidebar-foreground">{zone.label}</h2>
          <button
            type="button"
            onClick={() => setPanelOpen(false)}
            aria-label="Collapse page panel"
            title="Collapse panel"
            tabIndex={panelOpen ? 0 : -1}
            className="flex h-8 w-8 items-center justify-center rounded-lg text-sidebar-foreground/70 transition-colors hover:bg-sidebar-accent/50 hover:text-sidebar-foreground"
          >
            <PanelLeftClose className="h-4 w-4" aria-hidden="true" />
          </button>
        </div>
        <div className="hms-sidebar-border border-t" />
        <nav aria-label={zone.label} className="flex w-56 min-h-0 flex-1 flex-col gap-1 overflow-y-auto px-3 py-4">
          {zone.pages.map((pageLink) => {
            const isActive = active === pageLink.href;
            return (
              <a
                key={pageLink.href}
                href={pageLink.href}
                aria-current={isActive ? "page" : undefined}
                tabIndex={panelOpen ? 0 : -1}
                className={`flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors ${
                  isActive
                    ? "bg-sidebar-accent text-sidebar-foreground"
                    : "text-sidebar-foreground/70 hover:bg-sidebar-accent/50 hover:text-sidebar-foreground"
                }`}
              >
                <pageLink.icon className="h-4 w-4 shrink-0" aria-hidden="true" />
                {pageLink.label}
              </a>
            );
          })}
        </nav>
      </aside>

      {/* Content */}
      <div className="flex min-w-0 flex-1 flex-col overflow-y-auto">
        <header className="sticky top-0 z-30 border-b bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/60">
          <div className="flex h-16 items-center justify-between px-6">
            <div className="flex items-center gap-2">
              {!panelOpen && (
                <button
                  type="button"
                  onClick={() => setPanelOpen(true)}
                  aria-label="Expand page panel"
                  title="Expand panel"
                  className="mr-1 flex h-8 w-8 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                >
                  <PanelLeftOpen className="h-4 w-4" aria-hidden="true" />
                </button>
              )}
              <h1 className="text-xl font-semibold text-foreground">
                {activePage?.label ?? zone.label}
              </h1>
            </div>
            <a
              href="/logout"
              className="text-sm text-muted-foreground underline-offset-4 transition-colors hover:text-foreground hover:underline"
            >
              Sign out
            </a>
          </div>
        </header>
        <main className="flex-1 p-6">{children}</main>
      </div>
    </div>
  );
}
