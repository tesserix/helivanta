"use client";

import { useState } from "react";
import { useCustomerBusinessProfile, type CustomerOrganizationProfile } from "@helivanta/api";
import { Button, Panel, RequestFailure } from "@helivanta/ui";

export function BusinessProfilePanel({
  tenantId,
  kind,
  resourceId,
}: {
  tenantId: string;
  kind: "hospital" | "pharmacy";
  resourceId: string;
}) {
  const [open, setOpen] = useState(false);
  const profile = useCustomerBusinessProfile(tenantId, kind, resourceId, open);
  return (
    <Panel aria-label="Organisation contact and addresses" className="mb-6 space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-lg font-semibold">Organisation details</h2>
        <Button
          type="button"
          variant="outline"
          aria-expanded={open}
          onClick={() => setOpen((value) => !value)}
        >
          {open ? "Hide contact and addresses" : "View contact and addresses"}
        </Button>
      </div>
      {open &&
        (profile.isPending ? (
          <p role="status">Loading organisation details…</p>
        ) : profile.error || !profile.data ? (
          <RequestFailure
            message="Could not load organisation details."
            retry={profile.refetch}
            error={profile.error}
          />
        ) : profile.data.profile ? (
          <ProfileDetails profile={profile.data.profile} />
        ) : (
          <p>Address and contact details were not captured at signup.</p>
        ))}
    </Panel>
  );
}

function ProfileDetails({ profile }: { profile: CustomerOrganizationProfile }) {
  return (
    <div className="grid gap-5 text-sm sm:grid-cols-2">
      <div>
        <h3 className="mb-2 font-semibold">Organisation contact</h3>
        <p>{profile.contact.phone}</p>
        <p className="break-all">{profile.contact.email}</p>
      </div>
      <div>
        <h3 className="mb-2 font-semibold">Head office</h3>
        <BusinessAddress address={profile.headOffice} />
      </div>
      <div>
        <h3 className="mb-2 font-semibold">{profile.mainBranch.name}</h3>
        <BusinessAddress address={profile.mainBranch.address} />
      </div>
    </div>
  );
}

function BusinessAddress({ address }: { address: CustomerOrganizationProfile["headOffice"] }) {
  return (
    <address className="not-italic">
      <p>{address.line1}</p>
      {address.line2 && <p>{address.line2}</p>}
      <p>
        {address.city}, {address.district}
      </p>
      <p>
        {{ MH: "Maharashtra", OD: "Odisha" }[address.stateCode]} {address.postalCode}
      </p>
      <p>India</p>
    </address>
  );
}
