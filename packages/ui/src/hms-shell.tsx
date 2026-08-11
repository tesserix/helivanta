"use client";

import { useEffect, useState, type ReactNode } from "react";
import { LogOut, PanelLeftClose, PanelLeftOpen } from "lucide-react";
import { usePermissions } from "@hms/api";
import { visibleZones, activeZone } from "./zones";
import { ThemeToggle } from "./theme";

// Two-rail chrome in the tesserix-home AdminSidebar style. The zone rail is
// fixed icons-only (no expand/collapse); only the page panel toggles open ↔
// hidden, and that preference persists per browser. All links stay plain
// <a> — cross-zone navigation is a hard navigation by design (phase 1 spec
// D3).

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

/**
 * `tenantPicker` is a slot in the zone rail rather than a component this
 * package owns. Switching hospitals means re-minting the session — a
 * Firebase custom-token exchange plus a POST to the shell's session route
 * — and both the client config and that route belong to the shell app
 * (`apps/shell/components/tenant-picker.tsx`). Keeping the control here
 * would either drag app-specific auth config into `@hms/ui` or render a
 * switcher that cannot finish the switch, which is the exact failure this
 * slot exists to prevent. Zone apps pass nothing and show no switcher;
 * their users switch from the dashboard.
 */
export function HmsShell({
  active,
  tenantPicker,
  children,
}: {
  active: string;
  tenantPicker?: ReactNode;
  children: ReactNode;
}) {
  const { can } = usePermissions();
  const zones = visibleZones(can);
  // Fall back to the dashboard (always present in `zones`) if the active
  // zone was filtered out — e.g. while permissions are still loading.
  const zone = zones.find((z) => z.key === activeZone(active).key) ?? zones[0];
  const [panelOpen, setPanelOpen] = usePersistedFlag(PANEL_KEY, true);

  const activePage = zone.pages.find((p) => p.href === active);

  return (
    <div className="flex h-screen overflow-hidden">
      {/* Zone rail: fixed icons-only. Matches tesserix admin: one step
          darker than the page panel, faint border between the rails, none
          against the content. */}
      <aside className="hms-sidebar-border flex w-16 shrink-0 flex-col border-r bg-(--sidebar-rail)">
        <div className="flex h-16 items-center justify-center">
          <a
            href="/"
            aria-label="HMS home"
            className="flex h-9 items-center gap-2 rounded-lg text-lg font-semibold text-sidebar-primary"
          >
            <span className="flex h-9 w-9 items-center justify-center rounded-lg bg-linear-to-br from-(--hms-accent) to-(--hms-accent-strong) text-(--sidebar-primary-foreground)">
              H
            </span>
          </a>
        </div>
        <div className="hms-sidebar-border mx-2 border-t" />
        <nav
          aria-label="Zones"
          className="flex min-h-0 flex-1 flex-col items-center gap-1 overflow-y-auto px-0 py-4"
        >
          {zones.map((z) => {
            const isActive = z.key === zone.key;
            return (
              <a
                key={z.key}
                href={z.href}
                title={z.label}
                aria-label={z.label}
                aria-current={isActive ? "page" : undefined}
                className={`relative flex h-10 w-10 items-center justify-center rounded-lg transition-colors ${
                  isActive
                    ? "bg-(--hms-accent-dim) text-(--hms-accent)"
                    : "text-sidebar-foreground/70 hover:bg-sidebar-accent/50 hover:text-sidebar-foreground"
                }`}
              >
                {isActive && (
                  <span
                    aria-hidden="true"
                    className="absolute inset-y-1 left-0 w-[3px] rounded-full bg-(--hms-accent)"
                  />
                )}
                <z.icon className="h-5 w-5 shrink-0" aria-hidden="true" />
              </a>
            );
          })}
        </nav>
        <div className="flex flex-col items-center gap-1 px-0 py-3">
          {tenantPicker}
          <a
            href="/logout"
            title="Sign out"
            aria-label="Sign out"
            className="flex h-10 w-10 items-center justify-center rounded-lg text-sidebar-foreground/70 transition-colors hover:bg-sidebar-accent/50 hover:text-sidebar-foreground"
          >
            <LogOut className="h-4 w-4 shrink-0" aria-hidden="true" />
          </a>
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
        <nav
          aria-label={zone.label}
          className="flex w-56 min-h-0 flex-1 flex-col gap-1 overflow-y-auto px-3 py-4"
        >
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
            <div className="flex items-center gap-3">
              <ThemeToggle />
              <a
                href="/logout"
                className="text-sm text-muted-foreground underline-offset-4 transition-colors hover:text-foreground hover:underline"
              >
                Sign out
              </a>
            </div>
          </div>
        </header>
        <main className="flex-1 px-6 py-6">
          <div className="mx-auto w-full max-w-6xl">{children}</div>
        </main>
      </div>
    </div>
  );
}
