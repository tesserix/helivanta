import { FlaskConical, HeartPulse, LayoutDashboard, Pill, type LucideIcon } from "lucide-react";

export type ZonePage = { label: string; href: string };

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
    pages: [{ label: "Departments", href: "/" }],
  },
  {
    key: "medicore",
    label: "MediCore",
    icon: HeartPulse,
    href: "/medicore/opd",
    pages: [
      { label: "OPD", href: "/medicore/opd" },
      { label: "IPD", href: "/medicore/ipd" },
    ],
  },
  {
    key: "pharmacy",
    label: "Pharmacy",
    icon: Pill,
    href: "/pharmacy",
    pages: [
      { label: "Dispenses", href: "/pharmacy" },
      { label: "Medications", href: "/pharmacy/medications" },
    ],
  },
  {
    key: "lab",
    label: "Lab",
    icon: FlaskConical,
    href: "/lab",
    pages: [{ label: "Orders", href: "/lab" }],
  },
];

export function activeZone(path: string): Zone {
  if (path.startsWith("/medicore")) return ZONES[1];
  if (path.startsWith("/pharmacy")) return ZONES[2];
  if (path.startsWith("/lab")) return ZONES[3];
  return ZONES[0];
}
