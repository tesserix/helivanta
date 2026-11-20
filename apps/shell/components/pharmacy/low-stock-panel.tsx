"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import {
  ApiError,
  formatZonedDateTime,
  fetchLowStockPage,
  newPharmacyId,
  pharmacyKey,
  pharmacyRoot,
  pharmacyReorderLevelInputSchema,
  readReorderLevel,
  readReorderReceipt,
  saveReorderLevel,
  useApiPagedQuery,
  usePagedQuery,
  type PharmacyReorderLevelInput,
  type PharmacyReorderLevelReceipt,
  type PharmacyScope,
  type PharmacyProduct,
} from "@helivanta/api";
import {
  Field,
  LoadMore,
  Panel,
  RequestFailure,
  SelectControl,
  useConfirmLeave,
  useLeaveGuard,
  useZodForm,
} from "@helivanta/ui";

export function LowStockPanel({
  scope,
  canConfigure,
}: {
  scope: PharmacyScope;
  canConfigure: boolean;
}) {
  const stock = usePagedQuery(pharmacyKey(scope, "low-stock"), (cursor, signal) =>
    fetchLowStockPage(scope, cursor, signal),
  );
  return (
    <div className="flex flex-col gap-5">
      <Panel aria-label="Low stock">
        <h2 className="font-semibold">Low stock</h2>
        <p className="mt-1 text-sm text-muted-foreground">
          Active products at or below their configured minimum. Available stock excludes quarantine,
          expired or blocked lots and supplier reservations. This view does not place orders.
        </p>
        {stock.error ? (
          <RequestFailure
            message="Low-stock availability could not be loaded."
            retry={stock.retry}
            error={stock.error}
          />
        ) : stock.isPending ? (
          <p role="status">Loading low-stock availability…</p>
        ) : stock.items.length === 0 ? (
          <p className="mt-4 text-sm">
            No low-stock warnings. No configured active products are at or below their minimum.
            Products without a minimum are not counted.
          </p>
        ) : (
          <div className="mt-4 overflow-x-auto">
            <table className="w-full text-left text-sm">
              <thead>
                <tr className="border-b">
                  <th className="p-2">Product</th>
                  <th className="p-2">Available</th>
                  <th className="p-2">Minimum</th>
                  <th className="p-2">Observed</th>
                </tr>
              </thead>
              <tbody>
                {stock.items.map((row) => (
                  <tr key={row.id} className="border-b">
                    <td className="p-2">
                      <span className="font-medium">{row.brand_name}</span>
                      <span className="block text-muted-foreground">{row.sku}</span>
                    </td>
                    <td className="p-2 tabular-nums">
                      {row.available_units} {row.unit_of_issue}
                    </td>
                    <td className="p-2 tabular-nums">
                      {row.minimum_units} {row.unit_of_issue}
                    </td>
                    <td className="p-2">
                      {formatZonedDateTime(row.observed_at, row.time_zone)}
                      <span className="block text-xs text-muted-foreground">
                        Expiry date: {row.local_date} · {row.time_zone}
                      </span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        <LoadMore
          hasMore={stock.hasMore}
          isLoading={stock.isFetchingMore}
          onClick={stock.loadMore}
        />
      </Panel>
      {canConfigure && (
        <LevelChooser
          key={`${scope.tenantId}:${scope.pharmacyId}:${scope.outletId}`}
          scope={scope}
        />
      )}
    </div>
  );
}

function LevelChooser({ scope }: { scope: PharmacyScope }) {
  const products = useApiPagedQuery<PharmacyProduct>(
    pharmacyKey(scope, "reorder-products"),
    `${pharmacyRoot(scope)}/catalogue/products`,
  );
  const [selected, setSelected] = useState("");
  const leave = useConfirmLeave();
  const product = products.items.find((item) => item.id === selected && item.status === "active");
  return (
    <Panel aria-label="Configure reorder level">
      <h2 className="font-semibold">Configure reorder level</h2>
      <p className="mb-4 mt-1 text-sm text-muted-foreground">
        Set a minimum for one product in this outlet, using its issue unit. Zero warns when no units
        are available.
      </p>
      {products.error ? (
        <RequestFailure
          message="Products could not be loaded."
          retry={products.retry}
          error={products.error}
        />
      ) : (
        <>
          <Field id="reorder-product" label="Product">
            <SelectControl
              id="reorder-product"
              value={selected}
              onValueChange={(value) => leave(() => setSelected(value))}
              placeholder="Select an active product"
              options={products.items
                .filter((row) => row.status === "active")
                .map((row) => ({ value: row.id, label: `${row.brand_name} · ${row.sku}` }))}
              disabled={products.isPending}
            />
          </Field>
          <LoadMore
            hasMore={products.hasMore}
            isLoading={products.isFetchingMore}
            onClick={products.loadMore}
          />
        </>
      )}
      {product && <LevelEditor key={product.id} scope={scope} product={product} />}
    </Panel>
  );
}

function LevelEditor({ scope, product }: { scope: PharmacyScope; product: PharmacyProduct }) {
  const level = useQuery({
    queryKey: pharmacyKey(scope, "reorder-level", product.id),
    queryFn: ({ signal }) => readReorderLevel(scope, product.id, signal),
    staleTime: 0,
  });
  if (level.error)
    return (
      <RequestFailure
        message="The current reorder level could not be loaded."
        retry={() => void level.refetch()}
        error={level.error}
      />
    );
  if (level.isPending) return <p role="status">Loading reorder level…</p>;
  return (
    <LevelForm
      key={`${product.version}:${level.data?.version ?? 0}`}
      scope={scope}
      product={product}
      current={level.data}
    />
  );
}

const levelFormSchema = z
  .object({ enabled: z.boolean(), minimum: z.string() })
  .superRefine((value, ctx) => {
    if (!value.enabled) return;
    const minimum = /^(0|[1-9][0-9]*)$/.test(value.minimum) ? Number(value.minimum) : NaN;
    if (
      !pharmacyReorderLevelInputSchema.safeParse({
        minimum_units: minimum,
        expected_version: 0,
        product_version: 1,
      }).success
    )
      ctx.addIssue({
        code: "custom",
        path: ["minimum"],
        message: "Enter a whole number from 0 to 1,000,000,000.",
      });
  });
type Attempt = { command: string; input: PharmacyReorderLevelInput };
function LevelForm({
  scope,
  product,
  current,
}: {
  scope: PharmacyScope;
  product: PharmacyProduct;
  current: PharmacyReorderLevelReceipt | null;
}) {
  const client = useQueryClient();
  const valid = current !== null && current.unit_of_issue === product.unit_of_issue;
  const form = useZodForm(levelFormSchema, {
    enabled: valid && current?.minimum_units !== null,
    minimum: valid && current?.minimum_units !== null ? String(current.minimum_units) : "",
  });
  const [attempt, setAttempt] = useState<Attempt | null>(null);
  const [notFound, setNotFound] = useState(false);
  const [saved, setSaved] = useState(false);
  const invalidate = () => client.invalidateQueries({ queryKey: pharmacyKey(scope) });
  const complete = async (receipt: PharmacyReorderLevelReceipt, submitted: Attempt) => {
    if (
      receipt.minimum_units !== submitted.input.minimum_units ||
      receipt.expected_version !== submitted.input.expected_version ||
      receipt.product_version !== submitted.input.product_version
    )
      throw new Error(
        "The saved result does not match this change. Keep the form open and contact support.",
      );
    setAttempt(null);
    setNotFound(false);
    setSaved(true);
    form.reset({
      enabled: receipt.minimum_units !== null,
      minimum: receipt.minimum_units === null ? "" : String(receipt.minimum_units),
    });
    await invalidate();
  };
  const save = useMutation({
    mutationFn: async (value: Attempt) => {
      const receipt = await saveReorderLevel(scope, product.id, value.command, value.input);
      await complete(receipt, value);
    },
    retry: false,
  });
  const recover = useMutation({
    mutationFn: async () => {
      if (!attempt) return;
      setNotFound(false);
      try {
        await complete(await readReorderReceipt(scope, product.id, attempt.command), attempt);
      } catch (error) {
        if (error instanceof ApiError && error.status === 404) {
          setNotFound(true);
          return;
        }
        throw error;
      }
    },
    retry: false,
  });
  const busy = save.isPending || recover.isPending;
  useLeaveGuard(
    form.formState.isDirty || attempt !== null,
    `reorder level for ${product.brand_name}`,
    busy,
  );
  const conflict = save.error instanceof ApiError && save.error.status === 409;
  const enabled = form.watch("enabled");
  return (
    <form
      noValidate
      className="mt-5 flex max-w-xl flex-col gap-4"
      onSubmit={form.handleSubmit((values) => {
        if (attempt || busy) return;
        const next = {
          command: newPharmacyId(),
          input: {
            minimum_units: values.enabled ? Number(values.minimum) : null,
            expected_version: current?.version ?? 0,
            product_version: product.version,
          },
        };
        setSaved(false);
        setAttempt(next);
        setNotFound(false);
        save.mutate(next);
      })}
    >
      {current && !valid && (
        <p role="status">
          The product unit changed. The previous minimum is inactive; save a new minimum in{" "}
          {product.unit_of_issue}.
        </p>
      )}
      <label className="flex items-center gap-2 text-sm" htmlFor="reorder-enabled">
        <input
          type="checkbox"
          id="reorder-enabled"
          {...form.register("enabled")}
          disabled={busy || attempt !== null}
        />
        Enable low-stock warning
      </label>
      <Field
        id="reorder-minimum"
        label={`Minimum (${product.unit_of_issue})`}
        error={form.formState.errors.minimum?.message}
      >
        <Input
          id="reorder-minimum"
          inputMode="numeric"
          {...form.register("minimum")}
          disabled={!enabled || busy || attempt !== null}
          aria-invalid={Boolean(form.formState.errors.minimum)}
        />
      </Field>
      {attempt && save.error && (
        <div role="alert" className="text-sm">
          <p>
            {conflict
              ? "This change conflicts with an existing command or a newer product or level. Reload the current values before editing."
              : "The save could not be confirmed. Check its saved result before changing or resending it."}
          </p>
          {conflict ? (
            <Button
              type="button"
              variant="outline"
              onClick={() => {
                setAttempt(null);
                save.reset();
                form.reset();
                void invalidate();
              }}
            >
              Reload current values
            </Button>
          ) : (
            <>
              <Button
                type="button"
                variant="outline"
                onClick={() => recover.mutate()}
                disabled={busy}
              >
                Check saved result
              </Button>
              {notFound && (
                <>
                  <p>The command was not found. You can retry the identical change.</p>
                  <Button
                    type="button"
                    onClick={() => {
                      setNotFound(false);
                      save.mutate(attempt);
                    }}
                    disabled={busy}
                  >
                    Retry original change
                  </Button>
                </>
              )}
            </>
          )}
        </div>
      )}
      {recover.error && (
        <RequestFailure
          message="The saved result could not be checked. Your original change is retained."
          retry={() => recover.mutate()}
          error={recover.error}
        />
      )}
      {saved && <p role="status">Reorder level saved.</p>}
      <Button type="submit" disabled={busy || attempt !== null || !form.formState.isDirty}>
        {busy ? "Checking…" : "Save reorder level"}
      </Button>
    </form>
  );
}
