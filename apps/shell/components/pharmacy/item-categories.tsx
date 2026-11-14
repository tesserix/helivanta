"use client";

import { useEffect, useState } from "react";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import { Tags } from "lucide-react";
import {
  apiFetch,
  newPharmacyId,
  pharmacyItemCategoriesPath,
  pharmacyKey,
  pharmacyProductCategoryPath,
  useApiMutation,
  useApiPagedQuery,
  useApiQuery,
  type PharmacyItemCategory,
  type PharmacyProduct,
  type PharmacyProductCategory,
  type PharmacyProductCategoryDeclaration,
  type PharmacyScope,
} from "@helivanta/api";
import {
  ConfirmDialog,
  Field,
  FormSelectField,
  LoadMore,
  Panel,
  firstError,
  RequestFailure,
  useZodForm,
} from "@helivanta/ui";
import { useReportState, type FormState } from "@/lib/use-list-detail";

// Item categories (#988 slice 1): an outlet's categories and subcategories,
// and each product's category, declared by one person and approved by another.

/** The most categories the pickers load: ten pages of a hundred. */
const MAX_CATEGORY_PAGES = 10;

/**
 * Every category of the outlet, for the pickers: loaded page by page up to
 * MAX_CATEGORY_PAGES, and never retried in a loop once a page fails.
 */
export function useItemCategories(scope: PharmacyScope) {
  const list = useApiPagedQuery<PharmacyItemCategory>(
    pharmacyKey(scope, "item-categories"),
    `${pharmacyItemCategoriesPath(scope)}?limit=100`,
  );
  const { hasMore, isFetchingMore, isError, loadMore, items } = list;
  const pages = Math.ceil(items.length / 100);
  useEffect(() => {
    if (hasMore && !isFetchingMore && !isError && pages < MAX_CATEGORY_PAGES) loadMore();
  }, [hasMore, isFetchingMore, isError, pages, loadMore]);
  return list;
}

/** The outlet date after day (YYYY-MM-DD). */
function nextDay(day: string) {
  const d = new Date(`${day}T00:00:00Z`);
  d.setUTCDate(d.getUTCDate() + 1);
  return d.toISOString().slice(0, 10);
}

const categorySchema = z.object({
  code: z
    .string()
    .trim()
    .toUpperCase()
    .regex(/^[A-Z0-9][A-Z0-9_-]{0,19}$/, "Up to 20 capital letters, digits, - or _"),
  name: z.string().trim().min(1, "Enter a name").max(100, "At most 100 characters"),
  parent_id: z.string(),
});

