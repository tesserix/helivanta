"use client";

import { useEffect, useRef, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import { Plus, Search, History, Package } from "lucide-react";
import {
  ApiError,
  PAGE_SIZE,
  apiFetch,
  formatZonedDateTime,
  useApiPageQuery,
  useApiPagedQuery,
  useApiQuery,
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
  taxScheme,
  zonedDateTimeInput,
} from "@helivanta/api";
import {
  ConfirmDialog,
  DetailHeader,
  Field,
  FormSelectField,
  SelectControl,
  ListDetail,
  LoadMore,
  PageTabs,
  Pager,
  Panel,
  RequestFailure,
  useConfirmLeave,
  useFieldArray,
  useZodForm,
} from "@helivanta/ui";
import { useListDetail, useReportState, type FormState } from "@/lib/use-list-detail";
import { useStepBackWhenEmpty, useUrlPage } from "@/lib/use-url-page";
import { CurrencyAmountField, supportedCurrencies } from "./currency-amount-field";
import { ProductTaxSection, TaxBadge } from "./tax-classification";

const searchSchema = z.object({ query: z.string().trim().max(200) });

const TABS = {
  all: "All",
  active: "Active",
  discontinued: "Discontinued",
  withdrawn: "Withdrawn",
} as const;
type CatalogueTab = keyof typeof TABS;

// Keyed by the search in the URL, so it shows what the list was searched for.
function CatalogueSearch({
  initial,
  onSearch,
  children,
}: {
  initial: string;
  onSearch: (query: string) => void;
  children?: React.ReactNode;
}) {
  const search = useZodForm(searchSchema, { query: initial });
  return (
    <form
      noValidate
      role="search"
      className="flex flex-wrap items-end gap-3 border-b p-5"
      onSubmit={search.handleSubmit((values) => onSearch(values.query))}
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
      {children}
    </form>
  );
}
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
  currency: "INR",
  mrp_minor: 0,
  status: "active",
};

