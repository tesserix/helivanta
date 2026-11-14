"use client";

import { useCallback, useEffect, useState } from "react";
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
  type PharmacyDiscountCeiling,
  type PharmacyInvoiceProfile,
  type PharmacyOpenPrescriptionLine,
  type PharmacyProduct,
  type PharmacySale,
  type PharmacySaleAvailability,
  type PharmacySaleCharge,
  type PharmacySaleLine,
  type PharmacySaleQuote,
  type PharmacyScope,
  type PharmacyServiceCharge,
} from "@helivanta/api";
import { Field, FormSelectField, LoadMore, Panel, RequestFailure, useZodForm } from "@helivanta/ui";
import { CounterPatientLookup, CounterVisit, type CounterPatient } from "./counter-patient";
import { CounterAdmissionLookup, type CounterAdmission } from "./counter-admission";
import { CurrencyAmountField, formatPharmacyMoney } from "./currency-amount-field";
import { VerifiedUpi } from "./verified-upi";

/**
 * The counter sale (#1386 B-1, B-2b): a basket priced by the server exactly
 * as it will post it, paid by one tender and printed as a GST invoice. A
 * Schedule G, H or H1 line is dispensed against a recorded prescription line,
 * which the server holds to the units and dispensings the prescriber allowed.
 */
