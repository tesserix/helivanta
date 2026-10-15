"use client";

import { useRef, useState } from "react";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import { Plus, Search, History, Package, ArrowLeft } from "lucide-react";
import {
  apiFetch,
  useApiPagedQuery,
  useApiMutation,
  pharmacyKey,
  pharmacyRoot,
  newPharmacyId,
  pharmacyProductFormSchema,
  pharmacyProductFieldsSchema,
  type PharmacyProduct,
  type PharmacyProductInput,
  type PharmacyRevision,
  type PharmacyScope,
} from "@helivanta/api";
import {
  Field,
  FormSelectField,
  LoadMore,
  useZodForm,
  useFieldArray,
  ConfirmDialog,
  formatDateTime,
} from "@helivanta/ui";
import { CurrencyAmountField } from "./currency-amount-field";
import { RequestFailure } from "../request-failure";

const searchSchema = z.object({ query: z.string().trim().max(200) });
const newProduct: PharmacyProductInput = {
  expected_version: 0,
  sku: "",
  brand_name: "",
  generic_name: "",
  dosage_form: "capsule",
  route: "oral",
  manufacturer: "",
  marketing_company: "",
  ingredients: [{ name: "", strength: "", unit: "mg" }],
  pack_size: 10,
  unit_of_issue: "capsule",
  currency: "",
  mrp_minor: 0,
  status: "active",
};

export function CataloguePanel({
  scope,
  writable,
  defaultCurrency,
}: {
  scope: PharmacyScope;
  writable: boolean;
  defaultCurrency: string;
}) {
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState<PharmacyProduct | null | undefined>();
  const search = useZodForm(searchSchema, { query: "" });
  const products = useApiPagedQuery<PharmacyProduct>(
    pharmacyKey(scope, "catalogue", query),
    `${pharmacyRoot(scope)}/catalogue/products?q=${encodeURIComponent(query)}`,
  );
  const editor = useRef<HTMLElement>(null);
  function reveal(product: PharmacyProduct | null) {
    setSelected(product);
    requestAnimationFrame(() => {
      editor.current?.scrollIntoView({ block: "start", behavior: "instant" });
      editor.current?.querySelector<HTMLInputElement>("input")?.focus({ preventScroll: true });
    });
  }
  return (
    <div className="space-y-6">
      <section className="overflow-hidden rounded-2xl border bg-card">
        <div className="flex flex-wrap items-center justify-between gap-4 border-b p-5">
          <div>
            <h3 className="font-semibold">Product catalogue</h3>
            <p className="mt-1 text-sm text-muted-foreground">
              One SKU per outlet. Every change keeps its history.
            </p>
          </div>
          {writable && (
            <Button onClick={() => reveal(null)}>
              <Plus className="mr-2 size-4" />
              Add product
            </Button>
          )}
        </div>
        <form
          noValidate
          className="flex flex-wrap items-end gap-3 border-b p-5"
          onSubmit={search.handleSubmit((values) => setQuery(values.query))}
        >
          <div className="min-w-48 flex-1">
            <Field
              id="catalogue-search"
              label="Search by brand, generic or SKU"
              error={search.formState.errors.query?.message}
            >
              <Input id="catalogue-search" {...search.register("query")} />
            </Field>
          </div>
          <Button type="submit" variant="outline">
            <Search className="mr-2 size-4" />
            Search catalogue
          </Button>
        </form>
        {products.isError ? (
          <RequestFailure message="Could not load the catalogue." retry={products.retry} />
        ) : products.isPending ? (
          <p role="status" className="p-6">
            Loading catalogue…
          </p>
        ) : products.items.length === 0 ? (
          <div className="p-10 text-center">
            <Package className="mx-auto mb-3 size-8 text-muted-foreground" />
            <h4 className="font-semibold">{query ? "No matching products" : "No products yet"}</h4>
            <p className="mt-2 text-sm text-muted-foreground">
              {query
                ? "Try a brand, generic name or SKU prefix."
                : "Add a private product identity to begin regulatory review."}
            </p>
          </div>
        ) : (
          <ul className="divide-y">
            {products.items.map((product) => (
              <li
                key={product.id}
                className="flex flex-wrap items-center justify-between gap-4 p-5"
              >
                <div className="min-w-0">
                  <p className="font-medium">{product.brand_name}</p>
                  <p className="mt-1 text-sm text-muted-foreground">
                    {product.sku} · {product.generic_name} · {product.pack_size}{" "}
                    {product.unit_of_issue}
                  </p>
                  <p className="mt-1 text-xs text-muted-foreground">
                    {product.manufacturer} · {product.status} · Revision {product.version}
                  </p>
                </div>
                <div className="flex flex-wrap items-center gap-3">
                  <span className="rounded-full bg-(--hms-amber-tint) px-3 py-1 text-xs font-medium text-(--hms-amber)">
                    Pending regulatory review
                  </span>
                  <Button variant="outline" onClick={() => reveal(product)}>
                    Open {product.sku}
                  </Button>
                </div>
              </li>
            ))}
          </ul>
        )}
        <LoadMore
          hasMore={products.hasMore}
          isLoading={products.isFetchingMore}
          onClick={products.loadMore}
        />
      </section>
      {selected !== undefined && (
        <section ref={editor} className="scroll-mt-6 rounded-2xl border bg-card p-6">
          <div className="mb-6 flex items-center justify-between gap-4">
            <h3 className="text-lg font-semibold">
              {selected ? selected.brand_name : "New product identity"}
            </h3>
            <Button variant="ghost" onClick={() => setSelected(undefined)}>
              <ArrowLeft className="mr-2 size-4" />
              Close details
            </Button>
          </div>
          {writable && selected?.status !== "withdrawn" ? (
            <ProductForm
              key={selected ? `${selected.id}:${selected.version}` : "new"}
              scope={scope}
              product={selected}
              defaultCurrency={defaultCurrency}
              onSaved={setSelected}
            />
          ) : (
            selected && <ProductSummary product={selected} />
          )}{" "}
          {selected && <ProductHistory scope={scope} product={selected} />}
        </section>
      )}
    </div>
  );
}

