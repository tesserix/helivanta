"use client";
import { useState } from "react";
import { Button } from "@tesserix/web";
import { Panel, RequestFailure, formatDateTime } from "@helivanta/ui";
import {
  useSupplierWorkflowQuery,
  getPartnerShipmentSource,
  listPartnerShipmentOrders,
  listPartnerShipments,
  type PartnerShipmentScope,
  type PartnerShipmentOrder,
} from "@helivanta/api";
const expiryDate = new Intl.DateTimeFormat("en-IN", { dateStyle: "medium", timeZone: "UTC" });
export function SharedSourceDeliveries({ scope }: { scope: PartnerShipmentScope }) {
  const source = useSupplierWorkflowQuery(
    ["supplier-sharing", "source", scope],
    (signal) => getPartnerShipmentSource(scope, signal),
    true,
  );
  if (source.isError)
    return (
      <RequestFailure
        message="This shared source could not be verified. Previously loaded delivery evidence is hidden."
        error={source.error}
        retry={source.refetch}
      />
    );
  if (source.isPending) return <p role="status">Verifying the selected supplier source…</p>;
  return (
    <section className="space-y-4">
      <Panel className="space-y-2">
        <h3 className="text-lg font-semibold break-words">
          {source.data.source_organization_name}
        </h3>
        <p className="break-words text-muted-foreground">{source.data.source_location_name}</p>
        <p className="text-sm text-muted-foreground">Shared shipment evidence · read-only</p>
      </Panel>
      <Orders
        scope={scope}
        organization={source.data.source_organization_name}
        location={source.data.source_location_name}
      />
    </section>
  );
}
function Orders({
  scope,
  organization,
  location,
}: {
  scope: PartnerShipmentScope;
  organization: string;
  location: string;
}) {
  const [cursor, setCursor] = useState<string | null>(null);
  const [selected, setSelected] = useState<PartnerShipmentOrder | null>(null);
  const query = useSupplierWorkflowQuery(
    ["supplier-sharing", "orders", scope, cursor],
    (signal) => listPartnerShipmentOrders(scope, cursor, signal),
    true,
  );
  if (query.isError)
    return (
      <RequestFailure
        message="Shared delivery orders could not be verified. Previously loaded orders are hidden."
        error={query.error}
        retry={query.refetch}
      />
    );
  if (query.isPending) return <p role="status">Loading published delivery orders…</p>;
  if (
    query.data.data.some(
      (row) =>
        row.source_organization_name !== organization || row.source_location_name !== location,
    )
  )
    return (
      <RequestFailure
        message="Source details changed. Refresh the current order evidence."
        error={null}
        retry={query.refetch}
      />
    );
  if (selected) {
    const current = query.data.data.find((row) => row.id === selected.id);
    if (!current)
      return (
        <section role="alert">
          <p>The selected delivery order is no longer available on this page.</p>
          <Button variant="outline" onClick={() => setSelected(null)}>
            Back to delivery orders
          </Button>
        </section>
      );
    return (
      <section className="space-y-4">
        <Button variant="outline" onClick={() => setSelected(null)}>
          Back to delivery orders
        </Button>
        <h4 className="font-semibold">{current.purchase_order_reference}</h4>
        <Shipments
          scope={scope}
          order={current.id}
          organization={organization}
          location={location}
        />
      </section>
    );
  }
  return (
    <section className="space-y-3" aria-label="Published shared delivery orders">
      <h4 className="font-semibold">Published delivery orders</h4>
      {query.data.data.length === 0 ? (
        <p>No published dispatches are currently shared by this source.</p>
      ) : (
        <ul className="space-y-3">
          {query.data.data.map((row) => (
            <li key={row.id}>
              <Panel className="flex flex-wrap items-center justify-between gap-3">
                <div>
                  <p className="font-medium">{row.purchase_order_reference}</p>
                  <p className="text-sm text-muted-foreground">
                    Created {formatDateTime(row.created_at)}
                  </p>
                </div>
                <Button variant="outline" onClick={() => setSelected(row)}>
                  View published shipments
                </Button>
              </Panel>
            </li>
          ))}
        </ul>
      )}
      <div className="flex gap-2">
        {cursor && (
          <Button variant="outline" onClick={() => setCursor(null)}>
            First delivery orders
          </Button>
        )}
        {query.data.page.has_more && (
          <Button variant="outline" onClick={() => setCursor(query.data.page.next_cursor)}>
            Next delivery orders
          </Button>
        )}
      </div>
    </section>
  );
}
function Shipments({
  scope,
  order,
  organization,
  location,
}: {
  scope: PartnerShipmentScope;
  order: string;
  organization: string;
  location: string;
}) {
  const [cursor, setCursor] = useState<string | null>(null);
  const query = useSupplierWorkflowQuery(
    ["supplier-sharing", "shipments", scope, order, cursor],
    (signal) => listPartnerShipments(scope, order, cursor, signal),
    true,
  );
  if (query.isError)
    return (
      <RequestFailure
        message="Current shipment evidence could not be verified. Previously loaded deliveries are hidden."
        error={query.error}
        retry={query.refetch}
      />
    );
  if (query.isPending) return <p role="status">Loading shared shipment evidence…</p>;
  if (
    query.data.data.some(
      (row) =>
        row.source_organization_name !== organization || row.source_location_name !== location,
    )
  )
    return (
      <RequestFailure
        message="Source details changed. Refresh the current shipment evidence."
        error={null}
        retry={query.refetch}
      />
    );
  return (
    <section aria-label="Shared shipment evidence" className="space-y-3">
      {query.data.data.length === 0 ? (
        <p>No shipment evidence is available on this page.</p>
      ) : (
        query.data.data.map((row) => (
          <Panel key={row.id} className="space-y-3">
            <h5 className="font-semibold break-words">{row.handoff_reference}</h5>
            <p className="break-words">{row.product_description}</p>
            <dl className="grid gap-3 text-sm sm:grid-cols-2">
              <div>
                <dt className="text-muted-foreground">Dispatched quantity</dt>
                <dd>
                  {row.quantity_units} {row.unit_code} · pack size {row.pack_size}
                </dd>
              </div>
              <div>
                <dt className="text-muted-foreground">Lot</dt>
                <dd>{row.lot_number}</dd>
              </div>
              <div>
                <dt className="text-muted-foreground">Expiry</dt>
                <dd>{expiryDate.format(new Date(`${row.expires_on}T00:00:00Z`))}</dd>
              </div>
              <div>
                <dt className="text-muted-foreground">Dispatched</dt>
                <dd>{formatDateTime(row.dispatched_at)}</dd>
              </div>
            </dl>
          </Panel>
        ))
      )}
      <div className="flex gap-2">
        {cursor && (
          <Button variant="outline" onClick={() => setCursor(null)}>
            First shipments
          </Button>
        )}
        {query.data.page.has_more && (
          <Button variant="outline" onClick={() => setCursor(query.data.page.next_cursor)}>
            Next shipments
          </Button>
        )}
      </div>
    </section>
  );
}
