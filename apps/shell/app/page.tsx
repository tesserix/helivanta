import { HmsShell, ZONES } from "@hms/ui";

// Per-page descriptions, keyed by href — the registry (packages/ui/src/zones.ts)
// owns titles and hrefs, this map only supplies the dashboard-card copy.
const DESCRIPTIONS: Record<string, string> = {
  "/medicore/opd": "Outpatient registration & appointments",
  "/medicore/ipd": "Admissions, beds & ward rounds",
  "/pharmacy": "Dispensing & drug inventory",
  "/lab": "Orders, samples & results",
};

const cards = ZONES.filter((zone) => zone.key !== "dashboard").flatMap((zone) =>
  zone.pages.length > 1
    ? zone.pages.map((page) => ({ href: page.href, title: page.label, desc: DESCRIPTIONS[page.href] ?? "" }))
    : [{ href: zone.href, title: zone.label, desc: DESCRIPTIONS[zone.href] ?? "" }],
);

export default function Dashboard() {
  return (
    <HmsShell active="/">
      <h1 className="mb-6 text-2xl font-semibold">Departments</h1>
      <div className="grid max-w-3xl gap-4 sm:grid-cols-2">
        {cards.map((z) => (
          <a key={z.href} href={z.href} className="rounded-lg border p-4 hover:bg-accent">
            <div className="font-medium">{z.title}</div>
            <div className="text-sm text-muted-foreground">{z.desc}</div>
          </a>
        ))}
      </div>
    </HmsShell>
  );
}
