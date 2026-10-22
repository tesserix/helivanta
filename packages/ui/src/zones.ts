import {
  Ambulance,
  ArrowLeftRight,
  BadgeCheck,
  BedDouble,
  BellRing,
  BookOpen,
  Boxes,
  Building,
  Building2,
  CalendarCog,
  CalendarDays,
  CalendarX,
  Clock,
  Contact,
  FileText,
  Fingerprint,
  FlaskConical,
  HeartHandshake,
  HeartPulse,
  Hospital,
  Hourglass,
  IdCard,
  Layers,
  LayoutDashboard,
  ListChecks,
  PackagePlus,
  Palette,
  Pill,
  ReceiptText,
  Scale,
  Settings,
  ShieldAlert,
  SlidersHorizontal,
  Stethoscope,
  Store,
  Tablets,
  Timer,
  Users,
  Wallet,
  type LucideIcon,
} from "lucide-react";

export type ZonePage = {
  label: string;
  href: string;
  icon: LucideIcon;
  permission: string;
  /** Heading the page panel shows before the first page of a run (#1258 D1). */
  section?: string;
};

export type ZoneHue = "rose" | "amber" | "violet";

// The rail groups zones by use (#1270 D1); `admin` sits at the foot of the rail.
export type ZoneGroup = "overview" | "care" | "operations" | "admin";

