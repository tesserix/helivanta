import {
  BedDouble,
  Users,
  Settings,
  ClipboardList,
  FlaskConical,
  HeartPulse,
  LayoutDashboard,
  Pill,
  Stethoscope,
  type LucideIcon,
} from "lucide-react";

export type ZonePage = { label: string; href: string; icon: LucideIcon; permission: string };

export type ZoneHue = "rose" | "amber" | "violet";

export type Zone = {
  key: string;
  label: string;
  icon: LucideIcon;
  href: string;
  permission: string;
  hue: ZoneHue;
  pages: ZonePage[];
};

// Single source of zone navigation. Adding a zone is one entry here.
// All hrefs are absolute paths within the one app (apps/shell); HmsShell
// renders them with next/link, so moving between zones is client-side
// navigation (#933).
export const ZONES: Zone[] = [
  {
    key: "dashboard",
    label: "Dashboard",
    icon: LayoutDashboard,
    href: "/dashboard",
    permission: "public",
    hue: "rose",
    pages: [
      { label: "Departments", href: "/dashboard", icon: LayoutDashboard, permission: "public" },
    ],
  },
  {
    key: "patients",
    label: "Patients",
    icon: Users,
    href: "/patients",
    permission: "patient.read",
    hue: "rose",
    pages: [
      { label: "Patient registry", href: "/patients", icon: Users, permission: "patient.read" },
    ],
  },
  {
    key: "workforce",
    label: "Staff",
    icon: Users,
    href: "/workforce",
    permission: "public",
    hue: "rose",
    pages: [
      { label: "Staff directory", href: "/workforce", icon: Users, permission: "public" },
      { label: "HR setup", href: "/workforce/setup", icon: Settings, permission: "public" },
      { label: "Shifts", href: "/workforce/shifts", icon: Settings, permission: "public" },
      {
        label: "Roster setup",
        href: "/workforce/roster-setup",
        icon: Settings,
        permission: "public",
      },
      {
        label: "Roster policy",
        href: "/workforce/roster-policy",
        icon: Settings,
        permission: "public",
      },
    ],
  },
  {
    key: "settings",
    label: "Settings",
    icon: Settings,
    href: "/settings/branding",
    permission: "workspace.branding.manage",
    hue: "violet",
    pages: [
      {
        label: "Branding",
        href: "/settings/branding",
        icon: Settings,
        permission: "workspace.branding.manage",
      },
    ],
  },
  {
    key: "medicore",
    label: "MediCore",
    icon: HeartPulse,
    href: "/medicore/opd",
    permission: "medicore.visit.read",
    hue: "rose",
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
    hue: "amber",
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
    hue: "violet",
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
  return match ?? ZONES.find((zone) => zone.key === "dashboard")!;
}