/** An outlet's categories and subcategories, kept by its catalogue editors. */
export function ItemCategories({ scope, canEdit }: { scope: PharmacyScope; canEdit: boolean }) {
  const list = useItemCategories(scope);
  const top = list.items.filter((c) => c.parent_id === null);
  const [id, setId] = useState(() => newPharmacyId());
  const form = useZodForm(categorySchema, { code: "", name: "", parent_id: "" });
  const create = useApiMutation(
    (v: z.infer<typeof categorySchema>) =>
      apiFetch<PharmacyItemCategory>(`${pharmacyItemCategoriesPath(scope)}/${id}`, {
        method: "PUT",
        body: JSON.stringify({
          code: v.code,
          name: v.name,
          ...(v.parent_id ? { parent_id: v.parent_id } : {}),
        }),
      }),
    {
      successToast: "Category added",
      invalidate: [pharmacyKey(scope, "item-categories")],
      suppressErrorToast: true,
      onSuccess: () => {
        form.reset({ code: "", name: "", parent_id: "" });
        // A new category is a new record: never replay the last one's id.
        setId(newPharmacyId());
      },
    },
  );
  // Retiring is permanent: it is confirmed first.
  const [retiring, setRetiring] = useState<PharmacyItemCategory | null>(null);
  const retire = useApiMutation(
    (category: string) =>
      apiFetch<PharmacyItemCategory>(`${pharmacyItemCategoriesPath(scope)}/${category}/retire`, {
        method: "POST",
        body: "{}",
      }),
    {
      successToast: "Category retired",
      invalidate: [pharmacyKey(scope, "item-categories")],
      onSuccess: () => setRetiring(null),
    },
  );
  const retireButton = (c: PharmacyItemCategory) =>
    canEdit &&
    !c.retired_at && (
      <Button
        variant="outline"
        size="sm"
        aria-label={`Retire ${c.name}`}
        disabled={retire.isPending}
        onClick={() => setRetiring(c)}
      >
        Retire
      </Button>
    );
  return (
    <Panel>
      <h3 id="item-categories-heading" className="flex items-center gap-2 font-semibold">
        <Tags className="size-4" />
        Item categories
      </h3>
      <p className="mt-1 text-sm text-muted-foreground">
        This outlet&apos;s categories and their subcategories. A retired category keeps its products
        but takes no new ones.
      </p>
      {list.isError ? (
        <RequestFailure
          message="Could not load the categories."
          retry={list.retry}
          error={list.error}
        />
      ) : list.isPending ? (
        <p role="status">Loading categories…</p>
      ) : top.length === 0 ? (
        <p className="mt-3 text-sm text-muted-foreground">No categories yet.</p>
      ) : (
        <ul className="mt-3 divide-y text-sm" aria-label="Item categories">
          {top.map((c) => (
            <li key={c.id} className="py-2">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <span>
                  <span className="font-medium">{c.name}</span>{" "}
                  <span className="text-muted-foreground">{c.code}</span>
                  {c.retired_at && <span className="text-muted-foreground"> · retired</span>}
                </span>
                {retireButton(c)}
              </div>
              {list.items
                .filter((s) => s.parent_id === c.id)
                .map((s) => (
                  <div
                    key={s.id}
                    className="mt-1 flex flex-wrap items-center justify-between gap-2 pl-4 text-muted-foreground"
                  >
                    <span>
                      {s.name} · {s.code}
                      {s.retired_at && " · retired"}
                    </span>
                    {retireButton(s)}
                  </div>
                ))}
            </li>
          ))}
        </ul>
      )}
      <LoadMore
        label="Load more categories"
        hasMore={list.hasMore}
        isLoading={list.isFetchingMore}
        onClick={list.loadMore}
      />
      <ConfirmDialog
        open={retiring !== null}
        onOpenChange={(open) => !open && setRetiring(null)}
        title={retiring ? `Retire ${retiring.name}?` : "Retire this category?"}
        description="Its products keep it, but no product can newly go in it and it takes no new subcategory. Retiring cannot be undone."
        confirmLabel="Retire category"
        busy={retire.isPending}
        onConfirm={() => retiring && retire.mutate(retiring.id)}
      />
      {canEdit && (
        <form
          noValidate
          className="mt-4 grid gap-4 sm:grid-cols-4"
          onSubmit={form.handleSubmit((v) => create.mutate(v))}
        >
          <Field id="category-code" label="Code" error={form.formState.errors.code?.message}>
            <Input id="category-code" {...form.register("code")} />
          </Field>
          <Field id="category-name" label="Name" error={form.formState.errors.name?.message}>
            <Input id="category-name" {...form.register("name")} />
          </Field>
          <FormSelectField
            control={form.control}
            name="parent_id"
            id="category-parent"
            label="Under"
            options={[
              { value: "", label: "A category of its own" },
              ...top
                .filter((c) => !c.retired_at)
                .map((c) => ({ value: c.id, label: `A subcategory of ${c.name}` })),
            ]}
          />
          <div className="flex items-end">
            <Button type="submit" disabled={create.isPending}>
              Add category
            </Button>
          </div>
          {create.isError && (
            <p role="alert" className="text-sm text-destructive sm:col-span-4">
              {create.error.message}
            </p>
          )}
        </form>
      )}
    </Panel>
  );
}

function categoryName(categories: PharmacyItemCategory[], id: string | null) {
  if (!id) return "";
  return categories.find((c) => c.id === id)?.name ?? "a category no longer listed";
}

