"use client";

import { useRef, useState } from "react";
import {
  recordSourceSupplierResponse,
  recoverSourceSupplierResponse,
  type ReceivingResponseInput,
  type Shipment,
  ApiError,
  useSupplierWorkflowQuery,
  useApiMutation,
  supplierStockWorkflowKey,
  listSourceSupplierOrdersPage,
  getSourceSupplierOrder,
  getSourceSupplierReviewAccess,
  listSupplierAllocationsPage,
  listSupplierStockChoicesPage,
  listSupplierStockLocationsPage,
  reserveSupplierAllocation,
  recoverSupplierAllocation,
  decideSourceSupplierOrder,
  recoverSourceSupplierOrderCommand,
  SupplierOrderDecisionInputContract,
  type SupplierOrderDecisionInput,
  decideSupplierAllocation,
  recoverSupplierAllocationCommand,
  AllocationDecisionInputContract,
  type AllocationDecisionInput,
  type AllocationView,
  ReserveInputContract,
  newSupplierWorkflowId,
  type SupplierStockScope,
  type SupplierOrder,
  type ReserveInput,
} from "@helivanta/api";
import {
  Field,
  RequestFailure,
  useZodForm,
  useLeaveGuard,
  useConfirmLeave,
  Panel,
  SelectControl,
} from "@helivanta/ui";
import { Button, Input, Textarea } from "@tesserix/web";

import { SourceShipments } from "./source-shipments";

type Props = { scope: SupplierStockScope };
type SourceAttempt =
  | { kind: "response"; id: string; shipment: Shipment; input: ReceivingResponseInput }
  | { kind: "reserve"; id: string; input: ReserveInput }
  | { kind: "stock"; id: string; allocation: string; input: AllocationDecisionInput }
  | { kind: "order"; id: string; input: SupplierOrderDecisionInput };
