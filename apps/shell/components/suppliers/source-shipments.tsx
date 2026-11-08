"use client";
import { useState } from "react";
import {
  ApiError,
  useSupplierWorkflowQuery,
  supplierStockWorkflowKey,
  listSourceSupplierShipmentsPage,
  readSourceSupplierReceiving,
  listSourceSupplierResponsesPage,
  getSourceSupplierResponseAccess,
  ReceivingResponseInputContract,
  type SupplierStockScope,
  type Shipment,
  type ReceivingSummary,
  type ReceivingResponseInput,
} from "@helivanta/api";
import {
  Field,
  Panel,
  RequestFailure,
  SelectControl,
  useZodForm,
  useLeaveGuard,
  useConfirmLeave,
} from "@helivanta/ui";
import { Button, Textarea } from "@tesserix/web";
type Props = {
  scope: SupplierStockScope;
  order: string;
  frozen: boolean;
  onResponse: (shipment: Shipment, input: ReceivingResponseInput) => void;
};
export function SourceShipments({ scope, order, frozen, onResponse }: Props) {
  const confirm = useConfirmLeave();
  const [cursor, setCursor] = useState<string | null>(null),
    [selected, setSelected] = useState<Shipment | null>(null);
  const query = useSupplierWorkflowQuery(
    supplierStockWorkflowKey(scope, "shipments", order, cursor ?? ""),
    (signal) => listSourceSupplierShipmentsPage(scope, order, { cursor }, signal),
    true,
  );
  return (
    <section aria-label="Dispatched shipments and hospital receiving" className="space-y-3">
      <h5 className="font-medium">Dispatched shipments and hospital receiving</h5>
      {query.isError ? (
        <RequestFailure
          message="Source shipments could not be verified."
          error={query.error}
          retry={query.refetch}
        />
      ) : query.isPending ? (
        <p role="status">Loading dispatched shipments…</p>
      ) : (
        <>
          {query.data.data.length ? (
            <ul className="space-y-2">
              {query.data.data.map((shipment) => (
                <li
                  key={shipment.id}
                  className="flex items-center justify-between gap-3 border-b py-3"
                >
                  <span>
                    {shipment.handoff_reference} · {shipment.quantity_units} {shipment.unit_code} ·
                    lot {shipment.lot_number} · expiry {shipment.expires_on}
                  </span>
                  <Button
                    variant="outline"
                    disabled={frozen}
                    onClick={() => confirm(() => setSelected(shipment))}
                  >
                    Inspect receiving for {shipment.handoff_reference}
                  </Button>
                </li>
              ))}
            </ul>
          ) : (
            <p>No physical shipments have been dispatched for this order.</p>
          )}
          <Page
            cursor={cursor}
            next={query.data.page.next_cursor}
            disabled={frozen}
            change={(value) =>
              confirm(() => {
                setCursor(value);
                setSelected(null);
              })
            }
          />
        </>
      )}
      {selected && (
        <ShipmentReceiving
          key={selected.id}
          scope={scope}
          order={order}
          shipment={selected}
          frozen={frozen}
          onResponse={onResponse}
        />
      )}
    </section>
  );
}
function Page({
  cursor,
  next,
  disabled,
  change,
}: {
  cursor: string | null;
  next: string | null;
  disabled: boolean;
  change: (cursor: string | null) => void;
}) {
  return (
    <div className="flex gap-2">
      {cursor && (
        <Button type="button" variant="outline" disabled={disabled} onClick={() => change(null)}>
          Latest shipments or responses
        </Button>
      )}
      {next && (
        <Button type="button" variant="outline" disabled={disabled} onClick={() => change(next)}>
          Earlier shipments or responses
        </Button>
      )}
    </div>
  );
}
function ShipmentReceiving({
  scope,
  shipment,
  frozen,
  onResponse,
}: Props & { shipment: Shipment }) {
  const confirm = useConfirmLeave();
  const [cursor, setCursor] = useState<string | null>(null);
  const summary = useSupplierWorkflowQuery(
    supplierStockWorkflowKey(scope, "shipment-receiving", shipment.id),
    async (signal) => {
      try {
        return await readSourceSupplierReceiving(scope, shipment, signal);
      } catch (error) {
        if (error instanceof ApiError && error.status === 404) return null;
        throw error;
      }
    },
    true,
  );
  const history = useSupplierWorkflowQuery(
    supplierStockWorkflowKey(scope, "shipment-responses", shipment.id, cursor ?? ""),
    (signal) => listSourceSupplierResponsesPage(scope, shipment, { cursor }, signal),
    true,
  );
  const access = useSupplierWorkflowQuery(
    supplierStockWorkflowKey(scope, "response-access", shipment.order_id),
    (signal) => getSourceSupplierResponseAccess(scope, shipment.order_id, signal),
  );
  return (
    <Panel
      aria-label={`Hospital receiving for ${shipment.handoff_reference}`}
      className="space-y-3"
    >
      {summary.isError ? (
        <RequestFailure
          message="The hospital receiving projection could not be verified."
          error={summary.error}
          retry={summary.refetch}
        />
      ) : summary.isPending ? (
        <p role="status">Loading hospital receiving projection…</p>
      ) : summary.data ? (
        <>
          <p>
            Hospital receiving version {summary.data.version}: {summary.data.counted_units} counted,{" "}
            {summary.data.accepted_units} accepted, {summary.data.rejected_units} rejected,{" "}
            {summary.data.short_units} short.{" "}
            {summary.data.closed ? "Closed by hospital." : "Still open at hospital."}
          </p>
          <p>
            {summary.data.reason} · recorded{" "}
            {new Date(summary.data.recorded_at).toLocaleString("en-IN")}. This is the latest update
            received from the hospital.
          </p>
          {!summary.data.closed ? (
            <p>
              The hospital must close receiving before a supplier confirmation or dispute can be
              recorded.
            </p>
          ) : access.isError ? (
            access.error instanceof ApiError && access.error.status === 403 ? (
              <p>
                Responding to hospital receiving is not available for this account and source duty.
              </p>
            ) : (
              <RequestFailure
                message="Current supplier-response authority could not be verified."
                error={access.error}
                retry={access.refetch}
              />
            )
          ) : access.isPending ? (
            <p role="status">Verifying current source response duty…</p>
          ) : (
            <ResponseForm
              summary={summary.data}
              shipment={shipment}
              frozen={frozen}
              onResponse={onResponse}
            />
          )}
        </>
      ) : (
        <p>No hospital receiving update is available for this shipment yet.</p>
      )}
      {summary.isSuccess && (
        <>
          <h6 className="font-medium">Original supplier responses</h6>
          {history.isError ? (
            <RequestFailure
              message="Supplier response history could not be verified."
              error={history.error}
              retry={history.refetch}
            />
          ) : history.isPending ? (
            <p role="status">Loading supplier response history…</p>
          ) : (
            <>
              {history.data.data.length ? (
                <ul>
                  {history.data.data.map((response) => (
                    <li key={response.id}>
                      Receiving version {response.summary_version} · {response.response} ·{" "}
                      {response.reason} · {response.actor_subject}
                    </li>
                  ))}
                </ul>
              ) : (
                <p>No supplier response has been recorded.</p>
              )}
              <Page
                cursor={cursor}
                next={history.data.page.next_cursor}
                disabled={frozen}
                change={(value) => confirm(() => setCursor(value))}
              />
            </>
          )}
        </>
      )}
    </Panel>
  );
}
function ResponseForm({
  summary,
  shipment,
  frozen,
  onResponse,
}: {
  summary: ReceivingSummary;
  shipment: Shipment;
  frozen: boolean;
  onResponse: Props["onResponse"];
}) {
  const confirm = useConfirmLeave();
  const [version, setVersion] = useState(summary.version);
  const changed = version !== summary.version;
  const form = useZodForm(ReceivingResponseInputContract.omit({ expected_summary_version: true }), {
    response: "confirmed",
    reason: "",
  });
  useLeaveGuard(form.formState.isDirty, "supplier receiving response draft", frozen);
  return (
    <form
      aria-label={`Respond to ${shipment.handoff_reference}`}
      className="space-y-3"
      onSubmit={form.handleSubmit((value) => {
        if (!frozen && !changed)
          onResponse(shipment, { ...value, expected_summary_version: version });
      })}
    >
      <p>
        This response is sent to the hospital. It does not change hospital counts, stock or
        invoices.
      </p>
      {changed && (
        <>
          <p role="alert">
            A newer hospital receiving version is available. Recheck it before responding.
          </p>
          <Button
            type="button"
            variant="outline"
            disabled={frozen}
            onClick={() =>
              confirm(() => {
                setVersion(summary.version);
                form.reset();
              })
            }
          >
            Use latest hospital receiving evidence
          </Button>
        </>
      )}
      <Field id={`source-response-${shipment.id}`} label="Supplier response">
        <SelectControl
          id={`source-response-${shipment.id}`}
          disabled={frozen || changed}
          value={form.watch("response")}
          options={[
            { value: "confirmed", label: "Confirm hospital receiving" },
            { value: "disputed", label: "Dispute hospital receiving" },
          ]}
          onValueChange={(value) =>
            form.setValue("response", ReceivingResponseInputContract.shape.response.parse(value), {
              shouldDirty: true,
            })
          }
        />
      </Field>
      <Field
        id={`source-response-reason-${shipment.id}`}
        label="Supplier response evidence"
        error={form.formState.errors.reason?.message}
      >
        <Textarea
          id={`source-response-reason-${shipment.id}`}
          disabled={frozen || changed}
          {...form.register("reason")}
        />
      </Field>
      <Button type="submit" disabled={frozen || changed}>
        Record supplier response
      </Button>
    </form>
  );
}