/** A product's category in force, its history, and declaring and reviewing it. */
export function ProductCategorySection({
  scope,
  product,
  today,
  canDeclare,
  canReview,
  onStateChange,
  onReviewStateChange,
}: {
  scope: PharmacyScope;
  product: PharmacyProduct;
  today: string;
  canDeclare: boolean;
  canReview: boolean;
  onStateChange?: FormState;
  onReviewStateChange?: FormState;
}) {
  const key = pharmacyKey(scope, "product-category", product.id);
  const path = pharmacyProductCategoryPath(scope, product.id);
  const current = useApiQuery<PharmacyProductCategory>(key, path);
  const categories = useItemCategories(scope);
  const all = categories.items;
  // Only the newest declaration can be approved; an older one pending is superseded.
  const newest = current.data?.history[0];
  const pending = newest?.decision === null ? newest : undefined;
  const label = (d: { category_id: string; subcategory_id: string | null }) =>
    [categoryName(all, d.category_id), categoryName(all, d.subcategory_id)]
      .filter(Boolean)
      .join(" › ");
  return (
    <section aria-labelledby="product-category-heading" className="mt-8 border-t pt-6">
      <h4 id="product-category-heading" className="mb-1 flex items-center gap-2 font-medium">
        <Tags className="size-4" />
        Category
      </h4>
      {current.isError || categories.isError ? (
        <RequestFailure
          message="Could not load the product's category."
          retry={() => {
            void current.refetch();
            categories.retry();
          }}
          error={firstError(current.error, categories.error)}
        />
      ) : !current.data || categories.isPending ? (
        <p role="status" className="text-sm text-muted-foreground">
          Loading category…
        </p>
      ) : (
        <div className="space-y-4">
          <p className="text-sm text-muted-foreground">
            {current.data.in_force
              ? `${label(current.data.in_force)}, since ${current.data.in_force.in_force_from}`
              : "No category approved yet"}
          </p>
          {canReview && pending && (
            <CategoryReviewForm
              key={`review:${pending.id}`}
              declaration={pending}
              label={label(pending)}
              path={path}
              invalidate={[key]}
              onStateChange={onReviewStateChange}
            />
          )}
          {canDeclare && product.status !== "withdrawn" && (
            <CategoryDeclarationForm
              key={`declare:${current.data.history[0]?.id ?? "none"}`}
              categories={all}
              today={today}
              path={path}
              invalidate={[key]}
              onStateChange={onStateChange}
            />
          )}
          {current.data.history.length > 0 && (
            <ol className="space-y-2" aria-label="Category history">
              {current.data.history.map((d) => (
                <li key={d.id} className="rounded-md bg-muted p-3 text-sm">
                  <p className="font-medium">{label(d)}</p>
                  <p className="mt-0.5 text-muted-foreground">
                    from {d.effective_from} · {d.reason} · declared by {d.declared_by}
                  </p>
                  <p className="mt-0.5 text-xs">
                    {d.decision === null
                      ? d.id === pending?.id
                        ? "Awaiting review"
                        : "Superseded by a newer declaration; it can only be rejected"
                      : d.decision === "approved"
                        ? `Approved by ${d.reviewed_by}, in force from ${d.in_force_from}: ${d.decision_reason}`
                        : `Rejected by ${d.reviewed_by}: ${d.decision_reason}`}
                  </p>
                </li>
              ))}
            </ol>
          )}
        </div>
      )}
    </section>
  );
}

const declarationSchema = z.object({
  category_id: z.string().min(1, "Choose a category"),
  subcategory_id: z.string(),
  effective_from: z.string().regex(/^\d{4}-\d{2}-\d{2}$/, "Enter the date it takes effect"),
  reason: z.string().trim().min(5, "Say why, at least 5 characters").max(300),
});

