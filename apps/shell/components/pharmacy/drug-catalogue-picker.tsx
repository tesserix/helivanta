"use client";

import { useEffect, useRef, useState } from "react";
import { Button, Input } from "@tesserix/web";
import {
  apiFetch,
  pharmacyRoot,
  pharmacyKey,
  newPharmacyId,
  useApiMutation,
  CatalogueProductContract,
  type CatalogueProduct,
  type PharmacyProduct,
  type PharmacyScope,
} from "@helivanta/api";
import { Field, Panel } from "@helivanta/ui";
import { useReportState, type FormState } from "@/lib/use-list-detail";
import { CurrencyAmountField } from "./currency-amount-field";

export function DrugCataloguePicker({
  scope,
  onSaved,
  onStateChange,
}: {
  scope: PharmacyScope;
  onSaved: (product: PharmacyProduct) => void;
  onStateChange?: FormState;
}) {
  const [query, setQuery] = useState("");
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => {
    input.current?.focus();
  }, []);
  const [selection, setSelection] = useState<CatalogueProduct | null>(null);
  const [price, setPrice] = useState(0);
  const [id] = useState(() => newPharmacyId());
  const search = useApiMutation(
    async (q: string) => {
      const response = await apiFetch<{ data: unknown[] }>(
        `${pharmacyRoot(scope)}/catalogue/drugmaster?q=${encodeURIComponent(q)}`,
      );
      return response.data.map((row) => CatalogueProductContract.parse(row));
    },
    { suppressErrorToast: true },
  );
  const add = useApiMutation(
    async () => {
      if (!selection) throw new Error("Choose a medicine first.");
      return apiFetch<PharmacyProduct>(
        `${pharmacyRoot(scope)}/catalogue/products/${id}/from-drugmaster`,
        {
          method: "PUT",
          body: JSON.stringify({
            version_id: selection.version_id,
            currency: "INR",
            mrp_minor: price,
          }),
        },
      );
    },
    { suppressErrorToast: true, invalidate: [pharmacyKey(scope, "catalogue")], onSuccess: onSaved },
  );
  useReportState(onStateChange, !!selection, search.isPending || add.isPending);
  return (
    <Panel className="mb-6 space-y-4">
      <h3 className="font-semibold">Add from the medicine master</h3>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          setSelection(null);
          search.reset();
          add.reset();
          search.mutate(query.trim());
        }}
        className="flex items-end gap-3"
      >
        <Field id="master-query" label="Medicine name, composition or barcode">
          <Input
            id="master-query"
            ref={input}
            value={query}
            maxLength={200}
            onChange={(event) => setQuery(event.target.value)}
          />
        </Field>
        <Button
          type="submit"
          disabled={query.trim().length < 2 || search.isPending || add.isPending}
        >
          Search medicines
        </Button>
      </form>
      {search.isError && (
        <p role="alert">Could not search the published medicine master. Try again.</p>
      )}
      {search.isSuccess && search.data.length === 0 && (
        <p role="status">No published medicines match. You can add a product by hand below.</p>
      )}
      {search.data && (
        <ul className="space-y-3">
          {search.data.map((row) => (
            <li key={row.version_id}>
              <Button
                type="button"
                variant="outline"
                disabled={add.isPending}
                onClick={() => {
                  setSelection(row);
                  add.reset();
                }}
                aria-pressed={selection?.version_id === row.version_id}
              >
                {row.brand_name} · {row.formulation_label} · {row.pack_size} {row.unit_of_issue} ·{" "}
                {row.manufacturer}
              </Button>
            </li>
          ))}
        </ul>
      )}
      {selection && (
        <div className="space-y-4">
          <p>
            {selection.source_name} · {selection.source_version} · effective {selection.valid_from}
          </p>
          <p>
            Source schedule: {selection.regulatory_class}; NDPS: {selection.ndps ? "Yes" : "No"};
            HSN: {selection.hsn_code}
          </p>
          <p className="text-sm text-muted-foreground">
            Confirm the composition and pack. Outlet schedule, tax and price-control reviews are
            required before stock can be released or sold.
          </p>
          <CurrencyAmountField
            id="master-mrp"
            label="Pack MRP"
            currency="INR"
            initialMinor={0}
            onChange={setPrice}
          />
          {add.isError && <p role="alert">{add.error.message}</p>}
          <Button
            type="button"
            disabled={
              add.isPending ||
              search.isPending ||
              !Number.isSafeInteger(price) ||
              price < 0 ||
              price > 1_000_000_000_000
            }
            onClick={() => add.mutate()}
          >
            {add.isPending ? "Adding…" : "Add selected medicine"}
          </Button>
        </div>
      )}
    </Panel>
  );
}
