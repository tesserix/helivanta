"use client";

import { useEffect, useState } from "react";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import {
  ApiError,
  apiFetch,
  newPharmacyId,
  pharmacyKey,
  pharmacyRoot,
  useApiMutation,
  useApiPagedQuery,
  useApiQuery,
  type PharmacyInvoiceProfile,
  type PharmacyOpenPrescriptionLine,
  type PharmacyProduct,
  type PharmacySale,
  type PharmacySaleAvailability,
  type PharmacySaleLine,
  type PharmacySaleQuote,
  type PharmacyScope,
} from "@helivanta/api";
import { Field, FormSelectField, LoadMore, Panel, RequestFailure, useZodForm } from "@helivanta/ui";
import { CurrencyAmountField, formatPharmacyMoney } from "./currency-amount-field";

/**
 * The counter sale (#1386 B-1, B-2b): a basket priced by the server exactly
 * as it will post it, paid by one tender and printed as a GST invoice. A
 * Schedule G, H or H1 line is dispensed against a recorded prescription line,
 * which the server holds to the units and dispensings the prescriber allowed.
 */
export function CounterSale({
  scope,
  canSell,
  canManageProfile,
}: {
  scope: PharmacyScope;
  canSell: boolean;
  canManageProfile: boolean;
}) {
  const profile = useApiQuery<{ profile: PharmacyInvoiceProfile | null }>(
    pharmacyKey(scope, "invoice-profile"),
    `${pharmacyRoot(scope)}/invoice-profile`,
  );
  if (profile.isError)
    return (
      <RequestFailure
        message="Could not load the outlet's invoice details."
        retry={profile.refetch}
      />
    );
  if (!profile.data) return <p role="status">Loading invoice details…</p>;
  const current = profile.data.profile;
  return (
    <div className="space-y-6">
      {current ? (
        <p className="text-sm text-muted-foreground">
          Invoices print as {current.legal_name}, numbered {current.invoice_prefix}/…
        </p>
      ) : canManageProfile ? (
        <InvoiceProfileForm scope={scope} />
      ) : (
        <Panel>
          <h3 className="font-semibold">No invoice details yet</h3>
          <p className="mt-2 text-sm text-muted-foreground">
            Ask the pharmacy owner to record the outlet&apos;s legal name, address and invoice
            prefix before selling.
          </p>
        </Panel>
      )}
      {canSell ? (
        current && <Counter scope={scope} />
      ) : (
        <p className="text-sm text-muted-foreground">
          Selling here needs the pharmacist role; you can record the outlet&apos;s invoice details.
        </p>
      )}
    </div>
  );
}

const profileSchema = z.object({
  legal_name: z.string().trim().min(1, "Enter the legal name").max(200, "At most 200 characters"),
  address: z.string().trim().min(1, "Enter the address").max(500, "At most 500 characters"),
  invoice_prefix: z.string().regex(/^[A-Z0-9]{1,4}$/, "1 to 4 capital letters or digits"),
});

function InvoiceProfileForm({ scope }: { scope: PharmacyScope }) {
  const [id] = useState(() => newPharmacyId());
  const form = useZodForm(profileSchema, { legal_name: "", address: "", invoice_prefix: "" });
  const save = useApiMutation(
    (values: z.infer<typeof profileSchema>) =>
      apiFetch(`${pharmacyRoot(scope)}/invoice-profile/${id}`, {
        method: "PUT",
        body: JSON.stringify(values),
      }),
    {
      successToast: "Invoice details recorded",
      invalidate: [pharmacyKey(scope, "invoice-profile")],
    },
  );
  return (
    <Panel>
      <form noValidate className="space-y-4" onSubmit={form.handleSubmit((v) => save.mutate(v))}>
        <h3 className="font-semibold">Invoice details</h3>
        <p className="text-sm text-muted-foreground">
          What every invoice from this outlet prints as the seller. The GSTIN comes from the
          outlet&apos;s tax registration.
        </p>
        <div className="grid gap-4 sm:grid-cols-3">
          <Field
            id="profile-name"
            label="Legal name"
            error={form.formState.errors.legal_name?.message}
          >
            <Input id="profile-name" {...form.register("legal_name")} />
          </Field>
          <Field
            id="profile-address"
            label="Address"
            error={form.formState.errors.address?.message}
          >
            <Input id="profile-address" {...form.register("address")} />
          </Field>
          <Field
            id="profile-prefix"
            label="Invoice prefix"
            error={form.formState.errors.invoice_prefix?.message}
          >
            <Input id="profile-prefix" maxLength={4} {...form.register("invoice_prefix")} />
          </Field>
        </div>
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? "Recording…" : "Record invoice details"}
        </Button>
      </form>
    </Panel>
  );
}

