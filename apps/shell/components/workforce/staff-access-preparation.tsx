"use client";
import { GuardedLink, Panel } from "@helivanta/ui";

type Props = { kind: "hospital" | "pharmacy" | "supplier"; active: boolean };
export function StaffAccessPreparation({ kind, active }: Props) {
  return (
    <Panel aria-label="Staff access preparation" className="mt-6 space-y-3">
      <h3 className="font-semibold">Prepare Staff access</h3>
      <p className="text-sm">
        {active
          ? "Staff directory record is active. Continue with the separate account and access reviews."
          : "Staff record is inactive. Save an active Staff record before proposing an account link or operating duty."}
      </p>
      <ol className="list-decimal space-y-2 pl-5 text-sm">
        <li>Confirm the Staff record and current employment details.</li>
        <li>
          Open this Staff member’s account links, propose their verified login and have another
          authorized person approve it. Creating a Staff record does not create a login.
        </li>
        {kind === "hospital" ? (
          <li>
            Complete reviewed employment, the department posting and the exact pharmacy outlet
            relationship before assigning hospital pharmacy work.
            <nav aria-label="Hospital Staff preparation" className="mt-2 flex flex-wrap gap-4">
              <GuardedLink href="/workforce/employment-reviews">Employment reviews</GuardedLink>
              <GuardedLink href="/workforce/postings">Department postings</GuardedLink>
              <GuardedLink href="/workforce/pharmacy-relationships">
                Pharmacy relationships
              </GuardedLink>
            </nav>
          </li>
        ) : (
          <li>
            In Operating duties below, choose the reviewed account, one operating role and the exact{" "}
            {kind === "pharmacy" ? "pharmacy outlet" : "warehouse or depot"}. A separate reviewer
            must approve the duty; wait for verified activation before use.
          </li>
        )}
      </ol>
      <p className="text-sm text-muted-foreground">
        Active directory status does not grant application access. Trading permissions, current
        licences, professional authority and stock eligibility are checked separately for each
        action.
      </p>
    </Panel>
  );
}
