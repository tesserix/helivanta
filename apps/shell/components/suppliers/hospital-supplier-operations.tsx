"use client";
import { useState } from "react";
import { z } from "zod";
import { useApiQuery, type HospitalSupplierReceivingScope } from "@helivanta/api";
import { PageHeading, RequestFailure, useConfirmLeave } from "@helivanta/ui";
import { Button } from "@tesserix/web";
import { HospitalReceiving } from "./hospital-receiving";
import { SourceFulfilment } from "./source-fulfilment";
const target = z
  .object({
    hospitalId: z.uuid(),
    departmentId: z.uuid(),
    pharmacyId: z.uuid(),
    outletId: z.uuid(),
  })
  .strict();
type Target = z.infer<typeof target>;
export function HospitalSupplierOperations(props: Target) {
  const identity = useApiQuery<{ tenant_id: string; subject: string }>(
    ["iam", "me", "permissions"],
    "/iam/me/permissions",
  );
  if (!target.safeParse(props).success)
    return <p role="alert">This hospital pharmacy workspace link is invalid.</p>;
  if (identity.isError)
    return (
      <RequestFailure
        message="Your current account could not be verified."
        error={identity.error}
        retry={identity.refetch}
      />
    );
  if (!identity.data) return <p role="status">Verifying your hospital pharmacy workspace…</p>;
  const scope = { ...props, tenantId: identity.data.tenant_id, subject: identity.data.subject };
  return <Operations key={Object.values(scope).join(":")} scope={scope} />;
}
function Operations({ scope }: { scope: HospitalSupplierReceivingScope }) {
  const [mode, setMode] = useState<"receiving" | "fulfilment">("receiving");
  const confirm = useConfirmLeave();
  return (
    <div className="space-y-5">
      <PageHeading
        title="Hospital pharmacy supplier operations"
        description="Receive external deliveries or fulfil orders from this hospital-owned pharmacy under your current department duty."
      />
      <div className="flex gap-2">
        <Button
          variant={mode === "receiving" ? "default" : "outline"}
          onClick={() => confirm(() => setMode("receiving"))}
        >
          Hospital receiving
        </Button>
        <Button
          variant={mode === "fulfilment" ? "default" : "outline"}
          onClick={() => confirm(() => setMode("fulfilment"))}
        >
          Hospital pharmacy fulfilment
        </Button>
      </div>
      {mode === "receiving" ? (
        <HospitalReceiving scope={scope} />
      ) : (
        <SourceFulfilment
          scope={{
            tenantId: scope.tenantId,
            subject: scope.subject,
            product: "pharmacy",
            organizationId: scope.pharmacyId,
            locationId: scope.outletId,
            hospitalId: scope.hospitalId,
            departmentId: scope.departmentId,
          }}
        />
      )}
    </div>
  );
}
