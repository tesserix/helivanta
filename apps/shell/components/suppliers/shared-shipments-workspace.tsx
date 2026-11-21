"use client";
import { useState } from "react";
import { z } from "zod";
import { Button } from "@tesserix/web";
import { PageHeading, RequestFailure, RefreshButton } from "@helivanta/ui";
import {
  useApiQuery,
  useTenantTransition,
  usePartnerMemberHospitals,
  useSupplierWorkflowQuery,
  listPartnerShipmentSourceChoices,
  type SupplierHospitalScope,
  type PartnerShipmentSourceChoice,
  type PartnerShipmentScope,
} from "@helivanta/api";
import { SharedSourceChoices } from "./shared-source-choices";
import { SharedSourceDeliveries } from "./shared-source-deliveries";
const identityContract = z.object({
  tenant_id: z.uuid(),
  subject: z.string().min(1).max(200),
  data: z.array(z.string()),
});
export function SharedShipmentsWorkspace() {
  const switching = useTenantTransition();
  const [attempt, setAttempt] = useState(0);
  return switching ? (
    <p role="status">Switching supplier workspace…</p>
  ) : (
    <Account key={attempt} restart={() => setAttempt((v) => v + 1)} />
  );
}
function Account({ restart }: { restart: () => void }) {
  const [nonce] = useState(() => crypto.randomUUID());
  const identity = useApiQuery<unknown>(
    ["supplier-sharing", "account", nonce],
    "/iam/me/permissions",
    { gcTime: 0, staleTime: 0, poll: true, request: { cache: "no-store" } },
  );
  const parsed = identityContract.safeParse(identity.data);
  if (identity.isError || (!identity.isPending && !parsed.success))
    return (
      <RequestFailure
        message="Your current supplier-sharing account could not be verified."
        error={identity.error}
        retry={restart}
      />
    );
  if (!parsed.success) return <p role="status">Verifying your shared supplier access…</p>;
  if (
    !parsed.data.data.includes("partner.shipment.read") ||
    !parsed.data.data.includes("iam.supplier_grant.inbox.read")
  )
    return <p role="alert">Shared shipment access is not available for your current account.</p>;
  const scope = { tenantId: parsed.data.tenant_id, subject: parsed.data.subject };
  return (
    <div className="space-y-5">
      <PageHeading
        title="Shared supplier shipments"
        description="View published deliveries shared with your hospital. Current source and team access are checked for every read."
      />
      <Hospitals key={`${scope.tenantId}:${scope.subject}`} scope={scope} />
    </div>
  );
}
function Hospitals({ scope }: { scope: { tenantId: string; subject: string } }) {
  const hospitals = usePartnerMemberHospitals(scope);
  const [hospital, setHospital] = useState<{ hospital_id: string; name: string } | null>(null);
  if (hospitals.isError)
    return (
      <RequestFailure
        message="Current Staff hospitals could not be verified."
        error={hospitals.error}
        retry={hospitals.refetch}
      />
    );
  if (hospitals.isPending) return <p role="status">Loading your current hospitals…</p>;
  if (hospital)
    return (
      <section className="space-y-4">
        <h2 className="text-xl font-semibold">{hospital.name}</h2>
        <Button variant="outline" onClick={() => setHospital(null)}>
          Change hospital
        </Button>
        <Sources
          key={hospital.hospital_id}
          scope={{ ...scope, hospitalId: hospital.hospital_id }}
        />
      </section>
    );
  const rows = hospitals.data.pages.flatMap((page) => page.data);
  return (
    <section aria-label="Current hospital choices" className="space-y-3">
      <p>Choose the hospital whose shared deliveries you want to view.</p>
      {rows.length === 0 ? (
        <p>No current Staff hospitals are available.</p>
      ) : (
        rows.map((row) => (
          <Button key={row.hospital_id} variant="outline" onClick={() => setHospital(row)}>
            {row.name}
          </Button>
        ))
      )}
      {hospitals.hasNextPage && (
        <Button
          variant="outline"
          disabled={hospitals.isFetchingNextPage}
          onClick={() => hospitals.fetchNextPage()}
        >
          More hospitals
        </Button>
      )}
    </section>
  );
}
function Sources({ scope }: { scope: SupplierHospitalScope }) {
  const [cursor, setCursor] = useState<string | null>(null);
  const [selected, setSelected] = useState<PartnerShipmentSourceChoice | null>(null);
  const choices = useSupplierWorkflowQuery(
    ["supplier-sharing", "sources", scope, cursor],
    (signal) => listPartnerShipmentSourceChoices(scope, cursor, signal),
  );
  if (choices.isError)
    return (
      <RequestFailure
        message="Shared sources could not be verified. Previously loaded choices are hidden."
        error={choices.error}
        retry={choices.refetch}
      />
    );
  if (choices.isPending) return <p role="status">Checking current shared supplier sources…</p>;
  if (selected) {
    const source = selected.source;
    const current: PartnerShipmentScope = {
      ...scope,
      grantId: source.grant_id,
      relationshipId: source.relationship_id,
      termsSha256: source.terms_sha256,
      source: source.scope,
    };
    return (
      <section className="space-y-4">
        <Button variant="outline" onClick={() => setSelected(null)}>
          Change shared source
        </Button>
        <SharedSourceDeliveries key={source.grant_id} scope={current} />
      </section>
    );
  }
  return (
    <section className="space-y-4" aria-label="Shared source choices">
      <div className="flex items-center justify-between gap-3">
        <h3 className="text-lg font-semibold">Current shared sources</h3>
        <RefreshButton busy={choices.isFetching} refresh={choices.refetch} error={choices.error}>
          Refresh sources
        </RefreshButton>
      </div>
      {choices.data.data.length === 0 ? (
        <p>
          No active shipment sources are available on this page. Your administrator can check your
          direct or team access.
        </p>
      ) : (
        <SharedSourceChoices choices={choices.data.data} onSelect={setSelected} />
      )}
      <div className="flex gap-2">
        {cursor && (
          <Button variant="outline" onClick={() => setCursor(null)}>
            First sources
          </Button>
        )}
        {choices.data.page.has_more && (
          <Button variant="outline" onClick={() => setCursor(choices.data.page.next_cursor)}>
            Next sources
          </Button>
        )}
      </div>
    </section>
  );
}
