import { HmsShell, ZONES } from "@hms/ui";

// Per-page descriptions, keyed by href — the registry (packages/ui/src/zones.ts)
// owns titles and hrefs, this map only supplies the dashboard-card copy.
const DESCRIPTIONS: Record<string, string> = {
  "/medicore/opd": "Outpatient registration & appointments",
  "/medicore/ipd": "Admissions, beds & ward rounds",
  "/pharmacy": "Dispensing & drug inventory",
  "/pharmacy/medications": "Formulary & stock reference",
  "/lab": "Orders, samples & results",
};

const cards = ZONES.filter((zone) => zone.key !== "dashboard").flatMap((zone) =>
  zone.pages.length > 1
    ? zone.pages.map((page) => ({
        href: page.href,
        title: page.label,
        desc: DESCRIPTIONS[page.href] ?? "",
        icon: zone.icon,
      }))
    : [{ href: zone.href, title: zone.label, desc: DESCRIPTIONS[zone.href] ?? "", icon: zone.icon }],
);

export default function Dashboard() {
  return (
    <HmsShell active="/">
      <div className="mx-auto max-w-4xl">
        <p className="mb-6 text-sm text-muted-foreground">
          Jump into a department to manage visits, dispensing and lab work.
        </p>
        <div className="grid gap-4 sm:grid-cols-2">
          {cards.map((z) => (
            <a
              key={z.href}
              href={z.href}
              className="group rounded-lg border bg-card p-5 transition-all hover:border-foreground/20 hover:shadow-sm focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
            >
              <div className="flex items-start gap-4">
                <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground transition-colors group-hover:bg-primary group-hover:text-primary-foreground">
                  <z.icon className="h-5 w-5" aria-hidden="true" />
                </span>
                <div className="min-w-0">
                  <div className="font-medium text-foreground">{z.title}</div>
                  <div className="mt-0.5 text-sm text-muted-foreground">{z.desc}</div>
                </div>
              </div>
            </a>
          ))}
        </div>
      </div>
    </HmsShell>
  );
}