export function SourceFulfilment({ scope }: Props) {
  return <SourceWorklist key={supplierStockWorkflowKey(scope).join(":")} scope={scope} />;
}
function PageButtons({
  cursor,
  next,
  onPage,
  disabled = false,
}: {
  cursor: string | null;
  next: string | null;
  onPage: (cursor: string | null) => void;
  disabled?: boolean;
}) {
  return (
    <div className="flex gap-2">
      {cursor && (
        <Button variant="outline" disabled={disabled} onClick={() => onPage(null)}>
          First page
        </Button>
      )}
      {next && (
        <Button variant="outline" disabled={disabled} onClick={() => onPage(next)}>
          Next page
        </Button>
      )}
    </div>
  );
}
function SourceWorklist({ scope }: Props) {
  const confirm = useConfirmLeave();
  const [cursor, setCursor] = useState<string | null>(null),
    [selected, setSelected] = useState<string | null>(null);
  const query = useSupplierWorkflowQuery(
    supplierStockWorkflowKey(scope, "orders", cursor ?? ""),
    (signal) => listSourceSupplierOrdersPage(scope, { cursor }, signal),
  );
  return (
    <Panel aria-label="Supplier fulfilment" className="space-y-5">
      <h3 className="font-semibold">Supplier fulfilment</h3>
      <p className="text-sm text-muted-foreground">
        Fulfil orders from this stock owner and location. Every action rechecks current Staff, order
        and stock authority.
      </p>
      {query.isError ? (
        <RequestFailure
          message="Supplier orders could not be verified."
          error={query.error}
          retry={query.refetch}
        />
      ) : query.isPending ? (
        <p role="status">Loading supplier orders…</p>
      ) : !query.data.data.length ? (
        <p>No supplier orders at this location.</p>
      ) : (
        <ul className="space-y-2">
          {query.data.data.map((order) => (
            <li
              key={order.id}
              className="flex items-center justify-between gap-3 rounded-md border p-3"
            >
              <span>
                {order.input.purchase_order_reference} · {order.status.replaceAll("_", " ")}
              </span>
              <Button
                variant="outline"
                onClick={() => confirm(() => setSelected(order.id))}
                aria-label={`Open ${order.input.purchase_order_reference}`}
              >
                Open
              </Button>
            </li>
          ))}
        </ul>
      )}
      {query.isSuccess && (
        <PageButtons
          cursor={cursor}
          next={query.data.page.next_cursor}
          onPage={(value) => {
            confirm(() => {
              setSelected(null);
              setCursor(value);
            });
          }}
        />
      )}
      {selected && <SourceOrder key={selected} scope={scope} orderId={selected} />}
    </Panel>
  );
}
function SourceOrder({ scope, orderId }: Props & { orderId: string }) {
  const query = useSupplierWorkflowQuery(
    supplierStockWorkflowKey(scope, "order", orderId),
    (signal) => getSourceSupplierOrder(scope, orderId, signal),
    true,
  );
  const [confirmed, setConfirmed] = useState(0);
  const [attempt, setAttempt] = useState<SourceAttempt | null>(null),
    [message, setMessage] = useState("");
  const uncertain = useRef(false);
  const retained = useRef<SourceAttempt | null>(null),
    working = useRef(false);
  function complete() {
    const kind = retained.current?.kind;
    uncertain.current = false;
    working.current = false;
    retained.current = null;
    setAttempt(null);
    setConfirmed((value) => value + 1);
    setMessage(
      kind === "response"
        ? "Original supplier response verified and queued for the hospital."
        : kind === "order"
          ? "Original source review verified."
          : kind === "stock"
            ? "Original allocation decision verified."
            : "Original reservation record verified.",
    );
  }
  const options = {
    onSuccess: complete,
    onError: () => {
      working.current = false;
    },
    invalidate: [[...supplierStockWorkflowKey(scope)]],
    suppressErrorToast: true,
    invalidateOnError: true,
  };
  const save = useApiMutation(
    (value: SourceAttempt): Promise<unknown> =>
      value.kind === "response"
        ? recordSourceSupplierResponse(scope, value.shipment, value.id, value.input)
        : value.kind === "reserve"
          ? reserveSupplierAllocation(scope, orderId, value.id, value.input)
          : value.kind === "order"
            ? decideSourceSupplierOrder(scope, orderId, value.id, value.input)
            : decideSupplierAllocation(scope, orderId, value.allocation, value.id, value.input),
    {
      ...options,
      onError: (error) => {
        working.current = false;
        const rejected =
          error instanceof ApiError &&
          ((error.status === 400 && error.code === "invalid_request") ||
            (error.status === 409 &&
              ["fulfilment_changed", "purchase_order_conflict"].includes(error.code)));
        // A later rejection cannot disprove an earlier commit whose response was lost.
        if (rejected && !uncertain.current) {
          retained.current = null;
          setAttempt(null);
          setMessage(error.message);
        } else {
          uncertain.current = true;
        }
      },
    },
  );
  const recovery = useApiMutation(
    (value: SourceAttempt): Promise<unknown> =>
      value.kind === "response"
        ? recoverSourceSupplierResponse(scope, value.shipment, value.id, value.input)
        : value.kind === "reserve"
          ? recoverSupplierAllocation(scope, orderId, value.id, value.input)
          : value.kind === "order"
            ? recoverSourceSupplierOrderCommand(scope, orderId, value.id, value.input)
            : recoverSupplierAllocationCommand(
                scope,
                orderId,
                value.allocation,
                value.id,
                value.input,
              ),
    options,
  );
  function begin(value: SourceAttempt) {
    if (retained.current || working.current) return;
    retained.current = value;
    uncertain.current = false;
    working.current = true;
    setMessage("");
    setAttempt(value);
    save.mutate(value);
  }
  const busy = save.isPending || recovery.isPending;
  useLeaveGuard(attempt !== null, "unresolved supplier stock command", attempt !== null);
  return (
    <section aria-label="Selected supplier order" className="space-y-4 border-t pt-4">
      {message && <p role="status">{message}</p>}
      {attempt && (
        <Panel as="div" className="space-y-3">
          <p>
            This request is unconfirmed. Check its recorded result before creating another.
            {"quantity_units" in attempt.input && <> Quantity: {attempt.input.quantity_units}.</>}
          </p>
          {(save.isError || recovery.isError) && (
            <p role="alert">{(recovery.error ?? save.error)?.message}</p>
          )}
          <Button
            disabled={busy}
            onClick={() => {
              if (working.current) return;
              working.current = true;
              recovery.mutate(attempt);
            }}
          >
            Check recorded result
          </Button>
          <Button
            variant="outline"
            disabled={busy}
            onClick={() => {
              if (working.current) return;
              working.current = true;
              save.mutate(attempt);
            }}
          >
            Retry unchanged request
          </Button>
        </Panel>
      )}
      {query.isError ? (
        <RequestFailure
          message="The current supplier order could not be verified."
          error={query.error}
          retry={query.refetch}
        />
      ) : query.isPending ? (
        <p role="status">Loading current supplier order…</p>
      ) : (
        <>
          <h4 className="font-medium">
            {query.data.input.purchase_order_reference} · {query.data.status.replaceAll("_", " ")}
          </h4>
          <p>
            Quote: {query.data.input.quote_reference}. Terms: {query.data.terms.terms_reference}.
          </p>
          <ul>
            {query.data.input.lines.map((line, index) => (
              <li key={line.id}>
                Line {index + 1}: {line.quantity_units} {line.unit_code}
              </li>
            ))}
          </ul>
          {query.data.status === "incoming" && (
            <SourceReviewAccess
              key={`review:${confirmed}`}
              scope={scope}
              order={query.data}
              frozen={attempt !== null || busy}
              onReview={(input) => begin({ kind: "order", id: newSupplierWorkflowId(), input })}
            />
          )}
          {query.data.status === "accepted" ? (
            <ReserveStock
              key={`reserve:${confirmed}`}
              scope={scope}
              order={query.data}
              frozen={attempt !== null || busy}
              onReserve={(input) => begin({ kind: "reserve", id: newSupplierWorkflowId(), input })}
            />
          ) : (
            <p>This order must be accepted before stock can be reserved.</p>
          )}
          <SourceShipments
            key={`shipments:${confirmed}`}
            scope={scope}
            order={query.data.id}
            frozen={attempt !== null || busy}
            onResponse={(shipment, input) =>
              begin({ kind: "response", id: newSupplierWorkflowId(), shipment, input })
            }
          />
          <Allocations
            key={`allocations:${confirmed}`}
            scope={scope}
            order={query.data}
            frozen={attempt !== null || busy}
            onDecision={(allocation, input) =>
              begin({ kind: "stock", id: newSupplierWorkflowId(), allocation, input })
            }
          />
        </>
      )}
    </section>
  );
}
const reservationFields = ReserveInputContract.pick({ quantity_units: true, reason: true });
function ReserveStock({
  scope,
  order,
  frozen,
  onReserve,
}: Props & { order: SupplierOrder; frozen: boolean; onReserve: (input: ReserveInput) => void }) {
  const [lineId, setLineId] = useState(order.input.lines[0]!.id);
  const line = order.input.lines.find((value) => value.id === lineId) ?? order.input.lines[0]!;
  return (
    <div className="space-y-3">
      <Field id="supplier-order-line" label="Order line">
        <SelectControl
          id="supplier-order-line"
          disabled={frozen}
          value={line.id}
          onValueChange={setLineId}
          options={order.input.lines.map((value, index) => ({
            value: value.id,
            label: `Line ${index + 1} · ${value.quantity_units} ${value.unit_code}`,
          }))}
        />
      </Field>
      <ReserveLine
        key={line.id}
        scope={scope}
        order={order}
        line={line}
        frozen={frozen}
        onReserve={onReserve}
      />
    </div>
  );
}
function ReserveLine({
  scope,
  order,
  line,
  frozen,
  onReserve,
}: Props & {
  order: SupplierOrder;
  line: SupplierOrder["input"]["lines"][number];
  frozen: boolean;
  onReserve: (input: ReserveInput) => void;
}) {
  const [cursor, setCursor] = useState<string | null>(null),
    [stock, setStock] = useState("");
  const query = useSupplierWorkflowQuery(
    supplierStockWorkflowKey(scope, "stock-choices", order.id, line.id, cursor ?? ""),
    (signal) => listSupplierStockChoicesPage(scope, order.id, line, { cursor }, signal),
  );
  const form = useZodForm(reservationFields, { quantity_units: 1, reason: "" });
  const selected = query.data?.data.find((value) => value.id === stock);
  useLeaveGuard(form.formState.isDirty, "supplier reservation draft", frozen);
  if (query.isError)
    return (
      <RequestFailure
        message="Eligible physical stock could not be verified."
        error={query.error}
        retry={query.refetch}
      />
    );
  if (query.isPending) return <p role="status">Loading physical stock choices…</p>;
  const fields = (location?: string, shelfAvailable?: number) => (
    <form
      className="space-y-3"
      onSubmit={form.handleSubmit((value) => {
        if (!selected || frozen) return;
        if (
          value.quantity_units >
          Math.min(selected.available_units, shelfAvailable ?? selected.available_units)
        ) {
          form.setError("quantity_units", {
            message: "The observed free stock is lower. Refresh and count again.",
          });
          return;
        }
        onReserve({
          ...value,
          expected_order_version: order.version,
          line_id: line.id,
          stock_id: selected.id,
          ...(location ? { stock_location_id: location } : {}),
          expires_at: new Date(Date.now() + 5 * 60_000).toISOString(),
        });
      })}
    >
      <Field
        id="supplier-reserve-units"
        label="Units to reserve"
        error={form.formState.errors.quantity_units?.message}
      >
        <Input
          id="supplier-reserve-units"
          type="number"
          min={1}
          max={Math.min(
            selected?.available_units ?? 0,
            shelfAvailable ?? selected?.available_units ?? 0,
          )}
          disabled={frozen || !selected || (scope.product === "pharmacy" && !location)}
          {...form.register("quantity_units", { valueAsNumber: true })}
        />
      </Field>
      <Field
        id="supplier-reserve-reason"
        label="Reservation reason"
        error={form.formState.errors.reason?.message}
      >
        <Textarea id="supplier-reserve-reason" disabled={frozen} {...form.register("reason")} />
      </Field>
      <p className="text-xs text-muted-foreground">
        Reserve for five minutes. Observed free stock is a hint; the owner checks it again under
        lock.
      </p>
      <Button
        type="submit"
        disabled={frozen || !selected || (scope.product === "pharmacy" && !location)}
      >
        Reserve stock
      </Button>
    </form>
  );
  return (
    <section className="space-y-3" aria-label="Reserve supplier stock">
      <Field id="supplier-physical-lot" label="Physical lot">
        <SelectControl
          id="supplier-physical-lot"
          disabled={frozen}
          value={stock}
          onValueChange={setStock}
          placeholder="Choose a released lot"
          options={query.data.data.map((value) => ({
            value: value.id,
            label: `${value.lot_number} · expires ${value.expires_on} · ${value.available_units} ${value.unit_code} observed free`,
          }))}
        />
      </Field>
      {!query.data.data.length && <p>No eligible free stock for this order line.</p>}
      <PageButtons
        cursor={cursor}
        next={query.data.page.next_cursor}
        disabled={frozen}
        onPage={(value) => {
          setStock("");
          setCursor(value);
        }}
      />
      {selected && <p>{selected.description}</p>}
      {scope.product === "pharmacy" && selected ? (
        <ReserveShelf
          key={selected.id}
          scope={scope}
          order={order}
          line={line}
          stock={selected.id}
          frozen={frozen}
        >
          {fields}
        </ReserveShelf>
      ) : (
        fields()
      )}
    </section>
  );
}
function ReserveShelf({
  scope,
  order,
  line,
  stock,
  frozen,
  children,
}: Props & {
  order: SupplierOrder;
  line: SupplierOrder["input"]["lines"][number];
  stock: string;
  frozen: boolean;
  children: (id?: string, available?: number) => React.ReactNode;
}) {
  const [cursor, setCursor] = useState<string | null>(null),
    [location, setLocation] = useState("");
  const query = useSupplierWorkflowQuery(
    supplierStockWorkflowKey(scope, "stock-shelves", order.id, line.id, stock, cursor ?? ""),
    (signal) => listSupplierStockLocationsPage(scope, order.id, line, stock, { cursor }, signal),
  );
  if (query.isError)
    return (
      <RequestFailure
        message="Sellable stock locations could not be verified."
        error={query.error}
        retry={query.refetch}
      />
    );
  if (query.isPending) return <p role="status">Loading sellable shelves…</p>;
  const selected = query.data.data.find((value) => value.id === location);
  return (
    <div className="space-y-3">
      <Field id="supplier-shelf" label="Sellable shelf">
        <SelectControl
          id="supplier-shelf"
          disabled={frozen}
          value={location}
          onValueChange={setLocation}
          placeholder="Choose a sellable shelf"
          options={query.data.data.map((value) => ({
            value: value.id,
            label: `${value.code} · ${value.name} · ${value.available_units} observed free`,
          }))}
        />
      </Field>
      <PageButtons
        cursor={cursor}
        next={query.data.page.next_cursor}
        disabled={frozen}
        onPage={(value) => {
          setLocation("");
          setCursor(value);
        }}
      />
      {children(selected?.id, selected?.available_units)}
    </div>
  );
}
function Allocations({
  scope,
  order,
  frozen,
  onDecision,
}: Props & {
  order: SupplierOrder;
  frozen: boolean;
  onDecision: (allocation: string, input: AllocationDecisionInput) => void;
}) {
  const [cursor, setCursor] = useState<string | null>(null);
  const query = useSupplierWorkflowQuery(
    supplierStockWorkflowKey(scope, "allocations", order.id, cursor ?? ""),
    (signal) => listSupplierAllocationsPage(scope, order.id, { cursor }, signal),
    true,
  );
  if (query.isError)
    return (
      <RequestFailure
        message="Current reservations could not be verified."
        error={query.error}
        retry={query.refetch}
      />
    );
  if (query.isPending) return <p role="status">Loading current reservations…</p>;
  return (
    <section aria-label="Current stock reservations" className="space-y-3">
      <h5 className="font-medium">Current reservations</h5>
      {!query.data.data.length ? (
        <p>No reservations recorded for this order.</p>
      ) : (
        <ul>
          {query.data.data.map((value) => (
            <li key={value.allocation.plan.id}>
              {value.allocation.lot_number} · {value.effective_status} ·{" "}
              {value.effective_reserved_units} held · {value.allocation.dispatched_units} dispatched
              · {value.allocation.released_units} released
              {value.effective_status === "reserved" && value.effective_reserved_units > 0 && (
                <AllocationDecision
                  key={value.allocation.plan.id}
                  view={value}
                  canDispatch={order.status === "accepted"}
                  frozen={frozen}
                  onDecision={(input) => onDecision(value.allocation.plan.id, input)}
                />
              )}
            </li>
          ))}
        </ul>
      )}
      <PageButtons
        cursor={cursor}
        next={query.data.page.next_cursor}
        disabled={frozen}
        onPage={setCursor}
      />
    </section>
  );
}

