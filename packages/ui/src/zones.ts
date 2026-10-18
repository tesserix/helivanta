import {
  Building2,
  Ambulance,
  BedDouble,
  CalendarDays,
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
    key: "emergency",
    label: "EmergencyConnect",
    icon: Ambulance,
    href: "/emergency",
    permission: "emergency.base.read",
    hue: "rose",
    pages: [
      {
        label: "Operating bases & reports",
        href: "/emergency",
        icon: Ambulance,
        permission: "emergency.base.read",
      },
    ],
  },
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
      { label: "Rosters", href: "/workforce/rosters", icon: CalendarDays, permission: "public" },
      {
        label: "Leave setup",
        href: "/workforce/leave-setup",
        icon: Settings,
        permission: "public",
      },
      {
        label: "Leave balances",
        href: "/workforce/leave-balances",
        icon: CalendarDays,
        permission: "public",
      },
      {
        label: "Leave requests",
        href: "/workforce/leave-requests",
        icon: CalendarDays,
        permission: "public",
      },
      {
        label: "Leave reports",
        href: "/workforce/leave-reports",
        icon: CalendarDays,
        permission: "public",
      },
      {
        label: "Duty exchanges",
        href: "/workforce/duty-exchanges",
        icon: CalendarDays,
        permission: "public",
      },
      {
        label: "Overtime",
        href: "/workforce/overtime",
        icon: CalendarDays,
        permission: "public",
      },
      {
        label: "Attendance",
        href: "/workforce/attendance",
        icon: CalendarDays,
        permission: "public",
      },
      {
        label: "Credential register",
        href: "/workforce/credentials",
        icon: ClipboardList,
        permission: "workforce.enrollment.manage",
      },
      {
        label: "Credential reminders",
        href: "/workforce/credential-reminders",
        icon: ClipboardList,
        permission: "workforce.enrollment.manage",
      },
      {
        label: "Payroll setup",
        href: "/workforce/payroll-setup",
        icon: Settings,
        permission: "workforce.payroll.view",
      },
    ],
  },
  {
    key: "settings",
    label: "Settings",
    icon: Settings,
    href: "/settings/branding",
    permission: "public",
    hue: "violet",
    pages: [
      {
        label: "Country policies",
        href: "/settings/country-policies",
        icon: Settings,
        permission: "workspace.country_policy.read",
      },
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
    label: "PharmaConnect",
    icon: Pill,
    href: "/pharmacy",
    permission: "public",
    hue: "amber",
    pages: [
      { label: "Dispenses", href: "/pharmacy", icon: Pill, permission: "pharmacy.dispense.read" },
      {
        label: "Product catalogue",
        href: "/pharmacy/catalogue",
        icon: ClipboardList,
        permission: "pharmacy.catalogue.read",
      },
      {
        label: "Stock receiving",
        href: "/pharmacy/stock",
        icon: Pill,
        permission: "pharmacy.stock.read",
      },
      {
        label: "Expenses & audit",
        href: "/pharmacy/accounts",
        icon: ClipboardList,
        permission: "pharmacy.accounts.read",
      },
      {
        label: "Medications",
        href: "/pharmacy/medications",
        icon: ClipboardList,
        permission: "pharmacy.medication.read",
      },
    ],
  },
  {
    key: "supply",
    label: "Supply Connect",
    icon: Building2,
    href: "/supply/parties",
    permission: "supply.directory.read",
    hue: "amber",
    pages: [
      {
        label: "Organisations & suppliers",
        href: "/supply/parties",
        icon: Building2,
        permission: "supply.directory.read",
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
    .map((zone) => {
      const pages = zone.pages.filter((page) => can(page.permission));
      return {
        ...zone,
        pages,
        href: pages.some((page) => page.href === zone.href)
          ? zone.href
          : (pages[0]?.href ?? zone.href),
      };
    })
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
