import { homeWidth } from "@/app/page-width";
import { sectionHeading } from "./public-type";

// Placeholders, not claims: they live on the roadmap, where "placeholder" is
// the honest state (#1837 D4, D6).
const trustItems = [
  { label: "SOC 2 Type II", kind: "Certification placeholder", state: "Not yet certified" },
  { label: "ISO/IEC 27001", kind: "Certification placeholder", state: "Not yet certified" },
  { label: "HIPAA safeguards", kind: "Compliance documentation", state: "Documentation pending" },
];

export function TrustSection() {
  return (
    <section
      id="trust"
      aria-labelledby="trust-heading"
      className={`${homeWidth} scroll-mt-6 py-24`}
    >
      <h2 id="trust-heading" className={`${sectionHeading} max-w-2xl`}>
        Care deserves a clear standard.
      </h2>
      <p className="mt-5 max-w-2xl leading-relaxed text-muted-foreground">
        Our trust centre will make independent assurance and supporting documentation easy to find.
        The items below are placeholders, not claims of certification or compliance.
      </p>
      <div className="mt-10 grid gap-5 md:grid-cols-3">
        {trustItems.map((item) => (
          <article key={item.label} className="rounded-lg border border-dashed border-border p-7">
            <p className="text-[13px] text-muted-foreground">{item.kind}</p>
            <h3 className="mt-4 text-xl font-semibold">{item.label}</h3>
            <p className="mt-5 inline-block rounded-md border border-border px-3 py-1 text-[13px] text-muted-foreground">
              {item.state}
            </p>
          </article>
        ))}
      </div>
    </section>
  );
}
