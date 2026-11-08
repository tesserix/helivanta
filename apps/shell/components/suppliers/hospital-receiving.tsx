"use client";
import { useState } from "react";
import {
  newSupplierWorkflowId,
  ReceivingInputContract,
  readHospitalSupplierReceiving,
  listHospitalSupplierReceiptsPage,
  listHospitalSupplierResponsesPage,
  listHospitalReceivingProductChoicesPage,
  listHospitalReceivingLocationsPage,
  listHospitalReceivingBatchChoicesPage,
  type ReceivingInput,
  type ReceivingProductChoice,
  type ReceivingBatchChoice,
  hospitalSupplierReceivingKey,
  listHospitalSupplierShipmentsPage,
  useSupplierWorkflowQuery,
  type HospitalSupplierReceivingScope,
  type Shipment,
} from "@helivanta/api";
import {
  useZodForm,
  Field,
  Panel,
  RequestFailure,
  SelectControl,
  useLeaveGuard,
  useConfirmLeave,
} from "@helivanta/ui";
import { Button, Input } from "@tesserix/web";
import { ReceivingReceiptInspection, ReceivingClosureForm } from "./receiving-review";
import { useReceivingCommands, type ReceivingCommands } from "./receiving-commands";
import { CurrencyAmountField } from "../pharmacy/currency-amount-field";
type Props = { scope: HospitalSupplierReceivingScope };
export function HospitalReceiving({ scope }: Props) {
  return <ReceivingWorklist key={hospitalSupplierReceivingKey(scope).join(":")} scope={scope} />;
}
function ReceivingWorklist({ scope }: Props) {
  const confirm = useConfirmLeave();
  const [cursor, setCursor] = useState<string | null>(null),
    [selected, setSelected] = useState<Shipment | null>(null);
  const query = useSupplierWorkflowQuery(
    hospitalSupplierReceivingKey(scope, "shipments", cursor ?? ""),
    (signal) => listHospitalSupplierShipmentsPage(scope, { cursor }, signal),
  );
  return (
    <Panel aria-label="Hospital supplier receiving" className="space-y-4">
      <h3 className="font-semibold">Hospital supplier receiving</h3>
      <p>
        Inspect the actual delivery before recording counts. Accepted goods remain in quarantine
        until independent review and physical release.
      </p>
      {query.isError ? (
        <RequestFailure
          message="Supplier shipments could not be verified."
          error={query.error}
          retry={query.refetch}
        />
      ) : query.isPending ? (
        <p role="status">Loading supplier shipments…</p>
      ) : !query.data.data.length ? (
        <p>No supplier shipments for this outlet.</p>
      ) : (
        <ul className="space-y-2">
          {query.data.data.map((shipment) => (
            <li
              key={shipment.id}
              className="flex items-center justify-between gap-3 rounded-md border p-3"
            >
              <span>
                {shipment.handoff_reference} · {shipment.product_description} ·{" "}
                {shipment.quantity_units} {shipment.unit_code}
              </span>
              <Button
                variant="outline"
                aria-label={`Inspect ${shipment.handoff_reference}`}
                onClick={() => confirm(() => setSelected(shipment))}
              >
                Inspect
              </Button>
            </li>
          ))}
        </ul>
      )}
      {query.isSuccess && (
        <ReceivingPageButtons
          cursor={cursor}
          next={query.data.page.next_cursor}
          change={(value) =>
            confirm(() => {
              setSelected(null);
              setCursor(value);
            })
          }
        />
      )}
      {selected && <ReceivingShipment key={selected.id} scope={scope} shipment={selected} />}
    </Panel>
  );
}
function ReceivingPageButtons({
  cursor,
  next,
  change,
  disabled = false,
}: {
  cursor: string | null;
  next: string | null;
  change: (value: string | null) => void;
  disabled?: boolean;
}) {
  return (
    <div className="flex gap-2">
      {cursor && (
        <Button variant="outline" disabled={disabled} onClick={() => change(null)}>
          First page
        </Button>
      )}
      {next && (
        <Button variant="outline" disabled={disabled} onClick={() => change(next)}>
          Next page
        </Button>
      )}
    </div>
  );
}