export type Zone = {
  key: string;
  label: string;
  icon: LucideIcon;
  href: string;
  permission: string;
  hue: ZoneHue;
  group: ZoneGroup;
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
    group: "overview",
    pages: [{ label: "Overview", href: "/dashboard", icon: LayoutDashboard, permission: "public" }],
  },
  {
    key: "patients",
    label: "Patients",
    icon: Users,
    href: "/patients",
    permission: "patient.read",
    hue: "rose",
    group: "care",
    pages: [
      { label: "Patient registry", href: "/patients", icon: Users, permission: "patient.read" },
    ],
  },
  {
    key: "medicore",
    label: "MediCore",
    icon: HeartPulse,
    href: "/medicore/opd",
    permission: "medicore.visit.read",
    hue: "rose",
    group: "care",
    pages: [
      { label: "OPD", href: "/medicore/opd", icon: Stethoscope, permission: "medicore.visit.read" },
      { label: "IPD", href: "/medicore/ipd", icon: BedDouble, permission: "medicore.visit.read" },
    ],
  },
  {
    key: "lab",
    label: "Lab",
    icon: FlaskConical,
    href: "/lab",
    permission: "lab.order.read",
    hue: "violet",
    group: "care",
    pages: [{ label: "Orders", href: "/lab", icon: FlaskConical, permission: "lab.order.read" }],
  },
  {
    key: "pharmacy",
    label: "PharmaConnect",
    icon: Pill,
    href: "/pharmacy",
    permission: "public",
    hue: "amber",
    group: "care",
    pages: [
      {
        section: "Dispensing",
        label: "Dispense queue",
        href: "/pharmacy",
        icon: ListChecks,
        permission: "pharmacy.dispense.read",
      },
      {
        section: "Dispensing",
        label: "Medication list",
        href: "/pharmacy/medications",
        icon: Tablets,
        permission: "pharmacy.medication.read",
      },
      {
        section: "Inventory",
        label: "Stock on hand",
        href: "/pharmacy/stock",
        icon: Boxes,
        permission: "pharmacy.stock.read",
      },
      {
        section: "Inventory",
        label: "Receiving",
        href: "/pharmacy/receiving",
        icon: PackagePlus,
        permission: "pharmacy.stock.read",
      },
      {
        section: "Inventory",
        label: "Expiry & safety",
        href: "/pharmacy/safety",
        icon: ShieldAlert,
        permission: "pharmacy.stock.read",
      },
      {
        section: "Inventory",
        label: "Product catalogue",
        href: "/pharmacy/catalogue",
        icon: BookOpen,
        permission: "pharmacy.catalogue.read",
      },
      {
        section: "Purchasing",
        label: "Supplier bills",
        href: "/pharmacy/accounts",
        icon: ReceiptText,
        permission: "pharmacy.accounts.read",
      },
      {
        section: "Setup",
        label: "Outlets & access",
        href: "/pharmacy/setup",
        icon: Store,
        permission: "pharmacy.access.manage",
      },
    ],
  },
  {
    key: "emergency",
    label: "EmergencyConnect",
    icon: Ambulance,
    href: "/emergency",
    permission: "public",
    hue: "rose",
    group: "care",
    pages: [
      {
        label: "Operating bases & reports",
        href: "/emergency",
        icon: Building,
        permission: "emergency.base.read",
      },
      {
        label: "My crew assignments",
        href: "/emergency/crew",
        icon: Ambulance,
        permission: "emergency.crew.read",
      },
      {
        label: "Hospital receiving",
        href: "/emergency/receiving",
        icon: Hospital,
        permission: "emergency.receiving.read",
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
    group: "operations",
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
    key: "workforce",
    label: "Staff",
    icon: IdCard,
    href: "/workforce",
    permission: "public",
    hue: "rose",
    group: "operations",
    pages: [
      { label: "Staff directory", href: "/workforce", icon: Contact, permission: "public" },
      {
        label: "HR setup",
        href: "/workforce/setup",
        icon: SlidersHorizontal,
        permission: "public",
      },
      { label: "Shifts", href: "/workforce/shifts", icon: Clock, permission: "public" },
      {
        label: "Roster setup",
        href: "/workforce/roster-setup",
        icon: Layers,
        permission: "public",
      },
      {
        label: "Roster policy",
        href: "/workforce/roster-policy",
        icon: Scale,
        permission: "public",
      },
      { label: "Rosters", href: "/workforce/rosters", icon: CalendarDays, permission: "public" },
      {
        label: "Leave setup",
        href: "/workforce/leave-setup",
        icon: CalendarCog,
        permission: "public",
      },
      {
        label: "Leave balances",
        href: "/workforce/leave-balances",
        icon: Hourglass,
        permission: "public",
      },
      {
        label: "Leave requests",
        href: "/workforce/leave-requests",
        icon: CalendarX,
        permission: "public",
      },
      {
        label: "Leave reports",
        href: "/workforce/leave-reports",
        icon: FileText,
        permission: "public",
      },
      {
        label: "Duty exchanges",
        href: "/workforce/duty-exchanges",
        icon: ArrowLeftRight,
        permission: "public",
      },
      {
        label: "Overtime",
        href: "/workforce/overtime",
        icon: Timer,
        permission: "public",
      },
      {
        label: "Attendance",
        href: "/workforce/attendance",
        icon: Fingerprint,
        permission: "public",
      },
      {
        label: "Credential register",
        href: "/workforce/credentials",
        icon: BadgeCheck,
        permission: "workforce.enrollment.manage",
      },
      {
        label: "Credential reminders",
        href: "/workforce/credential-reminders",
        icon: BellRing,
        permission: "workforce.enrollment.manage",
      },
      {
        label: "Payroll setup",
        href: "/workforce/payroll-setup",
        icon: Wallet,
        permission: "workforce.payroll.view",
      },
      {
        label: "Benefits",
        href: "/workforce/benefits",
        icon: HeartHandshake,
        permission: "workforce.benefits.view",
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
    group: "admin",
    pages: [
      {
        label: "Country policies",
        href: "/settings/country-policies",
        icon: Scale,
        permission: "workspace.country_policy.read",
      },
      {
        label: "Branding",
        href: "/settings/branding",
        icon: Palette,
        permission: "workspace.branding.manage",
      },
    ],
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

// The longest page href that `path` equals or sits under. No match marks no
// page current: aria-current on the wrong link misleads a screen reader.
export function activePage(zone: Zone, path: string): ZonePage | undefined {
  return zone.pages
    .filter((page) => path === page.href || path.startsWith(page.href + "/"))
    .reduce<ZonePage | undefined>(
      (best, page) => (best && best.href.length >= page.href.length ? best : page),
      undefined,
    );
}