export function CataloguePanel({
  scope,
  writable,
  defaultCurrency,
  timeZone = "UTC",
  countryCode = "",
  canManageTax = false,
}: {
  scope: PharmacyScope;
  writable: boolean;
  defaultCurrency: string;
  timeZone?: string;
  countryCode?: string;
  /** pharmacy.tax.manage with editor access: rates are the owner's, not the catalogue editor's. */
  canManageTax?: boolean;
}) {
  const scheme = taxScheme(countryCode);
  // The tab, search and filter are the URL (#576 slice 3 D3/D4). Without a
  // tax scheme nothing can be classified, so the filter would be hidden and
  // could not be cleared: ignore it.
  const router = useRouter();
  const params = useSearchParams();
  const rawTab = params.get("tab") ?? "";
  const tab: CatalogueTab = Object.hasOwn(TABS, rawTab) ? (rawTab as CatalogueTab) : "all";
  const query = (params.get("q") ?? "").trim().slice(0, 200);
  const unclassified = !!scheme && params.get("tax") === "unclassified";
  const tax = unclassified ? "unclassified" : "";
  const url = useUrlPage({ tab, q: query, tax });
  const today = zonedDateTimeInput(new Date().toISOString(), timeZone).slice(0, 10);
  const filters = new URLSearchParams(
    Object.entries({ status: tab === "all" ? "" : tab, q: query, tax }).filter(([, v]) => v),
  ).toString();
  const products = useApiPageQuery<PharmacyProduct>(
    pharmacyKey(scope, "catalogue", "counted", tab, query, unclassified),
    `${pharmacyRoot(scope)}/catalogue/products/counted${filters ? `?${filters}` : ""}`,
    url.cursor,
  );
  const emptied = useStepBackWhenEmpty(products, url);
  // A product opens beside the list (#1303 E2): the URL holds it, and the
  // shared mechanism steps, guards and focuses as on Supplier bills.
  const pane = useListDetail({
    rows: products.items,
    name: (row) => row.brand_name,
    fallbackName: "this product",
    newName: "the new product",
    headingId: "catalogue-heading",
  });
  const { detail, paneRef, rowProps, openProps } = pane;
  // The search and the filter are not links: ask before they close an edit.
  const leave = useConfirmLeave();
  const go = (href: string) => leave(() => router.replace(href));
  const record = detail.creating ? (
    <Panel>
      <DetailHeader
        title="New product identity"
        noun="product"
        stepping={false}
        previous={{ disabled: true, onClick: () => {} }}
        next={{ disabled: true, onClick: () => {} }}
        closeLabel="Close product"
        onClose={pane.close}
      />
      <div className="mt-6">
        <ProductForm
          scope={scope}
          product={null}
          defaultCurrency={defaultCurrency}
          onSaved={(row) => pane.saved(row.id)}
          onStateChange={pane.formState("product")}
        />
      </div>
    </Panel>
  ) : detail.openId ? (
    <Panel>
      <OpenProduct
        key={detail.openId}
        id={detail.openId}
        scope={scope}
        scheme={scheme}
        writable={writable}
        canManageTax={canManageTax}
        defaultCurrency={defaultCurrency}
        timeZone={timeZone}
        today={today}
        pane={pane}
      />
    </Panel>
  ) : null;
  const compact = pane.isOpen;
  return (
    <div className="space-y-4" onClickCapture={pane.holdLinks}>
      {/* The heading, tabs and search span the page above the split, so
          opening a product narrows only the list (#1303). */}
      <Panel flush>
        <div className="flex flex-wrap items-center justify-between gap-4 border-b p-5">
          <div>
            <h3 id="catalogue-heading" tabIndex={-1} className="font-semibold outline-none">
              Product catalogue
            </h3>
            <p className="mt-1 text-sm text-muted-foreground">
              One SKU per outlet. Every change keeps its history.
            </p>
          </div>
          {writable && (
            <Button onClick={pane.create}>
              <Plus className="mr-2 size-4" />
              Add product
            </Button>
          )}
        </div>
        <PageTabs
          label="Product status"
          current={tab}
          tabs={(Object.keys(TABS) as CatalogueTab[]).map((key) => ({
            key,
            label: TABS[key],
            href: url.hrefFor({ tab: key, q: query, tax }),
          }))}
        />
        <CatalogueSearch
          key={query}
          initial={query}
          onSearch={(q) => go(url.hrefFor({ tab, q, tax }))}
        >
          {scheme && (
            <Button
              type="button"
              variant={unclassified ? "default" : "outline"}
              aria-pressed={unclassified}
              onClick={() =>
                go(url.hrefFor({ tab, q: query, tax: unclassified ? "" : "unclassified" }))
              }
            >
              Not classified
            </Button>
          )}
        </CatalogueSearch>
      </Panel>
      <ListDetail
        label={
          detail.creating ? "New product" : pane.openName ? `Product ${pane.openName}` : "Product"
        }
        backHref={detail.closeHref}
        backLabel="Back to products"
        detail={record && <div ref={paneRef}>{record}</div>}
        list={
          <Panel flush>
            {products.isError ? (
              <RequestFailure message="Could not load the catalogue." retry={products.retry} />
            ) : products.isPending ? (
              <p role="status" className="p-6">
                Loading catalogue…
              </p>
            ) : products.items.length === 0 ? (
              <div className="p-10 text-center">
                <Package className="mx-auto mb-3 size-8 text-muted-foreground" />
                <h4 className="font-semibold">
                  {unclassified
                    ? "Every product is classified"
                    : query
                      ? "No matching products"
                      : "No products yet"}
                </h4>
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
                    {...rowProps(product.id)}
                    className={`flex flex-wrap items-center justify-between gap-4 ${compact ? "px-4 py-3" : "p-5"} ${product.id === detail.openId ? "bg-(--hms-accent-dim)" : ""}`}
                  >
                    <div className="min-w-0">
                      <p className="wrap-anywhere font-medium">{product.brand_name}</p>
                      {/* Beside an open product: brand, SKU and tax (#1303 D1). */}
                      <p className="mt-1 text-sm text-muted-foreground">
                        {compact
                          ? product.sku
                          : `${product.sku} · ${product.generic_name} · ${product.pack_size} ${product.unit_of_issue}`}
                      </p>
                      {!compact && (
                        <p className="mt-1 text-xs text-muted-foreground">
                          {product.manufacturer} · {product.status} · Revision {product.version}
                        </p>
                      )}
                    </div>
                    <div className="flex flex-wrap items-center gap-3">
                      {scheme && <TaxBadge tax={product.tax} name={scheme.classification.name} />}
                      {!compact && (
                        <span className="rounded-full bg-(--hms-amber-tint) px-3 py-1 text-xs font-medium text-(--hms-amber)">
                          Pending regulatory review
                        </span>
                      )}
                      <Button
                        variant="outline"
                        {...openProps(product.id)}
                        aria-label={`Open ${product.sku}`}
                        onClick={() => pane.openRow(product.id)}
                      >
                        {compact ? "View" : `Open ${product.sku}`}
                      </Button>
                    </div>
                  </li>
                ))}
              </ul>
            )}
            {!products.isPending && !emptied && (
              <Pager
                page={url.page}
                pageSize={PAGE_SIZE}
                offset={products.offset}
                shown={products.items.length}
                total={products.total}
                previousHref={url.previousHref}
                nextHref={url.nextHref(products.nextCursor)}
                firstHref={url.first}
              />
            )}
          </Panel>
        }
      />
    </div>
  );
}

