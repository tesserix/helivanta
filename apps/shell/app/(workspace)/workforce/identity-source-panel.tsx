"use client";

import { useEffect, useRef } from "react";
import { Button } from "@tesserix/web";
import { Panel } from "@helivanta/ui";
import {
  ApiError,
  IdentitySourceContract,
  IdentitySourceProjectionQuery,
  IdentitySourceDraftProjection,
  useApiQuery,
} from "@helivanta/api";

import { StaffIdentityCases } from "./identity-cases-panel";

type Props = { hospitalId: string; staffId: string; onClose: () => void };
type Identity = { tenant_id: string; subject: string };
export function StaffIdentitySourcePanel(props: Props) {
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    heading.current?.focus();
  }, [props.staffId]);
  const identity = useApiQuery<Identity>(["iam", "me", "permissions"], "/iam/me/permissions");
  return (
    <Panel className="mt-5 space-y-4 p-5" aria-label="Staff identity details">
      <div className="flex items-center justify-between gap-4">
        <h3 ref={heading} tabIndex={-1} className="font-semibold">
          Identity review basis
        </h3>
        <Button variant="ghost" onClick={props.onClose}>
          Close identity details
        </Button>
      </div>
      {identity.isPending ? (
        <p role="status">Checking identity access…</p>
      ) : identity.isError || !identity.data?.tenant_id || !identity.data.subject ? (
        <p role="alert">Could not verify your current workspace. Close and reopen this panel.</p>
      ) : (
        <IdentityDetails
          key={`${identity.data.tenant_id}:${identity.data.subject}:${props.hospitalId}:${props.staffId}`}
          {...props}
          identity={identity.data}
        />
      )}
    </Panel>
  );
}
function IdentityDetails({ hospitalId, staffId, identity }: Props & { identity: Identity }) {
  const source = useApiQuery(
    ["workforce", "identity-source", identity.tenant_id, identity.subject, hospitalId, staffId],
    `/workforce/hospitals/${hospitalId}/identity-staff/${staffId}?${IdentitySourceProjectionQuery}=${IdentitySourceDraftProjection}`,
    {
      gcTime: 0,
      staleTime: 0,
      request: {
        cache: "no-store",
        headers: {
          "X-Helivanta-Expected-Tenant": identity.tenant_id,
          "X-Helivanta-Expected-Subject": identity.subject,
        },
      },
    },
  );
  if (source.isPending) return <p role="status">Loading current identity details…</p>;
  if (source.isError)
    return (
      <div role="alert">
        <p>
          {source.error instanceof ApiError && source.error.status === 403
            ? "A current hospital identity manager or reviewer assignment and active staff account link are required."
            : source.error instanceof ApiError && source.error.status === 404
              ? "Identity details are unavailable for this staff member or your current staff account link. Ask Staff administration to review the record and link."
              : "Could not load current identity details."}
        </p>
        <Button variant="outline" onClick={() => void source.refetch()}>
          Retry identity details
        </Button>
      </div>
    );
  const parsed = IdentitySourceContract.safeParse(source.data);
  if (!parsed.success || parsed.data.hospital_id !== hospitalId || parsed.data.staff_id !== staffId)
    return (
      <p role="alert">Could not validate current identity details. Close and reopen this panel.</p>
    );
  const record = parsed.data;
  return (
    <>
      <p className="text-sm text-muted-foreground">
        Viewing these details does not approve identity, background or photograph checks.
      </p>
      <dl className="grid gap-4 sm:grid-cols-2">
        {[
          ["Staff ID", record.employee_code],
          ["Display name", record.display_name],
          ["Given name", record.given_name || "Not recorded"],
          ["Family name", record.family_name || "Not recorded"],
          ["Date of birth", record.date_of_birth || "Not recorded"],
          ["Employment", record.employment_status],
        ].map(([label, value]) => (
          <div key={label}>
            <dt className="text-xs text-muted-foreground">{label}</dt>
            <dd className="font-medium">{value}</dd>
          </div>
        ))}
      </dl>
      <Button variant="outline" onClick={() => void source.refetch()} disabled={source.isFetching}>
        Refresh identity details
      </Button>
      {record.manage_checks_allowed === undefined ? (
        <p className="text-sm text-muted-foreground">
          Review drafts are not available on this server yet.
        </p>
      ) : (
        <StaffIdentityCases
          scope={{ tenantId: identity.tenant_id, subject: identity.subject, hospitalId, staffId }}
          source={record}
        />
      )}
    </>
  );
}
