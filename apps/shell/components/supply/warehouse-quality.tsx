"use client";

import { useRef, useState } from "react";
import {
  newWholesaleReceiptId,
  recordWholesaleIdentity,
  recoverWholesaleIdentity,
  recordWholesaleQuality,
  recoverWholesaleQuality,
  useApiMutation,
  useWholesaleInspection,
  wholesaleInspectionKey,
  WholesaleQualityInputContract,
  WholesaleIdentityInputContract,
  type WholesaleStockScope,
  type WholesaleReceipt,
  type WholesaleStockInspection,
  type WholesaleQualityInput,
  type WholesaleIdentityInput,
} from "@helivanta/api";
import { Field, RequestFailure, useZodForm } from "@helivanta/ui";
import { Button, Input, Textarea } from "@tesserix/web";

type Props = {
  scope: WholesaleStockScope;
  receipt: WholesaleReceipt;
  canCheck: boolean;
  canReview: boolean;
};
type Attempt =
  | { id: string; kind: "quality"; input: WholesaleQualityInput }
  | { id: string; kind: "identity"; input: WholesaleIdentityInput };
export function WarehouseQuality(props: Props) {
  const query = useWholesaleInspection(props.scope, props.receipt.id, true);
  if (query.isError)
    return (
      <RequestFailure
        message="Current stock inspection is unavailable."
        refused="Current stock inspection is unavailable."
        error={query.error}
        retry={() => void query.refetch()}
      />
    );
  if (query.isPending) return <p role="status">Loading stock inspection…</p>;
  return <StockInspection {...props} inspection={query.data} />;
}
function StockInspection({
  scope,
  receipt,
  inspection,
  canCheck,
  canReview,
}: Props & { inspection: WholesaleStockInspection }) {
  const [attempt, setAttempt] = useState<Attempt | null>(null);
  const pending = useRef<Attempt | null>(null);
  const busy = useRef(false);
  const [message, setMessage] = useState("");
  const quality = useZodForm(WholesaleQualityInputContract.pick({ decision: true, reason: true }), {
    decision: "release",
    reason: "",
  });
  const count = useZodForm(
    WholesaleIdentityInputContract.pick({
      unit_kind: true,
      expected_pack_size: true,
      expected_base_units: true,
      reason: true,
    }),
    {
      unit_kind: receipt.input.unit_code === "pack" ? "pack" : "unit",
      expected_pack_size: 0,
      expected_base_units: 0,
      reason: "",
    },
  );
  const complete = () => {
    setMessage(
      pending.current?.kind === "quality"
        ? "Quality decision recorded."
        : "Product identity and units checked.",
    );
    pending.current = null;
    busy.current = false;
    setAttempt(null);
    quality.reset();
    count.reset();
  };
  const execute = async (value: Attempt, recover: boolean) =>
    value.kind === "quality"
      ? (recover ? recoverWholesaleQuality : recordWholesaleQuality)(
          scope,
          receipt.id,
          value.id,
          value.input,
        )
      : (recover ? recoverWholesaleIdentity : recordWholesaleIdentity)(
          scope,
          receipt.id,
          value.id,
          value.input,
        );
  const save = useApiMutation((value: Attempt) => execute(value, false), {
    onSuccess: complete,
    onError: () => {
      busy.current = false;
    },
    invalidate: [wholesaleInspectionKey(scope, receipt.id)],
    suppressErrorToast: true,
  });
  const recover = useApiMutation((value: Attempt) => execute(value, true), {
    onSuccess: complete,
    onError: () => {
      busy.current = false;
    },
    invalidate: [wholesaleInspectionKey(scope, receipt.id)],
    suppressErrorToast: true,
  });
  const submit = (value: Attempt) => {
    if (busy.current || pending.current) return;
    busy.current = true;
    pending.current = value;
    setAttempt(value);
    setMessage("");
    save.mutate(value);
  };
  const frozen = !!attempt || save.isPending || recover.isPending;
  const identity = inspection.identity;
  const independent =
    receipt.actor_subject !== scope.subject && identity?.actor_subject !== scope.subject;
  return (
    <section
      aria-label={`Stock inspection ${receipt.input.lot_number}`}
      className="space-y-3 rounded-md border p-3"
    >
      <h4 className="font-medium">Inspect lot {receipt.input.lot_number}</h4>
      {message && <p role="status">{message}</p>}
      {identity ? (
        <p>
          {identity.source.brand_name} · {identity.quantity_base_units}{" "}
          {identity.source.unit_of_issue} matched
        </p>
      ) : (
        <p>Product identity and unit reconciliation are pending.</p>
      )}
      {inspection.quality && (
        <p>
          Last quality decision: {inspection.quality.status} (version {inspection.quality.version}).
          This is retained review evidence; stock commitment checks run again before dispatch.
        </p>
      )}
      {canCheck && (
        <form
          noValidate
          aria-label="Check physical stock identity"
          onSubmit={(event) => {
            void count.handleSubmit((input) =>
              submit({
                id: newWholesaleReceiptId(),
                kind: "identity",
                input: {
                  ...input,
                  expected_version: identity?.version ?? 0,
                  product_version_id: receipt.input.product_version_id,
                },
              }),
            )(event);
          }}
          className="space-y-2"
        >
          <Field
            id={`pack-${receipt.id}`}
            label="Pack size on label"
            error={count.formState.errors.expected_pack_size?.message}
          >
            <Input
              id={`pack-${receipt.id}`}
              type="number"
              disabled={frozen}
              {...count.register("expected_pack_size", { valueAsNumber: true })}
            />
          </Field>
          <Field
            id={`units-${receipt.id}`}
            label="Counted base units"
            error={count.formState.errors.expected_base_units?.message}
          >
            <Input
              id={`units-${receipt.id}`}
              type="number"
              disabled={frozen}
              {...count.register("expected_base_units", { valueAsNumber: true })}
            />
          </Field>
          <Field
            id={`count-${receipt.id}`}
            label="Count check reason"
            error={count.formState.errors.reason?.message}
          >
            <Textarea id={`count-${receipt.id}`} disabled={frozen} {...count.register("reason")} />
          </Field>
          <Button type="submit" disabled={frozen}>
            Confirm product and units
          </Button>
        </form>
      )}
      {canReview && identity && (
        <form
          noValidate
          aria-label="Independent stock quality review"
          onSubmit={(event) => {
            void quality.handleSubmit((input) =>
              submit({
                id: newWholesaleReceiptId(),
                kind: "quality",
                input: {
                  ...input,
                  expected_version: inspection.quality?.version ?? 0,
                  identity_check_id: identity.id,
                  identity_version: identity.version,
                },
              }),
            )(event);
          }}
          className="space-y-2"
        >
          <Field
            id={`quality-${receipt.id}`}
            label="Quality review reason"
            error={quality.formState.errors.reason?.message}
          >
            <Textarea
              id={`quality-${receipt.id}`}
              disabled={frozen}
              {...quality.register("reason")}
            />
          </Field>
          {!independent && (
            <p>Another authorized reviewer must release stock you received or counted.</p>
          )}
          <Button
            type="submit"
            disabled={frozen || !independent}
            onClick={() => quality.setValue("decision", "release")}
          >
            Release inspected stock
          </Button>
          <Button
            type="submit"
            variant="outline"
            disabled={frozen}
            onClick={() => quality.setValue("decision", "hold")}
          >
            Hold stock
          </Button>
        </form>
      )}
      {attempt && save.isError && (
        <p role="alert">
          The command outcome is unconfirmed. Recover its original receipt before another action.
        </p>
      )}
      {attempt && (
        <Button
          type="button"
          disabled={save.isPending || recover.isPending}
          onClick={() => {
            if (busy.current) return;
            busy.current = true;
            recover.mutate(attempt);
          }}
        >
          Recover {attempt.kind === "quality" ? "quality decision" : "product check"}
        </Button>
      )}
      {recover.isError && (
        <RequestFailure
          message="Command recovery is unavailable. Keep the original identifier and retry."
          error={recover.error}
          retry={() => {
            if (attempt) recover.mutate(attempt);
          }}
        />
      )}
    </section>
  );
}