export function CounterSale({
  scope,
  timeZone,
  canSell,
  canManageProfile,
  canDiscount = false,
  canManageDiscount = false,
  canRefund = false,
}: {
  scope: PharmacyScope;
  /** The outlet's time zone, for when a visit opened. */
  timeZone?: string;
  canSell: boolean;
  canManageProfile: boolean;
  /** Holds pharmacy.sale.discount, which a tenant administrator assigns (#1386 B-3). */
  canDiscount?: boolean;
  /** Records the outlet's discount ceiling. */
  canManageDiscount?: boolean;
  canRefund?: boolean;
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
        error={profile.error}
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
      {canManageDiscount && <DiscountCeilingForm scope={scope} />}
      {canSell ? (
        current && (
          <Counter
            scope={scope}
            timeZone={timeZone}
            canDiscount={canDiscount}
            canRefund={canRefund}
          />
        )
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
type BasketLine = {
  product: PharmacyProduct;
  quantity_units: number;
  prescription?: Citation;
  discount_bps?: number;
};

// The basket line as the quote and the sale send it.
const lineBody = (l: BasketLine) => ({
  product_id: l.product.id,
  quantity_units: l.quantity_units,
  ...(l.prescription ? { prescription: l.prescription } : {}),
  ...(l.discount_bps ? { discount_bps: l.discount_bps } : {}),
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
  // Percent off, as typed; empty is no discount (#1386 B-3).
  discount: z
    .string()
    .trim()
    .refine(
      (v) => v === "" || /^(100(\.0{1,2})?|[0-9]{1,2}(\.[0-9]{1,2})?)$/.test(v),
      "Enter a percentage from 0 to 100, at most two decimals",
    ),
});

function Counter({
  scope,
  timeZone,
  canDiscount,
  canRefund,
}: {
  scope: PharmacyScope;
  timeZone?: string;
  canDiscount: boolean;
  canRefund: boolean;
}) {
  const ceiling = useApiQuery<{ ceiling: PharmacyDiscountCeiling | null }>(
    pharmacyKey(scope, "counter", "discount-ceiling"),
    `${pharmacyRoot(scope)}/discount-ceiling`,
    { enabled: canDiscount },
  );
  const maxBps = ceiling.data?.ceiling?.max_bps ?? 0;
  const products = useApiPagedQuery<PharmacyProduct>(
    pharmacyKey(scope, "counter", "products"),
    `${pharmacyRoot(scope)}/catalogue/products?limit=100`,
  );
  const [basket, setBasket] = useState<BasketLine[]>([]);
  const [quote, setQuote] = useState<PharmacySaleQuote | null>(null);
  const [posted, setPosted] = useState<PharmacySale | null>(null);
  const [paymentHeld, setPaymentHeld] = useState(false);
  // A post whose outcome is unknown may have committed: until a settled
  // answer, nothing may change the sale, or a retry would be a second sale (#1533).
  const [uncertain, setUncertain] = useState(false);
  const held = paymentHeld || uncertain;
  // Service charges (#1388): their own lines, all of one channel per sale.
  const services = useApiQuery<{ items: PharmacyServiceCharge[] }>(
    pharmacyKey(scope, "counter", "service-charges"),
    `${pharmacyRoot(scope)}/service-charges`,
  );
  const offered = (services.data?.items ?? []).filter((c) => c.in_force?.status === "available");
  const [picked, setPicked] = useState<string[]>([]);
  // A charge that stops being offered (a new version awaits review) drops out
  // of the selection rather than lingering unseen and unremovable.
  const chargeIds = picked.filter((id) => offered.some((c) => c.id === id));
  const chargeChannel = offered.find((c) => c.id === chargeIds[0])?.in_force?.channel ?? null;
  const form = useZodForm(lineSchema, {
    product_id: "",
    quantity_units: 1,
    prescription: "",
    discount: "",
  });
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
        body: JSON.stringify({
          lines: lines.map(lineBody),
          ...(chargeIds.length > 0
            ? { charges: chargeIds.map((id) => ({ charge_id: id })), channel: chargeChannel }
            : {}),
        }),
      }),
    { onSuccess: setQuote, suppressErrorToast: true },
  );
  // A changed basket is a new price: the old quote no longer applies.
  function changeBasket(next: BasketLine[]) {
    if (held) return;
    setBasket(next);
    setQuote(null);
    price.reset();
  }
  function toggleCharge(id: string) {
    if (held) return;
    setPicked(chargeIds.includes(id) ? chargeIds.filter((c) => c !== id) : [...chargeIds, id]);
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
          setPicked([]);
          changeBasket([]);
        }}
      />
    );
  if (products.isError)
    return (
      <RequestFailure
        message="Could not load this outlet's products."
        retry={products.retry}
        error={products.error}
      />
    );
  if (products.isPending) return <p role="status">Loading products…</p>;
  const active = products.items.filter(
    (p) => p.status === "active" && !basket.some((b) => b.product.id === p.id),
  );
  return (
    <Panel>
      <h3 className="font-semibold">Counter sale</h3>
      <p className="mt-1 text-sm text-muted-foreground">
        A Schedule G, H or H1 medicine is dispensed against a recorded prescription. A product with
        an NPPA ceiling in force is priced within it; one with no price control declared sells at
        its MRP.
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
          const discountBps =
            values.discount === "" ? 0 : Math.round(Number(values.discount) * 100);
          if (discountBps > maxBps) {
            form.setError("discount", {
              message: `This outlet's ceiling is ${maxBps / 100}%`,
            });
            return;
          }
          const [prescriptionID, lineNumber] = values.prescription.split(":");
          changeBasket([
            ...basket,
            {
              product,
              quantity_units: values.quantity_units,
              ...(discountBps > 0 ? { discount_bps: discountBps } : {}),
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
          form.reset({ product_id: "", quantity_units: 1, prescription: "", discount: "" });
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
        {canDiscount && (
          <Field
            id="counter-discount"
            label={`Discount % (up to ${maxBps / 100}%)`}
            error={form.formState.errors.discount?.message}
          >
            <Input id="counter-discount" inputMode="decimal" {...form.register("discount")} />
          </Field>
        )}
        <div className="flex items-end">
          <Button type="submit" variant="outline" disabled={!classKnown || held}>
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
                {line.discount_bps ? ` · ${line.discount_bps / 100}% off` : ""}
              </span>
              <Button
                type="button"
                variant="ghost"
                disabled={held}
                onClick={() => changeBasket(basket.filter((b) => b !== line))}
              >
                Remove {line.product.sku}
              </Button>
            </li>
          ))}
        </ul>
      )}
      {basket.length > 0 && services.isError && (
        <div className="mt-4">
          <RequestFailure
            message="Could not load the service charges; none can be added to this sale."
            retry={services.refetch}
            error={services.error}
            refused="No service charges can be added: you don't have access to them."
          />
        </div>
      )}
      {basket.length > 0 && offered.length > 0 && (
        <fieldset className="mt-4 space-y-2 text-sm">
          <legend className="font-medium">Service charges</legend>
          {offered.map((c) => {
            const v = c.in_force!;
            // One sale is one channel: ward charges bill an admission, counter charges do not.
            const otherChannel = chargeChannel !== null && v.channel !== chargeChannel;
            return (
              <label key={c.id} className="flex items-center gap-2">
                <input
                  type="checkbox"
                  checked={chargeIds.includes(c.id)}
                  disabled={held || otherChannel}
                  onChange={() => toggleCharge(c.id)}
                />
                {v.name} · {formatPharmacyMoney(v.amount_minor, basket[0].product.currency)} ·{" "}
                {v.channel === "ward_delivery" ? "ward delivery" : "counter"}
              </label>
            );
          })}
        </fieldset>
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
          <SaleCharges charges={quote.charges} currency={quote.currency} />
          <Totals sale={quote} />
          <TenderForm
            scope={scope}
            timeZone={timeZone}
            quote={quote}
            basket={basket}
            chargeChannel={chargeChannel}
            onPosted={(sale) => {
              setPaymentHeld(false);
              setUncertain(false);
              setPosted(sale);
            }}
            onStale={() => setQuote(null)}
            canRefund={canRefund}
            paymentHeld={paymentHeld}
            onPaymentHold={setPaymentHeld}
            onUncertain={setUncertain}
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
  // Decision 5 and CGST s.15(3)(a): a discount is shown on the line it reduces.
  const discounted = lines.some((l) => l.discount_minor > 0);
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
          {discounted && <th>List</th>}
          {discounted && <th>Discount</th>}
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
            <td>
              {money(l.mrp_minor)}
              {l.price_limit === "ceiling" && l.ceiling_minor !== null && (
                <span className="block text-xs text-muted-foreground">
                  NPPA ceiling {money(l.ceiling_minor)}/unit + GST
                </span>
              )}
            </td>
            <td>{l.quantity_units}</td>
            {discounted && <td>{money(l.list_amount_minor)}</td>}
            {discounted && (
              <td>
                {l.discount_minor > 0
                  ? `${l.discount_bps / 100}% · ${money(l.discount_minor)}`
                  : "—"}
              </td>
            )}
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

function SaleCharges({ charges, currency }: { charges: PharmacySaleCharge[]; currency: string }) {
  if (charges.length === 0) return null;
  const money = (minor: number) => formatPharmacyMoney(minor, currency);
  return (
    <>
      <table className="mt-2 w-full text-sm" aria-label="Service charges">
        <thead className="text-left text-muted-foreground">
          <tr>
            <th className="py-2">Service charge</th>
            <th>SAC</th>
            <th>Taxable</th>
            <th>CGST + SGST</th>
            <th>Amount</th>
          </tr>
        </thead>
        <tbody>
          {charges.map((c) => (
            <tr key={c.charge_number} className="border-t">
              <td className="py-2">{c.name}</td>
              <td>{c.sac_code ?? "With the medicines"}</td>
              <td>{money(c.taxable_minor)}</td>
              <td>
                {money(c.cgst_minor)} + {money(c.sgst_minor)} ({c.rate_ppm / 10000}%)
              </td>
              <td>{money(c.amount_minor)}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {charges.some((c) => c.tax_treatment === "principal_supply") && (
        <p className="mt-1 text-xs text-muted-foreground">
          A charge marked &quot;With the medicines&quot; is part of their supply and taxed at their
          rate (CGST Act s.8(a)).
        </p>
      )}
    </>
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
    kind: z.enum(["cash", "upi_verified", "upi", "card"]),
    tendered_minor: z.number(),
    reference: z.string().trim().max(64, "At most 64 characters"),
    // The paid counter payment, set by VerifiedUpi only once it is paid.
    payment_request_id: z.string(),
    total: z.number(),
    discounted: z.boolean(),
    discount_reason: z.string().trim().max(200, "At most 200 characters"),
    // A sale names a registered patient or an optional walk-in buyer (#1386 B-2c).
    // Or bills the sale to the admission in a bed (#1387), collecting nothing.
    customer: z.enum(["walk_in", "patient", "admission"]),
    patient_id: z.string(),
    // The patient's OPD visit the sale serves (#987); empty is none.
    encounter_id: z.string(),
    occupancy_id: z.string(),
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
    if (v.customer === "admission") {
      if (v.occupancy_id === "")
        ctx.addIssue({
          code: "custom",
          path: ["occupancy_id"],
          message: "Find the admission and confirm the patient",
        });
      // Billed to the account: no tender is taken at the counter.
      if (v.discounted && v.discount_reason.length < 5)
        ctx.addIssue({
          code: "custom",
          path: ["discount_reason"],
          message: "Give the reason for the discount, at least 5 characters",
        });
      return;
    }
    if (
      v.kind === "cash" &&
      !(Number.isSafeInteger(v.tendered_minor) && v.tendered_minor >= v.total)
    )
      ctx.addIssue({ code: "custom", path: ["tendered_minor"], message: "At least the total" });
    if (v.kind === "upi_verified" && v.payment_request_id === "")
      ctx.addIssue({
        code: "custom",
        path: ["payment_request_id"],
        message: "Wait for the UPI payment to be confirmed",
      });
    if ((v.kind === "upi" || v.kind === "card") && v.reference === "")
      ctx.addIssue({ code: "custom", path: ["reference"], message: "Enter the payment reference" });
    if (v.customer === "patient" && v.patient_id === "")
      ctx.addIssue({
        code: "custom",
        path: ["patient_id"],
        message: "Find the patient and confirm it is them",
      });
    if (v.discounted && v.discount_reason.length < 5)
      ctx.addIssue({
        code: "custom",
        path: ["discount_reason"],
        message: "Give the reason for the discount, at least 5 characters",
      });
  });

function TenderForm({
  scope,
  timeZone,
  quote,
  basket,
  chargeChannel,
  onPosted,
  onStale,
  canRefund,
  paymentHeld,
  onPaymentHold,
  onUncertain,
}: {
  scope: PharmacyScope;
  timeZone?: string;
  quote: PharmacySaleQuote;
  basket: BasketLine[];
  /** Ward-delivery charges bill an admission; counter charges never do. */
  chargeChannel: "counter" | "ward_delivery" | null;
  onPosted: (sale: PharmacySale) => void;
  onStale: () => void;
  canRefund: boolean;
  paymentHeld: boolean;
  onPaymentHold: (held: boolean) => void;
  /** True while a post's outcome is unknown; the counter then holds the basket. */
  onUncertain: (uncertain: boolean) => void;
}) {
  const [id, setId] = useState(() => newPharmacyId());
  const form = useZodForm(tenderSchema, {
    kind: "cash",
    tendered_minor: quote.total_minor,
    reference: "",
    payment_request_id: "",
    total: quote.total_minor,
    customer: chargeChannel === "ward_delivery" ? "admission" : "walk_in",
    patient_id: "",
    encounter_id: "",
    occupancy_id: "",
    buyer_name: "",
    buyer_phone: "",
    discounted: basket.some((l) => l.discount_bps),
    discount_reason: "",
  });
  const kind = form.watch("kind");
  const customer = form.watch("customer");
  const [patient, setPatient] = useState<CounterPatient | null>(null);
  const [admission, setAdmission] = useState<CounterAdmission | null>(null);
  const chooseAdmission = (next: CounterAdmission | null) => {
    setAdmission(next);
    form.setValue("occupancy_id", next?.occupancy_id ?? "", { shouldValidate: false });
    if (next) form.clearErrors("occupancy_id");
  };
  const choosePatient = (next: CounterPatient | null) => {
    setPatient(next);
    form.setValue("patient_id", next?.patient_id ?? "", { shouldValidate: false });
    // Another patient's visit is never carried over.
    form.setValue("encounter_id", "", { shouldValidate: false });
    if (next) form.clearErrors("patient_id");
  };
  const { setValue, clearErrors } = form;
  const onPaid = useCallback(
    (paymentRequestId: string | null) => {
      setValue("payment_request_id", paymentRequestId ?? "", { shouldValidate: false });
      if (paymentRequestId) clearErrors("payment_request_id");
    },
    [setValue, clearErrors],
  );

  const post = useApiMutation(
    (values: z.infer<typeof tenderSchema>) =>
      apiFetch<PharmacySale>(`${pharmacyRoot(scope)}/sales/${id}`, {
        method: "PUT",
        body: JSON.stringify({
          lines: basket.map(lineBody),
          // The versions the quote priced: terms approved since are refused, not billed unseen.
          ...(quote.charges.length > 0
            ? {
                charges: quote.charges.map((c) => ({
                  charge_id: c.charge_id,
                  version_id: c.version_id,
                })),
              }
            : {}),
          tenders: [
            values.customer === "admission"
              ? { kind: "admission_account", amount_minor: quote.total_minor }
              : values.kind === "cash"
                ? {
                    kind: "cash",
                    amount_minor: quote.total_minor,
                    tendered_minor: values.tendered_minor,
                  }
                : values.kind === "upi_verified"
                  ? {
                      kind: "upi_verified",
                      amount_minor: quote.total_minor,
                      payment_request_id: values.payment_request_id,
                    }
                  : {
                      kind: values.kind,
                      amount_minor: quote.total_minor,
                      reference: values.reference,
                    },
          ],
          ...(values.discounted ? { discount_reason: values.discount_reason } : {}),
          ...(values.customer === "patient" && patient
            ? {
                patient: { patient_id: patient.patient_id, hospital_id: patient.hospital_id },
                ...(values.encounter_id
                  ? { encounter: { encounter_id: values.encounter_id } }
                  : {}),
              }
            : {}),
          ...(values.customer === "admission" && admission
            ? { admission: { occupancy_id: admission.occupancy_id } }
            : {}),
          ...(values.customer === "walk_in" && (values.buyer_name || values.buyer_phone)
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
      invalidate: [
        pharmacyKey(scope, "counter"),
        pharmacyKey(scope, "stock"),
        pharmacyKey(scope, "counter-payments"),
      ],
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
  const uncertain = post.isError && !refused;
  useEffect(() => onUncertain(uncertain), [uncertain, onUncertain]);
  // What was first sent; a retry resends exactly it, so the server replays
  // the committed sale or posts it once, never a different sale under its id.
  const [sent, setSent] = useState<z.infer<typeof tenderSchema> | null>(null);
  return (
    <form
      noValidate
      className="mt-6 space-y-4 border-t pt-4"
      onSubmit={(event) => {
        if (uncertain && sent) {
          event.preventDefault();
          post.mutate(sent);
          return;
        }
        return form.handleSubmit((values) => {
          setSent(values);
          post.mutate(values);
        })(event);
      }}
    >
      <h4 className="font-medium">Payment</h4>
      {uncertain && (
        <p role="status" className="text-sm">
          Whether this sale was posted is not known yet. Until it is, this sale cannot be changed;
          try again to find out.
        </p>
      )}
      <fieldset disabled={uncertain} className="space-y-4">
        {customer === "admission" ? (
          <p className="text-sm">
            Billed to the admission account. Nothing is collected at the counter.
          </p>
        ) : (
          <div className="grid gap-4 sm:grid-cols-3">
            <FormSelectField
              control={form.control}
              name="kind"
              id="tender-kind"
              label="Paid by"
              disabled={paymentHeld || post.isPending}
              options={[
                { value: "cash", label: "Cash" },
                { value: "upi_verified", label: "UPI (verified)" },
                { value: "upi", label: "UPI (reference only)" },
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
                  onChange={(minor) =>
                    form.setValue("tendered_minor", minor, { shouldValidate: true })
                  }
                />
                <p className="mt-1 text-xs text-muted-foreground">
                  Collect at least {formatPharmacyMoney(quote.total_minor, quote.currency)}
                </p>
              </div>
            ) : kind === "upi_verified" ? (
              <div className="sm:col-span-2">
                <VerifiedUpi
                  scope={scope}
                  amountMinor={quote.total_minor}
                  currency={quote.currency}
                  onPaid={onPaid}
                  onHold={onPaymentHold}
                  canRefund={canRefund}
                />
                {form.formState.errors.payment_request_id && (
                  <p className="mt-1 text-sm text-destructive">
                    {form.formState.errors.payment_request_id.message}
                  </p>
                )}
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
        )}
        {form.getValues("discounted") && (
          <Field
            id="discount-reason"
            label="Reason for the discount"
            error={form.formState.errors.discount_reason?.message}
          >
            <Input id="discount-reason" maxLength={200} {...form.register("discount_reason")} />
          </Field>
        )}
        <FormSelectField
          control={form.control}
          name="customer"
          id="sale-customer"
          label="Customer"
          disabled={paymentHeld || post.isPending}
          options={[
            { value: "walk_in", label: "Walk-in buyer" },
            { value: "patient", label: "Registered patient" },
            { value: "admission", label: "Bill to admission" },
          ].filter((o) =>
            chargeChannel === "ward_delivery"
              ? o.value === "admission"
              : chargeChannel === "counter"
                ? o.value !== "admission"
                : true,
          )}
        />
        {paymentHeld && (
          <p role="status" className="text-sm">
            Finish the sale or record the completed refund before changing the customer or tender.
            Cancel an unpaid QR and wait for confirmation before changing them.
          </p>
        )}
        {customer === "admission" ? (
          <div>
            <CounterAdmissionLookup scope={scope} value={admission} onChange={chooseAdmission} />
            {form.formState.errors.occupancy_id && (
              <p className="mt-1 text-sm text-destructive">
                {form.formState.errors.occupancy_id.message}
              </p>
            )}
          </div>
        ) : customer === "patient" ? (
          <div>
            <CounterPatientLookup scope={scope} value={patient} onChange={choosePatient} />
            {form.formState.errors.patient_id && (
              <p className="mt-1 text-sm text-destructive">
                {form.formState.errors.patient_id.message}
              </p>
            )}
            {patient && (
              <CounterVisit
                key={patient.patient_id}
                scope={scope}
                patient={patient}
                timeZone={timeZone}
                value={form.watch("encounter_id")}
                onChange={(next) => form.setValue("encounter_id", next, { shouldValidate: false })}
              />
            )}
          </div>
        ) : (
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
        )}
      </fieldset>
      {post.isError && (
        <div role="alert" className="text-sm text-destructive">
          <p>{post.error.message}</p>
          {refused ? (
            <Button
              type="button"
              variant="outline"
              className="mt-2"
              disabled={paymentHeld}
              onClick={onStale}
            >
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

// endorsements groups a sale's prescription lines by the prescription they
// cite, so each paper prescription gets only its own register serials.
function endorsements(sale: PharmacySale): { lines: number[]; serials: string[] }[] {
  const groups = new Map<string, number[]>();
  for (const l of sale.lines)
    if (l.prescription_id)
      groups.set(l.prescription_id, [...(groups.get(l.prescription_id) ?? []), l.line_number]);
  return [...groups.values()].map((lines) => ({
    lines,
    serials: lines.map((n) => `${sale.invoice_number} · line ${n}`),
  }));
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
        <SaleCharges charges={sale.charges} currency={sale.currency} />
        <Totals sale={sale} />
        {sale.tenders.map((t) => (
          <p key={t.tender_number} className="mt-2 text-sm">
            {t.kind === "admission_account" ? "To the admission account" : "Paid by"}{" "}
            {t.kind === "upi_verified"
              ? "UPI (verified)"
              : t.kind === "admission_account"
                ? ""
                : t.kind.toUpperCase()}{" "}
            {money(t.amount_minor)}
            {t.kind === "cash" && t.tendered_minor !== null && t.change_minor !== null
              ? ` · received ${money(t.tendered_minor)} · change ${money(t.change_minor)}`
              : t.reference
                ? ` · reference ${t.reference}`
                : ""}
          </p>
        ))}
        {sale.discount_minor > 0 && (
          <p className="mt-2 text-sm">
            Discount {money(sale.discount_minor)}
            {sale.discount_reason ? ` · ${sale.discount_reason}` : ""}
          </p>
        )}
        {sale.admission_id ? (
          <p className="mt-2 text-sm">
            Billed to the admission account · bed {sale.admission_bed_code} · hospital{" "}
            {sale.patient_hospital_id}
          </p>
        ) : (
          sale.patient_id &&
          sale.patient_hospital_id && (
            <p className="mt-2 text-sm">Registered patient · hospital {sale.patient_hospital_id}</p>
          )
        )}
        {(sale.buyer_name || sale.buyer_phone) && (
          <p className="mt-2 text-sm">
            Buyer {[sale.buyer_name, sale.buyer_phone].filter(Boolean).join(" · ")}
          </p>
        )}
        <p className="mt-3 text-xs text-muted-foreground">
          Schedule classes are as declared by the outlet.{" "}
          {sale.nppa_ceiling_checked
            ? "NPPA ceiling prices were checked against the outlet's declarations."
            : "NPPA ceiling prices were not checked for every line."}
        </p>
      </article>
      {sale.lines.some((l) => l.prescription_id) && (
        <div role="note" className="mt-4 rounded-md border p-3 text-sm print:hidden">
          Endorse the prescription: note {sale.seller_legal_name}, {sale.seller_address} and{" "}
          {sale.sale_date} on it before returning it. Each prescription also takes the register
          serial of the lines it was dispensed on (Rule 65(3)(1)):
          <ul className="mt-1 list-disc pl-5">
            {endorsements(sale).map(({ lines, serials }) => (
              <li key={lines.join(",")}>
                The prescription for {lines.length === 1 ? "line" : "lines"}{" "}
                {lines.join(lines.length === 2 ? " and " : ", ")}: {serials.join(", ")}
              </li>
            ))}
          </ul>
        </div>
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

const ceilingSchema = z.object({
  percent: z
    .string()
    .trim()
    .regex(
      /^(100(\.0{1,2})?|[0-9]{1,2}(\.[0-9]{1,2})?)$/,
      "Enter a percentage from 0 to 100, at most two decimals",
    ),
  reason: z
    .string()
    .trim()
    .min(5, "Give the reason, at least 5 characters")
    .max(200, "At most 200 characters"),
});

/** The owner records the outlet's discount ceiling (#1386 B-3, decision 3). */
function DiscountCeilingForm({ scope }: { scope: PharmacyScope }) {
  const [id, setId] = useState(() => newPharmacyId());
  const current = useApiQuery<{ ceiling: PharmacyDiscountCeiling | null }>(
    pharmacyKey(scope, "counter", "discount-ceiling"),
    `${pharmacyRoot(scope)}/discount-ceiling`,
  );
  const form = useZodForm(ceilingSchema, { percent: "", reason: "" });
  const save = useApiMutation(
    (values: z.infer<typeof ceilingSchema>) =>
      apiFetch<PharmacyDiscountCeiling>(`${pharmacyRoot(scope)}/discount-ceiling/${id}`, {
        method: "PUT",
        body: JSON.stringify({
          max_bps: Math.round(Number(values.percent) * 100),
          reason: values.reason,
        }),
      }),
    {
      successToast: "Discount ceiling recorded",
      invalidate: [pharmacyKey(scope, "counter", "discount-ceiling")],
      onSuccess: () => {
        setId(newPharmacyId());
        form.reset({ percent: "", reason: "" });
      },
    },
  );
  const inForce = current.data?.ceiling;
  return (
    <Panel>
      <form noValidate className="space-y-4" onSubmit={form.handleSubmit((v) => save.mutate(v))}>
        <h3 className="font-semibold">Discount ceiling</h3>
        <p className="text-sm text-muted-foreground">
          {inForce
            ? `In force: up to ${inForce.max_bps / 100}% off a line (${inForce.reason}).`
            : "None recorded, so no discount can be given at this outlet."}{" "}
          Only people a tenant administrator allows to discount can give one, always with a reason.
        </p>
        <div className="grid gap-4 sm:grid-cols-3">
          <Field
            id="ceiling-percent"
            label="Ceiling %"
            error={form.formState.errors.percent?.message}
          >
            <Input id="ceiling-percent" inputMode="decimal" {...form.register("percent")} />
          </Field>
          <Field id="ceiling-reason" label="Reason" error={form.formState.errors.reason?.message}>
            <Input id="ceiling-reason" maxLength={200} {...form.register("reason")} />
          </Field>
        </div>
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? "Recording…" : "Record discount ceiling"}
        </Button>
      </form>
    </Panel>
  );
}