const allocationFields = AllocationDecisionInputContract.pick({
  decision: true,
  quantity_units: true,
  handoff_reference: true,
  reason: true,
});
function AllocationDecision({
  view,
  canDispatch,
  frozen,
  onDecision,
}: {
  view: AllocationView;
  canDispatch: boolean;
  frozen: boolean;
  onDecision: (input: AllocationDecisionInput) => void;
}) {
  const form = useZodForm(allocationFields, {
    decision: canDispatch ? "dispatch" : "release",
    quantity_units: view.effective_reserved_units,
    handoff_reference: "",
    reason: "",
  });
  useLeaveGuard(form.formState.isDirty, "supplier allocation decision draft", frozen);
  const decision = form.watch("decision");
  const prefix = `supplier-allocation-${view.allocation.plan.id}`;
  return (
    <form
      className="space-y-3 py-3"
      onSubmit={form.handleSubmit((value) => {
        if (frozen) return;
        if (value.quantity_units > view.effective_reserved_units) {
          form.setError("quantity_units", {
            message: "The current reservation is lower. Refresh before continuing.",
          });
          return;
        }
        if (value.decision === "dispatch" && (!canDispatch || !value.handoff_reference.trim())) {
          form.setError("handoff_reference", {
            message: "An accepted order and a handoff reference are required.",
          });
          return;
        }
        onDecision({ ...value, expected_version: view.allocation.version });
      })}
    >
      <Field id={`${prefix}-decision`} label="Allocation action">
        <SelectControl
          id={`${prefix}-decision`}
          disabled={frozen}
          value={decision}
          onValueChange={(value) =>
            form.setValue("decision", value as "dispatch" | "release", { shouldDirty: true })
          }
          options={[
            ...(canDispatch ? [{ value: "dispatch", label: "Dispatch" }] : []),
            { value: "release", label: "Release hold" },
          ]}
        />
      </Field>
      <Field
        id={`${prefix}-quantity`}
        label="Units to move"
        error={form.formState.errors.quantity_units?.message}
      >
        <Input
          id={`${prefix}-quantity`}
          type="number"
          min={1}
          max={view.effective_reserved_units}
          disabled={frozen}
          {...form.register("quantity_units", { valueAsNumber: true })}
        />
      </Field>
      <Field
        id={`${prefix}-handoff`}
        label="Handoff reference"
        error={form.formState.errors.handoff_reference?.message}
      >
        <Input
          id={`${prefix}-handoff`}
          disabled={frozen || decision === "release"}
          {...form.register("handoff_reference")}
        />
      </Field>
      <Field
        id={`${prefix}-reason`}
        label="Allocation reason"
        error={form.formState.errors.reason?.message}
      >
        <Textarea id={`${prefix}-reason`} disabled={frozen} {...form.register("reason")} />
      </Field>
      <p className="text-xs text-muted-foreground">
        A dispatch moves physical stock. Releasing a hold leaves stock with this owner.
      </p>
      <Button type="submit" disabled={frozen}>
        {decision === "dispatch" ? "Dispatch reserved stock" : "Release reserved stock"}
      </Button>
    </form>
  );
}

