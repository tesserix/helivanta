"use client";
import { z } from "zod";
import { useApiQuery, supplierStockWorkflowKey } from "@helivanta/api";
import { PageHeading, RequestFailure } from "@helivanta/ui";
import { SourceFulfilment } from "./source-fulfilment";
const target = z
  .object({
    product: z.enum(["pharmacy", "supply"]),
    organizationId: z.uuid(),
    locationId: z.uuid(),
  })
  .strict();
export function SourceSupplierOperations(props: {
  product: string;
  organizationId: string;
  locationId: string;
}) {
  const identity = useApiQuery<{ tenant_id: string; subject: string }>(
    ["iam", "me", "permissions"],
    "/iam/me/permissions",
  );
  const parsed = target.safeParse(props);
  if (!parsed.success) return <p role="alert">This supplier workplace link is invalid.</p>;
  if (identity.isError)
    return (
      <RequestFailure
        message="Your current supplier account could not be verified."
        error={identity.error}
        retry={identity.refetch}
      />
    );
  if (!identity.data) return <p role="status">Verifying your supplier workplace…</p>;
  const scope = {
    ...parsed.data,
    tenantId: identity.data.tenant_id,
    subject: identity.data.subject,
  };
  return (
    <div className="space-y-5">
      <PageHeading
        title="Supplier fulfilment"
        description="Fulfil hospital orders, inspect dispatched shipments and respond to hospital receiving under your current workplace duty."
      />
      <SourceFulfilment key={supplierStockWorkflowKey(scope).join(":")} scope={scope} />
    </div>
  );
}