function CategoryDeclarationForm({
  categories,
  today,
  path,
  invalidate,
  onStateChange,
}: {
  categories: PharmacyItemCategory[];
  today: string;
  path: string;
  invalidate: unknown[][];
  onStateChange?: FormState;
}) {
  // Today's sales are already made, so a category takes effect tomorrow at the earliest.
  const form = useZodForm(
    declarationSchema.refine((v) => v.effective_from > today, {
      path: ["effective_from"],
      message: "A category takes effect from tomorrow",
    }),
    { category_id: "", subcategory_id: "", effective_from: nextDay(today), reason: "" },
  );
  // One id per declaration, so a retried submit replays rather than records twice.
  const [id] = useState(() => newPharmacyId());
  const record = useApiMutation(
    (v: z.infer<typeof declarationSchema>) =>
      apiFetch<PharmacyProductCategoryDeclaration>(`${path}/${id}`, {
        method: "PUT",
        body: JSON.stringify({
          category_id: v.category_id,
          ...(v.subcategory_id ? { subcategory_id: v.subcategory_id } : {}),
          effective_from: v.effective_from,
          reason: v.reason,
        }),
      }),
    { successToast: "Category declared", invalidate, suppressErrorToast: true },
  );
  useReportState(onStateChange, form.formState.isDirty, record.isPending);
  const chosen = form.watch("category_id");
  const live = categories.filter((c) => !c.retired_at);
  const errors = form.formState.errors;
  return (
    <form
      noValidate
      onSubmit={form.handleSubmit((v) => record.mutate(v))}
      className="space-y-4"
      aria-label="Declare the product's category"
    >
      <div className="grid gap-4 sm:grid-cols-3">
        <FormSelectField
          control={form.control}
          name="category_id"
          id="product-category"
          label="Category"
          // Another category's subcategory is never kept unseen.
          onValueChange={() => form.setValue("subcategory_id", "")}
          options={live
            .filter((c) => c.parent_id === null)
            .map((c) => ({ value: c.id, label: c.name }))}
        />
        <FormSelectField
          control={form.control}
          name="subcategory_id"
          id="product-subcategory"
          label="Subcategory"
          disabled={!chosen}
          options={[
            { value: "", label: "None" },
            ...live
              .filter((c) => chosen && c.parent_id === chosen)
              .map((c) => ({ value: c.id, label: c.name })),
          ]}
        />
        <Field id="category-from" label="In force from" error={errors.effective_from?.message}>
          <Input id="category-from" type="date" {...form.register("effective_from")} />
        </Field>
      </div>
      <Field id="category-reason" label="Why" error={errors.reason?.message}>
        <Input id="category-reason" {...form.register("reason")} />
      </Field>
      {errors.category_id && (
        <p className="text-sm text-destructive">{errors.category_id.message}</p>
      )}
      {record.isError && (
        <p role="alert" className="text-sm text-destructive">
          {record.error.message}
        </p>
      )}
      <Button type="submit" disabled={record.isPending}>
        {record.isPending ? "Declaring…" : "Declare category"}
      </Button>
    </form>
  );
}

const reviewSchema = z.object({
  reason: z.string().trim().min(1, "Say what you checked.").max(500),
});

function CategoryReviewForm({
  declaration,
  label,
  path,
  invalidate,
  onStateChange,
}: {
  declaration: PharmacyProductCategoryDeclaration;
  label: string;
  path: string;
  invalidate: unknown[][];
  onStateChange?: FormState;
}) {
  const form = useZodForm(reviewSchema, { reason: "" });
  const review = useApiMutation(
    (body: { decision: "approved" | "rejected"; reason: string }) =>
      apiFetch<PharmacyProductCategoryDeclaration>(`${path}/${declaration.id}/review`, {
        method: "POST",
        body: JSON.stringify(body),
      }),
    { successToast: "Review recorded", invalidate, suppressErrorToast: true },
  );
  useReportState(onStateChange, form.formState.isDirty, review.isPending);
  const decide = (decision: "approved" | "rejected") =>
    form.handleSubmit(({ reason }) => review.mutate({ decision, reason }));
  return (
    <form noValidate onSubmit={decide("approved")} className="space-y-3 rounded-md border p-3">
      <p className="text-sm">
        Awaiting review: {label}, from {declaration.effective_from}, declared by{" "}
        {declaration.declared_by}.
      </p>
      <Field
        id="category-review-reason"
        label="What you checked"
        error={form.formState.errors.reason?.message}
      >
        <Input id="category-review-reason" {...form.register("reason")} />
      </Field>
      {review.isError && (
        <p role="alert" className="text-sm text-destructive">
          {review.error.message}
        </p>
      )}
      <div className="flex gap-2">
        <Button type="submit" disabled={review.isPending}>
          Approve
        </Button>
        <Button
          type="button"
          variant="outline"
          disabled={review.isPending}
          onClick={decide("rejected")}
        >
          Reject
        </Button>
      </div>
    </form>
  );
}