function ReceivingShipment({
  scope,
  shipment,
}: {
  scope: HospitalSupplierReceivingScope;
  shipment: Shipment;
}) {
  const confirm = useConfirmLeave();
  const [selectedReceipt, setSelectedReceipt] = useState<string | null>(null);
  const [responseCursor, setResponseCursor] = useState<string | null>(null);
  const [cursor, setCursor] = useState<string | null>(null);
  const commands = useReceivingCommands(scope, shipment.id);
  const summary = useSupplierWorkflowQuery(
    hospitalSupplierReceivingKey(scope, shipment.id, "summary"),
    () => readHospitalSupplierReceiving(scope, shipment.id),
    true,
  );
  const receipts = useSupplierWorkflowQuery(
    hospitalSupplierReceivingKey(scope, shipment.id, "receipts", cursor ?? ""),
    (signal) => listHospitalSupplierReceiptsPage(scope, shipment.id, { cursor }, signal),
    true,
  );
  const responses = useSupplierWorkflowQuery(
    hospitalSupplierReceivingKey(scope, shipment.id, "supplier-responses", responseCursor ?? ""),
    (signal) =>
      listHospitalSupplierResponsesPage(scope, shipment, { cursor: responseCursor }, signal),
    true,
  );
  return (
    <Panel aria-label="Selected shipment" className="space-y-4">
      {summary.isError ? (
        <RequestFailure
          message="Current receiving could not be verified."
          error={summary.error}
          retry={summary.refetch}
        />
      ) : summary.isPending ? (
        <p role="status">Verifying current receiving…</p>
      ) : (
        <>
          <h4 className="font-semibold">{shipment.handoff_reference}</h4>
          <p>
            {shipment.product_description}. Lot {shipment.lot_number}; expiry {shipment.expires_on};
            pack {shipment.pack_size}; shipped {shipment.quantity_units} {shipment.unit_code}.
          </p>
          {summary.data ? (
            <p role="status">
              Current receiving: {summary.data.counted_units} counted, {summary.data.accepted_units}{" "}
              accepted, {summary.data.rejected_units} rejected, {summary.data.short_units} short.{" "}
              {summary.data.closed ? "Receiving closed." : "Receiving remains open."}
            </p>
          ) : (
            <p>No receiving has been recorded for this shipment.</p>
          )}
        </>
      )}
      <ReceivingCommandFeedback commands={commands} authorized={summary.isSuccess} />
      <DeliveryForm
        commands={commands}
        scope={scope}
        shipment={shipment}
        authorized={summary.isSuccess}
        closed={summary.data?.closed ?? false}
      />
      {summary.isSuccess && (
        <>
          <ReceivingClosureForm
            scope={scope}
            shipment={shipment.id}
            summary={summary.data}
            commands={commands}
          />
          {receipts.isError ? (
            <RequestFailure
              message="Original receipt history is unavailable."
              error={receipts.error}
              retry={receipts.refetch}
            />
          ) : receipts.isPending ? (
            <p role="status">Loading original receipts…</p>
          ) : (
            <>
              <h5 className="font-medium">Original receipt evidence</h5>
              {receipts.data.data.length ? (
                <ul>
                  {receipts.data.data.map((receipt) => (
                    <li key={receipt.id} className="border-b py-3">
                      <p>
                        {receipt.input.counted_units} counted · {receipt.input.accepted_units}{" "}
                        accepted · {receipt.input.rejected_units} rejected · {receipt.input.reason}
                      </p>
                      <Button
                        type="button"
                        variant="outline"
                        disabled={commands.frozen}
                        onClick={() => confirm(() => setSelectedReceipt(receipt.id))}
                      >
                        Inspect original receipt
                      </Button>
                    </li>
                  ))}
                </ul>
              ) : (
                <p>No original receipts yet.</p>
              )}
              <ReceivingPageButtons
                cursor={cursor}
                next={receipts.data.page.next_cursor}
                disabled={commands.frozen}
                change={(value) =>
                  confirm(() => {
                    setSelectedReceipt(null);
                    setCursor(value);
                  })
                }
              />
              {(() => {
                const active =
                  receipts.data.data.find((v) => v.id === selectedReceipt) ?? receipts.data.data[0];
                return active ? (
                  <ReceivingReceiptInspection
                    key={active.id}
                    scope={scope}
                    shipment={shipment.id}
                    receipt={active}
                    summary={summary.data}
                    commands={commands}
                  />
                ) : null;
              })()}
            </>
          )}
          <h5 className="font-medium">Supplier responses</h5>
          <p>
            Supplier-authored responses annotate a specific receiving version. They do not change
            hospital counts or stock.
          </p>
          {responses.isError ? (
            <RequestFailure
              message="Supplier responses could not be verified."
              error={responses.error}
              retry={responses.refetch}
            />
          ) : responses.isPending ? (
            <p role="status">Loading supplier responses…</p>
          ) : responses.data.data.length ? (
            <ul>
              {responses.data.data.map((response) => (
                <li key={response.id}>
                  Version {response.summary_version} · {response.response} · {response.reason} ·{" "}
                  {response.actor_subject}
                </li>
              ))}
            </ul>
          ) : (
            <p>No supplier responses recorded.</p>
          )}
          {responses.isSuccess && (
            <ReceivingPageButtons
              cursor={responseCursor}
              next={responses.data.page.next_cursor}
              disabled={commands.frozen}
              change={(value) => confirm(() => setResponseCursor(value))}
            />
          )}
        </>
      )}
    </Panel>
  );
}

