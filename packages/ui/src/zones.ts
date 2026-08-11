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
// only avoids showing doors that will not open. The dashboard always
// stays so a user with no permissions still lands somewhere coherent.
export function visibleZones(can: (permission: string) => boolean): Zone[] {
  return ZONES.filter((zone) => zone.key === "dashboard" || can(zone.permission)).map((zone) => ({
    ...zone,
    pages: zone.pages.filter((page) => page.permission === "public" || can(page.permission)),
  }));
}

export function activeZone(path: string): Zone {
  const match = ZONES.find(
    (z) =>
      z.key !== "dashboard" &&
      (path === "/" + z.key || path.startsWith("/" + z.key + "/") || path.startsWith("/" + z.key)),
  );
  return match ?? ZONES[0];
}
