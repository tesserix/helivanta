import { HmsShell } from "@hms/ui";

const ZONES = [
  { href: "/medicore/opd", title: "OPD", desc: "Outpatient registration & appointments" },
  { href: "/medicore/ipd", title: "IPD", desc: "Admissions, beds & ward rounds" },
  { href: "/pharmacy", title: "Pharmacy", desc: "Dispensing & drug inventory" },
  { href: "/lab", title: "Lab", desc: "Orders, samples & results" },
];

export default function Dashboard() {
  return (
    <HmsShell active="/">
      <h1 className="mb-6 text-2xl font-semibold">Departments</h1>
      <div className="grid max-w-3xl gap-4 sm:grid-cols-2">
        {ZONES.map((z) => (
          <a key={z.href} href={z.href} className="rounded-lg border p-4 hover:bg-accent">
            <div className="font-medium">{z.title}</div>
            <div className="text-sm text-muted-foreground">{z.desc}</div>
          </a>
        ))}
      </div>
    </HmsShell>
  );
}