function ProductForm({
  scope,
  product,
  defaultCurrency,
  onSaved,
}: {
  scope: PharmacyScope;
  product: PharmacyProduct | null;
  defaultCurrency: string;
  onSaved: (product: PharmacyProduct) => void;
}) {
  const [id] = useState(() => product?.id ?? newPharmacyId());
  const [confirm, setConfirm] = useState(false);
  const form = useZodForm(
    pharmacyProductFormSchema,
    product
      ? { ...pharmacyProductFieldsSchema.strip().parse(product), expected_version: product.version }
      : { ...newProduct, currency: defaultCurrency },
  );
  const ingredients = useFieldArray({ control: form.control, name: "ingredients" });
  const save = useApiMutation(
    (values: PharmacyProductInput) =>
      apiFetch<PharmacyProduct>(`${pharmacyRoot(scope)}/catalogue/products/${id}`, {
        method: "PUT",
        body: JSON.stringify(values),
      }),
    {
      successToast: "Product saved",
      invalidate: [pharmacyKey(scope, "catalogue"), pharmacyKey(scope, "history", id)],
      onSuccess: (row) => {
        setConfirm(false);
        onSaved(row);
      },
      suppressErrorToast: true,
    },
  );
  const identityFields = [
    ["sku", "Local SKU"],
    ["brand_name", "Brand or preparation name"],
    ["generic_name", "Generic / composition name"],
    ["dosage_form", "Dosage form"],
    ["route", "Route"],
    ["manufacturer", "Manufacturer"],
    ["marketing_company", "Marketing company"],
  ] as const;
  const fieldErrors = form.formState.errors;
  return (
    <>
      <form
        noValidate
        onSubmit={form.handleSubmit((values) =>
          values.status === "withdrawn" ? setConfirm(true) : save.mutate(values),
        )}
        className="space-y-7"
      >
        <p className="rounded-lg bg-muted p-4 text-sm leading-6 text-muted-foreground">
          This records product identity. Drug schedules, prescription rules and tax approval require
          governed review before prescribing or dispensing.
        </p>
        <fieldset>
          <legend className="mb-4 font-medium">Product identity</legend>
          <div className="grid gap-4 sm:grid-cols-2">
            {identityFields.map(([name, label]) => (
              <Field
                key={name}
                id={`product-${name}`}
                label={label}
                error={fieldErrors[name]?.message}
              >
                <Input
                  id={`product-${name}`}
                  aria-invalid={!!fieldErrors[name]}
                  aria-describedby={fieldErrors[name] ? `product-${name}-error` : undefined}
                  {...form.register(name)}
                />
              </Field>
            ))}
          </div>
        </fieldset>
        <fieldset>
          <legend className="mb-4 font-medium">Active ingredients</legend>
          <div className="space-y-4">
            {ingredients.fields.map((ingredient, index) => (
              <div
                key={ingredient.id}
                className="grid items-end gap-3 rounded-xl border p-4 sm:grid-cols-[2fr_1fr_1fr_auto]"
              >
                <Field
                  id={`ingredient-${index}-name`}
                  label={`Ingredient ${index + 1}`}
                  error={fieldErrors.ingredients?.[index]?.name?.message}
                >
                  <Input
                    id={`ingredient-${index}-name`}
                    {...form.register(`ingredients.${index}.name`)}
                  />
                </Field>
                <Field
                  id={`ingredient-${index}-strength`}
                  label="Strength"
                  error={fieldErrors.ingredients?.[index]?.strength?.message}
                >
                  <Input
                    id={`ingredient-${index}-strength`}
                    inputMode="decimal"
                    {...form.register(`ingredients.${index}.strength`)}
                  />
                </Field>
                <Field
                  id={`ingredient-${index}-unit`}
                  label="Strength unit"
                  error={fieldErrors.ingredients?.[index]?.unit?.message}
                >
                  <Input
                    id={`ingredient-${index}-unit`}
                    {...form.register(`ingredients.${index}.unit`)}
                  />
                </Field>
                <Button
                  type="button"
                  variant="ghost"
                  disabled={ingredients.fields.length === 1}
                  onClick={() => ingredients.remove(index)}
                  aria-label={`Remove ingredient ${index + 1}`}
                >
                  Remove
                </Button>
              </div>
            ))}
          </div>
          <Button
            type="button"
            variant="outline"
            className="mt-3"
            disabled={ingredients.fields.length >= 20}
            onClick={() => ingredients.append({ name: "", strength: "", unit: "mg" })}
          >
            Add ingredient
          </Button>
        </fieldset>
        <fieldset>
          <legend className="mb-4 font-medium">Pack and commercial reference</legend>
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            <Field
              id="product-pack"
              label="Base units per pack"
              error={fieldErrors.pack_size?.message}
            >
              <Input
                id="product-pack"
                type="number"
                {...form.register("pack_size", { valueAsNumber: true })}
              />
            </Field>
            <Field
              id="product-unit"
              label="Unit of issue"
              error={fieldErrors.unit_of_issue?.message}
            >
              <Input id="product-unit" {...form.register("unit_of_issue")} />
            </Field>
            <Field
              id="product-currency"
              label="Currency code"
              error={fieldErrors.currency?.message}
            >
              <Input
                id="product-currency"
                {...form.register("currency", {
                  setValueAs: (value) => String(value).toUpperCase(),
                  onChange: () => form.setValue("mrp_minor", 0),
                })}
              />
            </Field>
            <CurrencyAmountField
              id="product-mrp"
              label="Pack MRP"
              key={form.watch("currency")}
              currency={form.watch("currency")}
              initialMinor={form.getValues("mrp_minor")}
              error={fieldErrors.mrp_minor?.message}
              onChange={(minor) =>
                form.setValue("mrp_minor", minor, { shouldValidate: true, shouldDirty: true })
              }
            />
          </div>
        </fieldset>
        <div className="max-w-xs">
          <FormSelectField
            control={form.control}
            name="status"
            id="product-status"
            label="Lifecycle"
            options={[
              { value: "active", label: "Active" },
              { value: "discontinued", label: "Discontinued" },
              { value: "withdrawn", label: "Withdrawn (permanent)" },
            ]}
          />
        </div>
        {save.isError && (
          <p role="alert" className="text-sm text-destructive">
            {save.error.message}
          </p>
        )}
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? "Saving…" : "Save product"}
        </Button>
      </form>
      <ConfirmDialog
        open={confirm}
        onOpenChange={setConfirm}
        title="Withdraw this product?"
        description="The product and its history remain available, but withdrawal is permanent and prevents further edits."
        confirmLabel="Withdraw product"
        busy={save.isPending}
        onConfirm={() => save.mutate(form.getValues())}
      />
    </>
  );
}

