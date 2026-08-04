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

export type ZonePage = { label: string; href: string; icon: LucideIcon };

export type Zone = {
  key: string;
  label: string;
  icon: LucideIcon;
  href: string;
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
    pages: [{ label: "Departments", href: "/", icon: LayoutDashboard }],
  },
  {
    key: "medicore",
    label: "MediCore",
    icon: HeartPulse,
    href: "/medicore/opd",
    pages: [
      { label: "OPD", href: "/medicore/opd", icon: Stethoscope },
      { label: "IPD", href: "/medicore/ipd", icon: BedDouble },
    ],
  },
  {
    key: "pharmacy",
    label: "Pharmacy",
    icon: Pill,
    href: "/pharmacy",
    pages: [
      { label: "Dispenses", href: "/pharmacy", icon: Pill },
      { label: "Medications", href: "/pharmacy/medications", icon: ClipboardList },
    ],
  },
  {
    key: "lab",
    label: "Lab",
    icon: FlaskConical,
    href: "/lab",
    pages: [{ label: "Orders", href: "/lab", icon: FlaskConical }],
  },
];

export function activeZone(path: string): Zone {
  const match = ZONES.find(
    (z) =>
      z.key !== "dashboard" &&
      (path === "/" + z.key || path.startsWith("/" + z.key + "/") || path.startsWith("/" + z.key)),
  );
  return match ?? ZONES[0];
}