const deliverySchema = ReceivingInputContract.pick({
  counted_units: true,
  accepted_units: true,
  rejected_units: true,
  reason: true,
  identity_confirmed: true,
  observed_lot_number: true,
  observed_expires_on: true,
  mrp_minor: true,
}).superRefine((v, ctx) => {
  if (v.accepted_units + v.rejected_units !== v.counted_units)
    ctx.addIssue({
      code: "custom",
      path: ["counted_units"],
      message: "Accepted and rejected units must equal the physical count.",
    });
  if (v.accepted_units > 0 && !v.identity_confirmed)
    ctx.addIssue({
      code: "custom",
      path: ["identity_confirmed"],
      message:
        "Inspect and confirm the complete medicine and pack identity before accepting units.",
    });
});
type Destination = { product: ReceivingProductChoice; location: string; batch: string };
function DeliveryForm({
  scope,
  shipment,
  authorized,
  closed,
  commands,
}: {
  scope: HospitalSupplierReceivingScope;
  shipment: Shipment;
  authorized: boolean;
  closed: boolean;
  commands: ReceivingCommands;
}) {
  const [destination, setDestination] = useState<Destination | null>(null),
    [localError, setLocalError] = useState<string | null>(null);
  const form = useZodForm(deliverySchema, {
    accepted_units: 0,
    rejected_units: 0,
    reason: "",
    identity_confirmed: false,
    observed_lot_number: "",
    observed_expires_on: "",
    mrp_minor: undefined,
  });
  const frozen = commands.frozen;
  useLeaveGuard(form.formState.isDirty, "hospital delivery draft", frozen);
  if (!authorized) return null;
  if (closed) return <p>Receiving is closed. No new delivery may be recorded.</p>;
  return (
    <form
      aria-label="Record inspected delivery"
      className="space-y-3"
      onSubmit={(event) => {
        void form.handleSubmit((value) => {
          if (commands.frozen) return;
          if (value.accepted_units > 0 && !destination) {
            setLocalError("Choose and inspect the destination product, lot and quarantine shelf.");
            return;
          }
          const input: ReceivingInput = {
            counted_units: value.counted_units,
            accepted_units: value.accepted_units,
            rejected_units: value.rejected_units,
            reason: value.reason,
            identity_confirmed: value.identity_confirmed,
            observed_lot_number: value.observed_lot_number,
            observed_expires_on: value.observed_expires_on,
          };
          if (value.accepted_units > 0 && destination)
            Object.assign(input, {
              destination_product_id: destination.product.id,
              destination_product_version: destination.product.version,
              destination_batch_id: destination.batch,
              quarantine_location_id: destination.location,
              mrp_minor: value.mrp_minor,
            });
          void commands
            .run({ kind: "delivery", id: newSupplierWorkflowId(), input })
            .then((saved) => {
              if (saved) {
                form.reset();
                setDestination(null);
                setLocalError(null);
              }
            });
        })(event);
      }}
    >
      <h5 className="font-medium">Record inspected delivery</h5>
      <fieldset disabled={frozen} className="space-y-3">
        {(
          [
            ["counted_units", "Physically counted units"],
            ["accepted_units", "Accepted units"],
            ["rejected_units", "Rejected units"],
          ] as const
        ).map(([name, label]) => (
          <Field
            key={name}
            id={`receiving-${name}`}
            label={label}
            error={form.formState.errors[name]?.message}
          >
            <Input
              id={`receiving-${name}`}
              type="number"
              min={name === "counted_units" ? 1 : 0}
              step="1"
              {...form.register(name, {
                valueAsNumber: true,
                onChange: (event) => {
                  if (name === "accepted_units" && !(Number(event.target.value) > 0)) {
                    setDestination(null);
                    form.setValue("identity_confirmed", false);
                    form.setValue("mrp_minor", undefined);
                  }
                },
              })}
            />
          </Field>
        ))}
        <Field
          id="receiving-lot"
          label="Observed lot number"
          error={form.formState.errors.observed_lot_number?.message}
        >
          <Input id="receiving-lot" {...form.register("observed_lot_number")} />
        </Field>
        <Field
          id="receiving-expiry"
          label="Observed expiry date"
          error={form.formState.errors.observed_expires_on?.message}
        >
          <Input id="receiving-expiry" type="date" {...form.register("observed_expires_on")} />
        </Field>
        {form.watch("accepted_units") > 0 && (
          <>
            <DestinationChoices
              scope={scope}
              shipment={shipment}
              onSelection={(value) => {
                setDestination(value);
                if (!value) {
                  form.setValue("identity_confirmed", false);
                  form.setValue("mrp_minor", undefined);
                }
              }}
              frozen={frozen}
            />
            {destination && (
              <CurrencyAmountField
                key={destination?.product.id ?? "unselected"}
                currency={destination.product.currency}
                initialMinor={null}
                id="receiving-mrp"
                label="Printed pack MRP"
                disabled={frozen}
                error={form.formState.errors.mrp_minor?.message}
                onChange={(minor) =>
                  form.setValue("mrp_minor", minor, { shouldDirty: true, shouldValidate: true })
                }
              />
            )}
            <Field
              id="receiving-identity"
              label="I inspected ingredient, strength, formulation, route, pack and units against the shipment and destination product"
              error={form.formState.errors.identity_confirmed?.message}
            >
              <Input
                id="receiving-identity"
                type="checkbox"
                {...form.register("identity_confirmed")}
              />
            </Field>
          </>
        )}
        <Field
          id="receiving-reason"
          label="Inspection and count notes"
          error={form.formState.errors.reason?.message}
        >
          <Input id="receiving-reason" {...form.register("reason")} />
        </Field>
      </fieldset>
      {localError && <p role="alert">{localError}</p>}
      <Button type="submit" disabled={frozen}>
        Record inspected delivery
      </Button>
    </form>
  );
}
function DestinationChoices({
  scope,
  shipment,
  onSelection,
  frozen,
}: {
  scope: HospitalSupplierReceivingScope;
  shipment: Shipment;
  onSelection: (value: Destination | null) => void;
  frozen: boolean;
}) {
  const [locationCursor, setLocationCursor] = useState<string | null>(null);
  const [cursor, setCursor] = useState<string | null>(null),
    [selected, setSelected] = useState<ReceivingProductChoice | null>(null),
    [location, setLocation] = useState("");
  const products = useSupplierWorkflowQuery(
    hospitalSupplierReceivingKey(scope, shipment.id, "product-choices", cursor ?? ""),
    (signal) => listHospitalReceivingProductChoicesPage(scope, shipment.id, { cursor }, signal),
  );
  const locations = useSupplierWorkflowQuery(
    hospitalSupplierReceivingKey(scope, shipment.id, "quarantine-locations", locationCursor ?? ""),
    (signal) =>
      listHospitalReceivingLocationsPage(
        scope,
        shipment.id,
        "quarantine",
        { cursor: locationCursor },
        signal,
      ),
  );
  if (products.isError || locations.isError)
    return (
      <RequestFailure
        message="Destination choices could not be verified."
        error={products.error ?? locations.error}
        retry={() => {
          void products.refetch();
          void locations.refetch();
        }}
      />
    );
  if (products.isPending || locations.isPending)
    return <p role="status">Loading destination products and quarantine shelves…</p>;
  return (
    <div className="space-y-3">
      <p>
        Unit and pack matches are inspection candidates. They do not certify medicine equivalence.
      </p>
      <Field id="receiving-product" label="Destination medicine">
        <SelectControl
          id="receiving-product"
          disabled={frozen}
          value={selected?.id ?? ""}
          placeholder="Choose destination medicine"
          options={products.data.data.map((product) => ({
            value: product.id,
            label: `${product.brand_name} · ${product.generic_name} · ${product.identity_description} · ${product.manufacturer} · pack ${product.pack_size}`,
          }))}
          onValueChange={(id) => {
            setSelected(products.data.data.find((v) => v.id === id) ?? null);
            onSelection(null);
          }}
        />
      </Field>
      <ReceivingPageButtons
        cursor={cursor}
        next={products.data.page.next_cursor}
        disabled={frozen}
        change={(value) => {
          setCursor(value);
          setSelected(null);
          onSelection(null);
        }}
      />
      <Field id="receiving-quarantine" label="Quarantine shelf">
        <SelectControl
          id="receiving-quarantine"
          disabled={frozen}
          value={location}
          placeholder="Choose quarantine shelf"
          options={locations.data.data.map((v) => ({
            value: v.id,
            label: `${v.code} · ${v.name}`,
          }))}
          onValueChange={(value) => {
            setLocation(value);
            onSelection(null);
          }}
        />
      </Field>
      <ReceivingPageButtons
        cursor={locationCursor}
        next={locations.data.page.next_cursor}
        disabled={frozen}
        change={(value) => {
          setLocationCursor(value);
          setLocation("");
          onSelection(null);
        }}
      />
      {selected && location && (
        <DestinationLot
          key={`${selected.id}:${selected.version}:${location}`}
          scope={scope}
          shipment={shipment}
          product={selected}
          location={location}
          frozen={frozen}
          onSelection={onSelection}
        />
      )}
    </div>
  );
}
function DestinationLot({
  scope,
  shipment,
  product,
  location,
  frozen,
  onSelection,
}: {
  scope: HospitalSupplierReceivingScope;
  shipment: Shipment;
  product: ReceivingProductChoice;
  location: string;
  frozen: boolean;
  onSelection: (value: Destination | null) => void;
}) {
  const [chosen, setChosen] = useState("");
  const batches = useSupplierWorkflowQuery(
    hospitalSupplierReceivingKey(scope, shipment.id, "batch-choices", product.id),
    (signal) => listHospitalReceivingBatchChoicesPage(scope, shipment, product.id, {}, signal),
  );
  if (batches.isError)
    return (
      <RequestFailure
        message="Destination lot could not be verified."
        error={batches.error}
        retry={batches.refetch}
      />
    );
  if (batches.isPending) return <p role="status">Checking the destination lot…</p>;
  function choose(batch: ReceivingBatchChoice) {
    setChosen(batch.id);
    onSelection({ product, location, batch: batch.id });
  }
  return (
    <div className="space-y-2">
      {batches.data.data.length ? (
        batches.data.data.map((batch) => (
          <div key={batch.id}>
            <p>
              Existing lot {batch.lot_number}, expiry {batch.expires_on}, product version{" "}
              {batch.product_version}, MRP{" "}
              {batch.mrp_minor === null
                ? "not recorded"
                : new Intl.NumberFormat("en-IN", {
                    style: "currency",
                    currency: product.currency,
                  }).format(batch.mrp_minor / 100)}
              .
            </p>
            <Button
              type="button"
              variant="outline"
              disabled={
                frozen ||
                batch.product_version !== product.version ||
                batch.expires_on !== shipment.expires_on
              }
              onClick={() => choose(batch)}
            >
              {chosen === batch.id ? "Existing lot selected" : "Use this existing lot"}
            </Button>
          </div>
        ))
      ) : (
        <Button
          type="button"
          variant="outline"
          disabled={frozen || batches.data.page.has_more}
          onClick={() => {
            const batch = newSupplierWorkflowId();
            setChosen(batch);
            onSelection({ product, location, batch });
          }}
        >
          {chosen ? "New destination lot selected" : "Create a destination lot for this delivery"}
        </Button>
      )}
    </div>
  );
}

function ReceivingCommandFeedback({
  commands,
  authorized,
}: {
  commands: ReceivingCommands;
  authorized: boolean;
}) {
  return (
    <div className="space-y-2">
      {commands.message && <p role={commands.retained ? "alert" : "status"}>{commands.message}</p>}
      {commands.error && authorized && (
        <RequestFailure
          message="The receiving action could not be verified."
          error={commands.error}
          retry={() =>
            commands.retained ? void commands.run(commands.retained, true) : commands.reset()
          }
        />
      )}
      {commands.retained && authorized && (
        <div className="flex gap-2">
          <Button
            type="button"
            disabled={commands.pending}
            onClick={() => void commands.run(commands.retained!)}
          >
            Retry original {commands.retained.kind}
          </Button>
          <Button
            type="button"
            variant="outline"
            disabled={commands.pending}
            onClick={() => void commands.run(commands.retained!, true)}
          >
            Recover original {commands.retained.kind}
          </Button>
        </div>
      )}
    </div>
  );
}