type Citation = { prescription_id: string; line_number: number };
type BasketLine = { product: PharmacyProduct; quantity_units: number; prescription?: Citation };

// The basket line as the quote and the sale send it.
const lineBody = (l: BasketLine) => ({
  product_id: l.product.id,
  quantity_units: l.quantity_units,
  ...(l.prescription ? { prescription: l.prescription } : {}),
});

const lineSchema = z.object({
  product_id: z.string().min(1, "Choose a product"),
  quantity_units: z
    .number({ message: "Enter a whole number of units" })
    .int("Enter a whole number of units")
    .min(1, "At least one unit")
    .max(1000000, "At most 1,000,000 units"),
  // "prescription_id:line_number", or empty for an OTC product.
  prescription: z.string(),
});

function Counter({ scope }: { scope: PharmacyScope }) {
  const products = useApiPagedQuery<PharmacyProduct>(
    pharmacyKey(scope, "counter", "products"),
    `${pharmacyRoot(scope)}/catalogue/products?limit=100`,
  );
  const [basket, setBasket] = useState<BasketLine[]>([]);
  const [quote, setQuote] = useState<PharmacySaleQuote | null>(null);
  const [posted, setPosted] = useState<PharmacySale | null>(null);
  const form = useZodForm(lineSchema, { product_id: "", quantity_units: 1, prescription: "" });
  const chosen = form.watch("product_id");
  const availability = useApiQuery<PharmacySaleAvailability>(
    pharmacyKey(scope, "counter", "availability", chosen),
    `${pharmacyRoot(scope)}/sales/availability?product_id=${encodeURIComponent(chosen)}`,
    { enabled: chosen !== "" },
  );
  const rxClass = availability.data?.regulatory_class;
  const needsPrescription = rxClass === "G" || rxClass === "H" || rxClass === "H1";
  const openLines = useApiQuery<{ lines: PharmacyOpenPrescriptionLine[] }>(
    pharmacyKey(scope, "counter", "open-lines", chosen),
    `${pharmacyRoot(scope)}/prescriptions/open-lines?product_id=${encodeURIComponent(chosen)}`,
    { enabled: needsPrescription },
  );
  // A prescription is chosen for one product; choosing a product starts over.
  useEffect(() => {
    form.setValue("prescription", "");
  }, [chosen, form]);
  // The class decides whether a prescription is needed, so a line waits for it.
  const classKnown = chosen === "" || availability.data !== undefined;
  const price = useApiMutation(
    (lines: BasketLine[]) =>
      apiFetch<PharmacySaleQuote>(`${pharmacyRoot(scope)}/sales/quote`, {
        method: "POST",
        body: JSON.stringify({ lines: lines.map(lineBody) }),
      }),
    { onSuccess: setQuote, suppressErrorToast: true },
  );
  // A changed basket is a new price: the old quote no longer applies.
  function changeBasket(next: BasketLine[]) {
    setBasket(next);
    setQuote(null);
    price.reset();
  }
  if (posted)
    return (
      <Invoice
        sale={posted}
        products={products.items}
        onNext={() => {
          setPosted(null);
          changeBasket([]);
        }}
      />
    );
  if (products.isError)
    return (
      <RequestFailure message="Could not load this outlet's products." retry={products.retry} />
    );
  if (products.isPending) return <p role="status">Loading products…</p>;
  const active = products.items.filter(
    (p) => p.status === "active" && !basket.some((b) => b.product.id === p.id),
  );
  return (
    <Panel>
      <h3 className="font-semibold">Counter sale</h3>
      <p className="mt-1 text-sm text-muted-foreground">
        A Schedule G, H or H1 medicine is dispensed against a recorded prescription. NPPA ceiling
        prices are not checked.
      </p>
      <form
        noValidate
        className="mt-4 grid gap-4 sm:grid-cols-3"
        onSubmit={form.handleSubmit((values) => {
          const product = products.items.find((p) => p.id === values.product_id);
          if (!product) return;
          const offered = (openLines.data?.lines ?? []).some(
            (l) => `${l.prescription_id}:${l.line_number}` === values.prescription,
          );
          if (needsPrescription && !offered) {
            form.setError("prescription", {
              message: `Schedule ${rxClass}: choose the prescription this is dispensed against`,
            });
            return;
          }
          const [prescriptionID, lineNumber] = values.prescription.split(":");
          changeBasket([
            ...basket,
            {
              product,
              quantity_units: values.quantity_units,
              ...(needsPrescription
                ? {
                    prescription: {
                      prescription_id: prescriptionID,
                      line_number: Number(lineNumber),
                    },
                  }
                : {}),
            },
          ]);
          form.reset({ product_id: "", quantity_units: 1, prescription: "" });
        })}
      >
        <FormSelectField
          control={form.control}
          name="product_id"
          id="counter-product"
          label="Product"
          options={active.map((p) => ({ value: p.id, label: `${p.sku} · ${p.brand_name}` }))}
        />
        <Field
          id="counter-quantity"
          label="Units"
          error={form.formState.errors.quantity_units?.message}
        >
          <Input
            id="counter-quantity"
            type="number"
            step="1"
            {...form.register("quantity_units", { valueAsNumber: true })}
          />
        </Field>
        <div className="flex items-end">
          <Button type="submit" variant="outline" disabled={!classKnown}>
            Add to basket
          </Button>
        </div>
        {chosen && availability.data && (
          <p className="text-sm text-muted-foreground sm:col-span-3" role="status">
            {availability.data.sellable_units} units sellable
            {availability.data.nearest_expiry
              ? `, nearest expiry ${availability.data.nearest_expiry}`
              : ""}
          </p>
        )}
        {chosen && needsPrescription && (
          <div className="sm:col-span-3">
            <FormSelectField
              control={form.control}
              name="prescription"
              id="counter-prescription"
              label={`Prescription (Schedule ${rxClass})`}
              options={(openLines.data?.lines ?? []).map((l) => ({
                value: `${l.prescription_id}:${l.line_number}`,
                label: `${l.patient_name} · ${l.prescriber_name} · ${l.prescribed_on} · ${l.units_remaining} units, ${l.dispensings_remaining} dispensing${l.dispensings_remaining === 1 ? "" : "s"} left${l.addresses_on_file ? "" : " · addresses missing"}`,
              }))}
            />
            {openLines.data && openLines.data.lines.length === 0 && (
              <p className="mt-1 text-sm text-muted-foreground">
                No recorded prescription for this product can still be dispensed. Record the
                prescription first.
              </p>
            )}
          </div>
        )}
      </form>
      <LoadMore
        label="Load more products"
        hasMore={products.hasMore}
        isLoading={products.isFetchingMore}
        onClick={products.loadMore}
      />
      {basket.length > 0 && (
        <ul className="mt-4 divide-y rounded-md border" aria-label="Basket">
          {basket.map((line) => (
            <li key={line.product.id} className="flex items-center justify-between p-3 text-sm">
              <span>
                {line.product.sku} · {line.product.brand_name} × {line.quantity_units}
                {line.prescription ? " · on prescription" : ""}
              </span>
              <Button
                type="button"
                variant="ghost"
                onClick={() => changeBasket(basket.filter((b) => b !== line))}
              >
                Remove {line.product.sku}
              </Button>
            </li>
          ))}
        </ul>
      )}
      {basket.length > 0 && !quote && (
        <Button className="mt-4" disabled={price.isPending} onClick={() => price.mutate(basket)}>
          {price.isPending ? "Pricing…" : "Price basket"}
        </Button>
      )}
      {price.isError && (
        <p role="alert" className="mt-3 text-sm text-destructive">
          {price.error.message}
        </p>
      )}
      {quote && (
        <>
          <SaleLines lines={quote.lines} currency={quote.currency} products={products.items} />
          <Totals sale={quote} />
          <TenderForm
            scope={scope}
            quote={quote}
            basket={basket}
            onPosted={(sale) => setPosted(sale)}
            onStale={() => setQuote(null)}
          />
        </>
      )}
    </Panel>
  );
}

