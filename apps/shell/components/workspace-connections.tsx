"use client";

import Link from "next/link";
import { ArrowUpRight, LockKeyhole } from "lucide-react";
import { usePermissions } from "@helivanta/api";
import { visibleZones, ZONES } from "@helivanta/ui";

export function WorkspaceConnections({ current }: { current: "supply" | "pharmacy" }) {
  const { can, isLoading } = usePermissions();
  const key = current === "supply" ? "pharmacy" : "supply";
  const workspace = ZONES.find((zone) => zone.key === key)!;
  const available = visibleZones(can).find((zone) => zone.key === key);

  return (
    <section aria-label="Connected workspace" className="rounded-2xl border bg-card p-5 sm:p-6">
      <div className="flex items-start gap-3">
        <span className="flex size-10 shrink-0 items-center justify-center rounded-xl bg-(--hms-accent-dim) text-(--hms-accent)">
          <workspace.icon className="size-5" aria-hidden="true" />
        </span>
        <div className="min-w-0">
          <p className="text-xs font-medium text-muted-foreground">Connected workspace</p>
          <h3 className="mt-1 font-semibold">{workspace.label}</h3>
          <p className="mt-2 text-sm leading-6 text-muted-foreground">
            {current === "supply"
              ? "Manage medicines, outlet inventory, dispensing, expenses and payment records in PharmaConnect."
              : "Manage supplier organisations, premises, licences and supporting evidence in Supply Connect."}
          </p>
        </div>
      </div>
      {isLoading ? (
        <p role="status" className="mt-4 text-sm text-muted-foreground">
          Checking workspace access…
        </p>
      ) : available ? (
        <nav
          aria-label={`${workspace.label} workflows`}
          className="mt-5 grid gap-2 sm:grid-cols-2 xl:grid-cols-3"
        >
          {available.pages.map((page) => (
            <Link
              key={page.href}
              href={page.href}
              className="flex min-w-0 items-center gap-3 rounded-xl border px-4 py-3 text-sm font-medium transition-colors hover:bg-muted/50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-(--hms-accent)"
            >
              <page.icon className="size-4 shrink-0 text-(--hms-accent)" aria-hidden="true" />
              <span className="min-w-0 flex-1">{page.label}</span>
              <ArrowUpRight className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
            </Link>
          ))}
        </nav>
      ) : (
        <p className="mt-4 flex items-start gap-2 rounded-xl bg-muted/50 p-4 text-sm leading-6 text-muted-foreground">
          <LockKeyhole className="mt-1 size-4 shrink-0" aria-hidden="true" />
          {current === "supply"
            ? "This account does not have pharmacy permissions. Ask your tenant administrator for the appropriate pharmacy role and access to your outlet, or sign in with an authorised pharmacy account."
            : "This account does not have supplier-directory permissions. Ask your tenant administrator for the appropriate Supply Connect role."}
        </p>
      )}
      <p className="mt-4 text-xs leading-5 text-muted-foreground">
        Supplier verification and pharmacy outlet access are separate. A hospital affiliation does
        not share private records.
      </p>
    </section>
  );
}