// The open product, fetched by its id so a reload or a link reopens it. Another
// outlet's or tenant's product is the same 404 as a missing one (#1303 D3).
function OpenProduct({
  id,
  scope,
  scheme,
  writable,
  canManageTax,
  defaultCurrency,
  timeZone,
  today,
  pane,
}: {
  id: string;
  scope: PharmacyScope;
  scheme: ReturnType<typeof taxScheme>;
  writable: boolean;
  canManageTax: boolean;
  defaultCurrency: string;
  timeZone: string;
  today: string;
  pane: ReturnType<typeof useListDetail<PharmacyProduct>>;
}) {
  const product = useApiQuery<PharmacyProduct>(
    pharmacyKey(scope, "product", id),
    `${pharmacyRoot(scope)}/catalogue/products/${id}`,
  );
  const heading = useRef<HTMLHeadingElement>(null);
  // Viewing focuses the heading, not an edit field (#1303 D6).
  const loaded = product.data?.id;
  useEffect(() => {
    if (loaded) heading.current?.focus({ preventScroll: true });
  }, [loaded]);
  const row = product.data;
  // The header shows whatever the pane could load: beside the list there is
  // no back link, so Close must be here even for a record not found.
  const header = (
    <DetailHeader
      title={row?.brand_name ?? "Product"}
      subtitle={row?.sku}
      headingRef={heading}
      noun="product"
      previous={{ disabled: !pane.canStep(-1), onClick: () => pane.step(-1) }}
      next={{ disabled: !pane.canStep(1), onClick: () => pane.step(1) }}
      closeLabel="Close product"
      onClose={pane.close}
    />
  );
  if (!row)
    return (
      <div className="space-y-6">
        {header}
        {product.error instanceof ApiError && product.error.status === 404 ? (
          <p role="alert" className="text-sm">
            This record was not found, or you do not have access.
          </p>
        ) : product.isError ? (
          <RequestFailure message="Could not load this product." retry={product.refetch} />
        ) : (
          <p role="status">Loading product…</p>
        )}
      </div>
    );
  return (
    <div className="space-y-6">
      {header}
      {writable && row.status !== "withdrawn" ? (
        <ProductForm
          key={`${row.id}:${row.version}`}
          scope={scope}
          product={row}
          defaultCurrency={defaultCurrency}
          onSaved={(saved) => pane.saved(saved.id)}
          onStateChange={pane.formState("product")}
        />
      ) : (
        <ProductSummary product={row} />
      )}
      <ProductTaxSection
        scope={scope}
        product={row}
        scheme={scheme}
        canManage={canManageTax}
        today={today}
        onStateChange={pane.formState("tax")}
      />
      <ProductHistory scope={scope} product={row} timeZone={timeZone} />
    </div>
  );
}

function ProductForm({
  scope,
  product,
  defaultCurrency,
  onSaved,
  onStateChange,
}: {
  scope: PharmacyScope;
  product: PharmacyProduct | null;
  defaultCurrency: string;
  onSaved: (product: PharmacyProduct) => void;
  onStateChange?: FormState;
}) {
  const [id] = useState(() => product?.id ?? newPharmacyId());
  const [confirm, setConfirm] = useState(false);
  const form = useZodForm(
    pharmacyProductFormSchema,
    product
      ? { ...pharmacyProductFieldsSchema.strip().parse(product), expected_version: product.version }
      : { ...newProduct, currency: defaultCurrency as PharmacyProductInput["currency"] },
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
      invalidate: [
        pharmacyKey(scope, "catalogue"),
        pharmacyKey(scope, "history", id),
        pharmacyKey(scope, "product", id),
      ],
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
  useReportState(onStateChange, form.formState.isDirty, save.isPending);
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
              <Panel
                key={ingredient.id}
                as="div"
                className="grid items-end gap-3 sm:grid-cols-[2fr_1fr_1fr_auto]"
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
              </Panel>
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
              <SelectControl
                id="product-currency"
                value={form.watch("currency")}
                onValueChange={(value) => {
                  form.setValue("currency", value as PharmacyProductInput["currency"]);
                  form.setValue("mrp_minor", 0);
                }}
                options={supportedCurrencies.map((value) => ({ value, label: value }))}
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
function ProductHistory({
  scope,
  product,
  timeZone,
}: {
  scope: PharmacyScope;
  product: PharmacyProduct;
  timeZone: string;
}) {
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
            <Panel key={row.id} as="li" className="text-sm">
              <p className="font-medium">
                Revision {row.version} · {row.snapshot.brand_name}
              </p>
              <p className="mt-1 text-muted-foreground">
                <time dateTime={row.created_at} title={row.created_at}>
                  {formatZonedDateTime(row.created_at, timeZone)}
                </time>{" "}
                · {row.actor_subject} · {row.snapshot.status}
              </p>
              <details className="mt-2">
                <summary className="cursor-pointer">View recorded identity</summary>
                <div className="mt-3">
                  <ProductSummary product={row.snapshot} />
                </div>
              </details>
            </Panel>
          ))}
        </ol>
      )}
      <LoadMore
        label="Load more revisions"
        hasMore={history.hasMore}
        isLoading={history.isFetchingMore}
        onClick={history.loadMore}
      />
    </section>
  );
}
