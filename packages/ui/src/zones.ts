import {
  BedDouble,
  ClipboardList,
  FlaskConical,
  HeartPulse,
  LayoutDashboard,
  Pill,
  Stethoscope,
  type LucideIcon,
} from "lucide-react";

export type ZonePage = { label: string; href: string; icon: LucideIcon; permission: string };

export type Zone = {
  key: string;
  label: string;
  icon: LucideIcon;
  href: string;
  permission: string;
  pages: ZonePage[];
};

// Single source of zone navigation. Adding a zone is one entry here.
// All hrefs are absolute paths; every link renders as a plain <a> so
// cross-zone navigation is a hard navigation (phase 1 spec D3).
export const ZONES: Zone[] = [
  {
    key: "dashboard",
    label: "Dashboard",
    icon: LayoutDashboard,
    href: "/",
    permission: "public",
    pages: [{ label: "Departments", href: "/", icon: LayoutDashboard, permission: "public" }],
  },
  {
    key: "medicore",
    label: "MediCore",
    icon: HeartPulse,
    href: "/medicore/opd",
    permission: "medicore.visit.read",
    pages: [
      { label: "OPD", href: "/medicore/opd", icon: Stethoscope, permission: "medicore.visit.read" },
      { label: "IPD", href: "/medicore/ipd", icon: BedDouble, permission: "medicore.visit.read" },
    ],
  },
  {
    key: "pharmacy",
    label: "Pharmacy",
    icon: Pill,
    href: "/pharmacy",
    permission: "pharmacy.dispense.read",
    pages: [
      { label: "Dispenses", href: "/pharmacy", icon: Pill, permission: "pharmacy.dispense.read" },
      {
        label: "Medications",
        href: "/pharmacy/medications",
        icon: ClipboardList,
        permission: "pharmacy.medication.read",
      },
    ],
  },
  {
    key: "lab",
    label: "Lab",
    icon: FlaskConical,
    href: "/lab",
    permission: "lab.order.read",
    pages: [{ label: "Orders", href: "/lab", icon: FlaskConical, permission: "lab.order.read" }],
  },
];

// Navigation is a convenience layer: the API enforces permissions, this
// only avoids showing doors that will not open. `can("public")` resolves
// to true (packages/api/src/permissions.tsx mirrors the Go backend's
// authz.Public sentinel), so the dashboard's "public" permission keeps it
// visible without a dashboard-specific special case here. A zone whose
// pages are all filtered out is dropped too, rather than showing as an
// empty entry in the nav.
export function visibleZones(can: (permission: string) => boolean, zones: Zone[] = ZONES): Zone[] {
  return zones
    .map((zone) => ({
      ...zone,
      pages: zone.pages.filter((page) => can(page.permission)),
    }))
    .filter((zone) => can(zone.permission) && zone.pages.length > 0);
}

export function activeZone(path: string): Zone {
  const match = ZONES.find(
    (z) =>
      z.key !== "dashboard" &&
      (path === "/" + z.key || path.startsWith("/" + z.key + "/") || path.startsWith("/" + z.key)),
  );
  return match ?? ZONES[0];
}