function SaleLines({
  lines,
  currency,
  products,
}: {
  lines: PharmacySaleLine[];
  currency: string;
  products: PharmacyProduct[];
}) {
  const money = (minor: number) => formatPharmacyMoney(minor, currency);
  return (
    <table className="mt-4 w-full text-sm">
      <thead className="text-left text-muted-foreground">
        <tr>
          <th className="py-2">Product</th>
          <th>HSN</th>
          <th>Batch</th>
          <th>Expiry</th>
          <th>MRP per pack</th>
          <th>Units</th>
          <th>Taxable</th>
          <th>CGST + SGST</th>
          <th>Amount</th>
        </tr>
      </thead>
      <tbody>
        {lines.map((l) => (
          <tr key={l.line_number} className="border-t">
            <td className="py-2">
              {products.find((p) => p.id === l.product_id)?.sku ?? l.product_id}
            </td>
            <td>{l.classification_code}</td>
            <td>{l.lot_number}</td>
            <td>{l.expires_on}</td>
            <td>{money(l.mrp_minor)}</td>
            <td>{l.quantity_units}</td>
            <td>{money(l.taxable_minor)}</td>
            <td>
              {money(l.cgst_minor)} + {money(l.sgst_minor)} ({l.rate_ppm / 10000}%)
            </td>
            <td>{money(l.amount_minor)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function Totals({ sale }: { sale: PharmacySaleQuote | PharmacySale }) {
  const money = (minor: number) => formatPharmacyMoney(minor, sale.currency);
  return (
    <dl className="mt-4 grid max-w-sm grid-cols-2 gap-1 text-sm">
      <dt>Taxable value</dt>
      <dd>{money(sale.taxable_minor)}</dd>
      <dt>CGST</dt>
      <dd>{money(sale.cgst_minor)}</dd>
      <dt>SGST</dt>
      <dd>{money(sale.sgst_minor)}</dd>
      <dt className="font-semibold">Total</dt>
      <dd className="font-semibold">{money(sale.total_minor)}</dd>
    </dl>
  );
}

const tenderSchema = z
  .object({
    kind: z.enum(["cash", "upi", "card"]),
    tendered_minor: z.number(),
    reference: z.string().trim().max(64, "At most 64 characters"),
    total: z.number(),
    // The walk-in buyer is optional on every sale (#1386 B-2b decision 7).
    buyer_name: z.string().trim().max(200, "At most 200 characters"),
    buyer_phone: z
      .string()
      .trim()
      .refine(
        (v) => v === "" || /^\+?[0-9]{6,15}$/.test(v),
        "Use 6 to 15 digits, with an optional +",
      ),
  })
  .superRefine((v, ctx) => {
    if (
      v.kind === "cash" &&
      !(Number.isSafeInteger(v.tendered_minor) && v.tendered_minor >= v.total)
    )
      ctx.addIssue({ code: "custom", path: ["tendered_minor"], message: "At least the total" });
    if (v.kind !== "cash" && v.reference === "")
      ctx.addIssue({ code: "custom", path: ["reference"], message: "Enter the payment reference" });
  });

function TenderForm({
  scope,
  quote,
  basket,
  onPosted,
  onStale,
}: {
  scope: PharmacyScope;
  quote: PharmacySaleQuote;
  basket: BasketLine[];
  onPosted: (sale: PharmacySale) => void;
  onStale: () => void;
}) {
  const [id, setId] = useState(() => newPharmacyId());
  const form = useZodForm(tenderSchema, {
    kind: "cash",
    tendered_minor: quote.total_minor,
    reference: "",
    total: quote.total_minor,
    buyer_name: "",
    buyer_phone: "",
  });
  const kind = form.watch("kind");

  const post = useApiMutation(
    (values: z.infer<typeof tenderSchema>) =>
      apiFetch<PharmacySale>(`${pharmacyRoot(scope)}/sales/${id}`, {
        method: "PUT",
        body: JSON.stringify({
          lines: basket.map(lineBody),
          tenders: [
            values.kind === "cash"
              ? {
                  kind: "cash",
                  amount_minor: quote.total_minor,
                  tendered_minor: values.tendered_minor,
                }
              : { kind: values.kind, amount_minor: quote.total_minor, reference: values.reference },
          ],
          ...(values.buyer_name || values.buyer_phone
            ? {
                buyer: {
                  ...(values.buyer_name ? { name: values.buyer_name } : {}),
                  ...(values.buyer_phone ? { phone: values.buyer_phone } : {}),
                },
              }
            : {}),
        }),
      }),
    {
      successToast: "Sale posted",
      invalidate: [pharmacyKey(scope, "counter"), pharmacyKey(scope, "stock")],
      onSuccess: (sale) => {
        setId(newPharmacyId());
        onPosted(sale);
      },
      suppressErrorToast: true,
    },
  );
  // Only a settled refusal may start over with a new sale id. Any other
  // failure may have committed, so a retry keeps the id and replays.
  const refused =
    post.error instanceof ApiError &&
    post.error.status === 409 &&
    post.error.code === "not_sellable";
  return (
    <form
      noValidate
      className="mt-6 space-y-4 border-t pt-4"
      onSubmit={form.handleSubmit((values) => post.mutate(values))}
    >
      <h4 className="font-medium">Payment</h4>
      <div className="grid gap-4 sm:grid-cols-3">
        <FormSelectField
          control={form.control}
          name="kind"
          id="tender-kind"
          label="Paid by"
          options={[
            { value: "cash", label: "Cash" },
            { value: "upi", label: "UPI" },
            { value: "card", label: "Card" },
          ]}
        />
        {kind === "cash" ? (
          <div>
            <CurrencyAmountField
              id="tender-received"
              label="Amount received"
              currency={quote.currency}
              initialMinor={quote.total_minor}
              error={form.formState.errors.tendered_minor?.message}
              onChange={(minor) => form.setValue("tendered_minor", minor, { shouldValidate: true })}
            />
            <p className="mt-1 text-xs text-muted-foreground">
              Collect at least {formatPharmacyMoney(quote.total_minor, quote.currency)}
            </p>
          </div>
        ) : (
          <Field
            id="tender-reference"
            label="Payment reference"
            error={form.formState.errors.reference?.message}
          >
            <Input id="tender-reference" maxLength={64} {...form.register("reference")} />
          </Field>
        )}
      </div>
      <div className="grid gap-4 sm:grid-cols-3">
        <Field
          id="buyer-name"
          label="Buyer name (optional)"
          error={form.formState.errors.buyer_name?.message}
        >
          <Input id="buyer-name" maxLength={200} {...form.register("buyer_name")} />
        </Field>
        <Field
          id="buyer-phone"
          label="Buyer phone (optional)"
          error={form.formState.errors.buyer_phone?.message}
        >
          <Input id="buyer-phone" inputMode="tel" {...form.register("buyer_phone")} />
        </Field>
      </div>
      {post.isError && (
        <div role="alert" className="text-sm text-destructive">
          <p>{post.error.message}</p>
          {refused ? (
            <Button type="button" variant="outline" className="mt-2" onClick={onStale}>
              Price the basket again
            </Button>
          ) : (
            <p className="mt-1">
              The sale may have been posted. Try again: the same sale is never posted twice.
            </p>
          )}
        </div>
      )}
      <Button type="submit" disabled={post.isPending}>
        {post.isPending ? "Posting…" : post.isError && !refused ? "Try again" : "Post sale"}
      </Button>
    </form>
  );
}

function Invoice({
  sale,
  products,
  onNext,
}: {
  sale: PharmacySale;
  products: PharmacyProduct[];
  onNext: () => void;
}) {
  const money = (minor: number) => formatPharmacyMoney(minor, sale.currency);
  return (
    <Panel>
      <article aria-label={`Invoice ${sale.invoice_number}`}>
        <h3 className="font-semibold">Tax invoice {sale.invoice_number}</h3>
        <p className="text-sm">
          {sale.seller_legal_name}, {sale.seller_address}
        </p>
        <p className="text-sm">
          GSTIN {sale.seller_gstin} · Place of supply {sale.place_of_supply} · Date {sale.sale_date}
        </p>
        <SaleLines lines={sale.lines} currency={sale.currency} products={products} />
        <Totals sale={sale} />
        {sale.tenders.map((t) => (
          <p key={t.tender_number} className="mt-2 text-sm">
            Paid by {t.kind.toUpperCase()} {money(t.amount_minor)}
            {t.kind === "cash" && t.tendered_minor !== null && t.change_minor !== null
              ? ` · received ${money(t.tendered_minor)} · change ${money(t.change_minor)}`
              : ` · reference ${t.reference}`}
          </p>
        ))}
        {(sale.buyer_name || sale.buyer_phone) && (
          <p className="mt-2 text-sm">
            Buyer {[sale.buyer_name, sale.buyer_phone].filter(Boolean).join(" · ")}
          </p>
        )}
        <p className="mt-3 text-xs text-muted-foreground">
          Schedule classes are as declared by the outlet. NPPA ceiling prices were not checked.
        </p>
      </article>
      {sale.lines.some((l) => l.prescription_id) && (
        <p role="note" className="mt-4 rounded-md border p-3 text-sm print:hidden">
          Endorse the prescription: note {sale.seller_legal_name}, {sale.seller_address} and{" "}
          {sale.sale_date} on it before returning it.
        </p>
      )}
      <div className="mt-4 flex gap-2 print:hidden">
        <Button type="button" variant="outline" onClick={() => window.print()}>
          Print invoice
        </Button>
        <Button type="button" onClick={onNext}>
          New sale
        </Button>
      </div>
    </Panel>
  );
}
