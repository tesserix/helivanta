"use client";

import { usePermissions } from "@helivanta/api";
import { PanelLink, visibleZones, type Zone, type ZoneHue } from "@helivanta/ui";
import { OperationalDashboard } from "@/components/dashboard/operational-dashboard";

// Per-page descriptions, keyed by href — the registry (packages/ui/src/zones.ts)
// owns titles and hrefs, this map only supplies the dashboard-card copy.
const DESCRIPTIONS: Record<string, string> = {
  "/patients": "Hospital-scoped registration, patient search and consent receipts",
  "/workforce": "Hospital teams, staff profiles and employment status",
  "/settings/branding": "Your logo, medical theme and sign-in experience",
  "/medicore/opd": "Outpatient visit registrations",
  "/medicore/ipd": "Inpatient visit registrations",
  "pharmacy:Dispensing": "Dispense queue and medication list",
  "pharmacy:Inventory": "Stock on hand, receiving, expiry and the product catalogue",
  "pharmacy:Purchasing": "Supplier bills, payments and outstanding balances",
  "pharmacy:Setup": "Pharmacy outlets and who can access them",
  "/supply/parties": "Organisations, suppliers, premises and independent licence review",
  "/lab": "Orders, samples & results",
};

// Static class map keyed by hue, full class strings so Tailwind's scanner
// can see them (string interpolation would hide the classes at build time).
const TINT_CLASSES: Record<ZoneHue, string> = {
  rose: "bg-(--hms-rose-tint) text-(--hms-rose)",
  amber: "bg-(--hms-amber-tint) text-(--hms-amber)",
  violet: "bg-(--hms-violet-tint) text-(--hms-violet)",
};

// Cards must show only doors that will open (zones.ts's own contract) —
// the same visibleZones(can) filter the sidebar (hms-shell.tsx) uses, not
// a second copy of the zone list. usePermissions().can() denies while
// permissions are loading, so cards never flash before they resolve —
// same behaviour the sidebar already relies on.
function cardsFor(zones: Zone[]) {
  return zones
    .filter((zone) => zone.key !== "dashboard")
    .flatMap((zone) =>
      zone.pages.some((page) => page.section)
        ? // A sectioned zone gets one card per section (#1258), opening its
          // first permitted page, so eight pharmacy pages are four doors.
          zone.pages
            .filter((page, index, pages) => page.section !== pages[index - 1]?.section)
            .map((page) => ({
              href: page.href,
              title: page.section ?? page.label,
              desc: DESCRIPTIONS[`${zone.key}:${page.section}`] ?? "",
              icon: zone.icon,
              hue: zone.hue,
            }))
        : zone.pages.length > 1
        ? zone.pages.map((page) => ({
            href: page.href,
            title: page.label,
            desc: DESCRIPTIONS[page.href] ?? "",
            icon: zone.icon,
            hue: zone.hue,
          }))
        : [
            {
              href: zone.href,
              title: zone.label,
              desc: DESCRIPTIONS[zone.href] ?? "",
              icon: zone.icon,
              hue: zone.hue,
            },
          ],
    );
}

export default function Dashboard() {
  const { can } = usePermissions();
  const cards = cardsFor(visibleZones(can));

  return (
    <>
      <OperationalDashboard />
      <div className="mb-6 flex flex-col gap-1 sm:flex-row sm:items-baseline sm:justify-between">
        <h2 className="text-sm font-semibold text-muted-foreground">Jump into a department</h2>
        <p className="text-xs text-muted-foreground">Manage visits, dispensing and lab work.</p>
      </div>
      <nav aria-label="Department shortcuts" className="grid gap-4 sm:grid-cols-2">
        {cards.map((z) => (
          <PanelLink key={z.href} href={z.href} className="group flex items-start gap-4">
            <span
              className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-lg ${TINT_CLASSES[z.hue]}`}
            >
              <z.icon className="h-5 w-5" aria-hidden="true" />
            </span>
            <div className="min-w-0 flex-1">
              <div className="font-medium text-foreground">{z.title}</div>
              <div className="mt-0.5 text-sm text-muted-foreground">{z.desc}</div>
            </div>
            <span
              className="text-muted-foreground transition-colors group-hover:text-(--hms-accent)"
              aria-hidden="true"
            >
              →
            </span>
          </PanelLink>
        ))}
      </nav>
    </>
  );
}