function ProductSummary({ product }: { product: PharmacyProduct }) {
  return (
    <dl className="grid gap-4 text-sm sm:grid-cols-2">
      {[
        ["SKU", product.sku],
        ["Generic", product.generic_name],
        ["Manufacturer", product.manufacturer],
        ["Pack", `${product.pack_size} ${product.unit_of_issue}`],
        ["Lifecycle", product.status],
        ["Regulatory review", "Pending"],
      ].map(([label, value]) => (
        <div key={label}>
          <dt className="text-muted-foreground">{label}</dt>
          <dd className="mt-1 font-medium">{value}</dd>
        </div>
      ))}
    </dl>
  );
}
function ProductHistory({ scope, product }: { scope: PharmacyScope; product: PharmacyProduct }) {
  const history = useApiPagedQuery<PharmacyRevision>(
    pharmacyKey(scope, "history", product.id),
    `${pharmacyRoot(scope)}/catalogue/products/${product.id}/revisions`,
  );
  return (
    <section className="mt-8 border-t pt-6">
      <h4 className="mb-4 flex items-center gap-2 font-medium">
        <History className="size-4" />
        Revision history
      </h4>
      {history.isError ? (
        <RequestFailure message="Could not load revision history." retry={history.retry} />
      ) : history.isPending ? (
        <p role="status">Loading revisions…</p>
      ) : (
        <ol className="space-y-3">
          {history.items.map((row) => (
            <li key={row.id} className="rounded-xl bg-muted p-4 text-sm">
              <p className="font-medium">
                Revision {row.version} · {row.snapshot.brand_name}
              </p>
              <p className="mt-1 text-muted-foreground">
                {formatDateTime(row.created_at)} · {row.actor_subject} · {row.snapshot.status}
              </p>
              <details className="mt-2">
                <summary className="cursor-pointer">View recorded identity</summary>
                <div className="mt-3">
                  <ProductSummary product={row.snapshot} />
                </div>
              </details>
            </li>
          ))}
        </ol>
      )}
      <LoadMore
        hasMore={history.hasMore}
        isLoading={history.isFetchingMore}
        onClick={history.loadMore}
      />
    </section>
  );
}