const sourceReviewFields = SupplierOrderDecisionInputContract.omit({
  expected_version: true,
}).extend({ decision: SupplierOrderDecisionInputContract.shape.decision.exclude(["cancel"]) });
function SourceOrderReview({
  order,
  frozen,
  onReview,
}: {
  order: SupplierOrder;
  frozen: boolean;
  onReview: (input: SupplierOrderDecisionInput) => void;
}) {
  const form = useZodForm(sourceReviewFields, { decision: "approve", reason: "" });
  useLeaveGuard(form.formState.isDirty, "supplier order review draft", frozen);
  const decision = form.watch("decision");
  return (
    <form
      className="space-y-3"
      onSubmit={form.handleSubmit((input) => {
        if (!frozen) onReview({ ...input, expected_version: order.version });
      })}
    >
      <Field id="supplier-order-decision" label="Source order decision">
        <SelectControl
          id="supplier-order-decision"
          disabled={frozen}
          value={decision}
          onValueChange={(value) =>
            form.setValue("decision", value as "approve" | "reject", { shouldDirty: true })
          }
          options={[
            { value: "approve", label: "Accept order" },
            { value: "reject", label: "Reject order" },
          ]}
        />
      </Field>
      <Field
        id="supplier-review-reason"
        label="Order review reason"
        error={form.formState.errors.reason?.message}
      >
        <Textarea id="supplier-review-reason" disabled={frozen} {...form.register("reason")} />
      </Field>
      <p className="text-xs text-muted-foreground">
        Review the referenced quote and purchase-order lines. Acceptance does not reserve or
        dispatch physical stock.
      </p>
      <Button disabled={frozen} type="submit">
        {decision === "approve" ? "Accept source order" : "Reject source order"}
      </Button>
    </form>
  );
}

function SourceReviewAccess({
  scope,
  order,
  frozen,
  onReview,
}: Props & {
  order: SupplierOrder;
  frozen: boolean;
  onReview: (input: SupplierOrderDecisionInput) => void;
}) {
  const query = useSupplierWorkflowQuery(
    supplierStockWorkflowKey(scope, "review-access", order.id),
    (signal) => getSourceSupplierReviewAccess(scope, order.id, signal),
  );
  if (query.isError) {
    if (query.error instanceof ApiError && query.error.status === 403)
      return <p>Source order review is not available for this account or scope.</p>;
    return (
      <RequestFailure
        message="Current source review access could not be verified."
        error={query.error}
        retry={query.refetch}
      />
    );
  }
  if (query.isPending) return <p role="status">Verifying current source review access…</p>;
  return <SourceOrderReview order={order} frozen={frozen} onReview={onReview} />;
}
