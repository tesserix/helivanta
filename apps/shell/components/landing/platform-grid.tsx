import Link from "next/link";
import { availableGroups, platformGroups, type CatalogueGroup } from "@/app/platform-catalog";
import { homeWidth } from "@/app/page-width";
import { ModuleStatusLabel } from "./module-status";
import { sectionHeading } from "./public-type";
import { StatusGuide } from "./term";

// The platform section. The home page lists what is available today and links
// to the roadmap, which lists every module with its status (#1837 D3).
export function PlatformGrid({ scope }: { scope: "available" | "all" }) {
  return (
    <section
      id="platform"
      aria-labelledby="platform-heading"
      className="scroll-mt-6 border-t border-border bg-muted py-24"
    >
      <div className={homeWidth}>
        <h2 id="platform-heading" className={sectionHeading}>
          Different responsibilities.
          <br />A familiar place to work.
        </h2>
        {scope === "all" ? (
          <p className="mt-5 max-w-3xl leading-relaxed text-muted-foreground">
            Explore the full platform and its roadmap. Availability varies by module: foundations
            cover the workflows described below, while planned and future capabilities are not yet
            available. Access depends on your organisation and permissions. <StatusGuide />
          </p>
        ) : (
          <p className="mt-5 max-w-3xl leading-relaxed text-muted-foreground">
            <Link href="/roadmap" className="underline underline-offset-4">
              The full platform and its status, module by module.
            </Link>
          </p>
        )}
        <div className="mt-12">
          {(scope === "all" ? (platformGroups as readonly CatalogueGroup[]) : availableGroups).map(
            (group) => (
              <div
                key={group.name}
                className="grid gap-8 border-t border-border py-10 lg:grid-cols-[17rem_1fr]"
              >
                <div>
                  <h3 className="text-xl font-semibold font-stretch-[85%]">{group.name}</h3>
                  <p className="mt-2 text-base leading-relaxed text-muted-foreground">
                    {group.description}
                  </p>
                </div>
                <div className="grid gap-x-10 gap-y-8 sm:grid-cols-2">
                  {group.modules.map((module) => (
                    <article key={module.name} aria-label={module.name}>
                      <div className="flex items-start justify-between gap-4">
                        <h4 className="font-semibold">{module.name}</h4>
                        <ModuleStatusLabel status={module.status} />
                      </div>
                      <p className="mt-1 text-[13px] text-muted-foreground">{module.product}</p>
                      <p className="mt-3 text-base leading-relaxed text-muted-foreground">
                        {module.description}
                      </p>
                    </article>
                  ))}
                </div>
              </div>
            ),
          )}
        </div>
      </div>
    </section>
  );
}
