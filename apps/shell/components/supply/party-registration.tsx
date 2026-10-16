"use client";

import { useRef, useState } from "react";
import { Button, Input } from "@tesserix/web";
import { Building2, FileCheck2, Plus } from "lucide-react";
import {
  apiFetch,
  countryProfile,
  newSupplyId,
  supplyRegistryKey,
  supplyRegistryRoot,
  supplyPremiseFieldsSchema,
  supplyPremiseFormSchema,
  supplyLicenseFieldsSchema,
  supplyLicenseFormSchema,
  useApiPagedQuery,
  useApiQuery,
  useApiMutation,
  usePermissions,
  type SupplyPremise,
  type SupplyLicense,
  type SupplyRegistryScope,
  type SupplyRegistryRevision,
  type SupplyPremiseInput,
  type SupplyLicenseInput,
} from "@helivanta/api";
import {
  Field,
  FormSelectField,
  LoadMore,
  useZodForm,
  formatDateTime,
  type FieldValues,
  type FieldPath,
  type UseFormReturn,
} from "@helivanta/ui";
import { LicenceReview } from "./licence-review";
import { LicenceEvidence } from "./licence-evidence";
import { RequestFailure } from "../request-failure";

const readers = ["administrator", "purchasing", "finance", "compliance", "auditor"];
const label = (value: string) =>
  value.replaceAll("_", " ").replace(/\b\w/g, (letter) => letter.toUpperCase());
export function PartyRegistration({
  scope,
  country,
  roles,
}: {
  scope: SupplyRegistryScope;
  country: string;
  roles: string[];
}) {
  const { can } = usePermissions();
  const allowed = roles.some((role) => readers.includes(role));
  const premises = useApiPagedQuery<SupplyPremise>(
    supplyRegistryKey(scope, "premises"),
    supplyRegistryRoot(scope, "premises"),
    { enabled: allowed && can("supply.premise.read") },
  );
  const licenses = useApiPagedQuery<SupplyLicense>(
    supplyRegistryKey(scope, "licenses"),
    supplyRegistryRoot(scope, "licenses"),
    { enabled: allowed && can("supply.license.read") },
  );
  const [review, setReview] = useState<SupplyLicense | null>(null);
  const currentReview = review && licenses.items.find((row) => row.id === review.id);
  const [evidence, setEvidence] = useState<SupplyLicense | null>(null);
  const currentEvidence = evidence && licenses.items.find((row) => row.id === evidence.id);
  const [premise, setPremise] = useState<SupplyPremise | null>();
  const [license, setLicense] = useState<SupplyLicense | null>();
  const [history, setHistory] = useState<{ kind: "premises" | "licenses"; id: string } | null>(
    null,
  );
  const target = useRef<HTMLDivElement>(null);
  const editPremise =
    can("supply.premise.write") &&
    roles.some((role) => role === "administrator" || role === "purchasing");
  const editLicense =
    can("supply.license.write") &&
    roles.some((role) => ["administrator", "purchasing", "compliance"].includes(role));
  function focusForm() {
    requestAnimationFrame(() => {
      target.current?.scrollIntoView({ block: "start", behavior: "instant" });
      target.current?.querySelector<HTMLInputElement>("input")?.focus({ preventScroll: true });
    });
  }
  if (!allowed || (!can("supply.premise.read") && !can("supply.license.read"))) return null;
  return (
    <section className="space-y-6 border-t pt-6" aria-label="Premises and licences">
      <div>
        <h4 className="text-lg font-semibold">Premises and licences</h4>
        <p className="mt-2 text-sm leading-6 text-muted-foreground">
          Keep operating addresses and declared licences together. These records require independent
          evidence review before verification or trading approval.
        </p>
      </div>
      {can("supply.premise.read") && (
        <section aria-label="Organisation premises" className="rounded-xl border p-4 sm:p-5">
          <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
            <h5 className="flex items-center gap-2 font-semibold">
              <Building2 className="size-4" />
              Premises
            </h5>
            {editPremise && (
              <Button
                variant="outline"
                onClick={() => {
                  setLicense(undefined);
                  setPremise(null);
                  focusForm();
                }}
              >
                <Plus className="mr-2 size-4" />
                Add premise
              </Button>
            )}
          </div>
          {premises.isError ? (
            <RequestFailure message="Could not load private premises." retry={premises.retry} />
          ) : premises.isPending ? (
            <p role="status">Loading premises…</p>
          ) : premises.items.length === 0 ? (
            <p className="text-sm text-muted-foreground">No premises recorded.</p>
          ) : (
            <ul className="divide-y">
              {premises.items.map((row) => (
                <li key={row.id} className="flex flex-wrap items-start justify-between gap-3 py-4">
                  <div className="min-w-0">
                    <p className="break-words font-medium">{row.name}</p>
                    <p className="mt-1 text-sm text-muted-foreground">
                      {row.code} · {label(row.kind)} · {label(row.status)}
                    </p>
                    <p className="mt-1 break-words text-sm text-muted-foreground">
                      {[row.line1, row.line2, row.city, row.region, row.postal_code]
                        .filter(Boolean)
                        .join(", ")}
                    </p>
                  </div>
                  <div className="flex flex-wrap gap-2">
                    {editPremise && (
                      <Button
                        variant="outline"
                        onClick={() => {
                          setLicense(undefined);
                          setPremise(row);
                          focusForm();
                        }}
                        aria-label={`Edit premise ${row.code}`}
                      >
                        Edit
                      </Button>
                    )}
                    <Button
                      variant="ghost"
                      onClick={() => setHistory({ kind: "premises", id: row.id })}
                      aria-label={`History for premise ${row.code}`}
                    >
                      History
                    </Button>
                  </div>
                </li>
              ))}
            </ul>
          )}
          <LoadMore
            hasMore={premises.hasMore}
            isLoading={premises.isFetchingMore}
            onClick={premises.loadMore}
          />
        </section>
      )}
      {can("supply.license.read") && (
        <section aria-label="Declared licences" className="rounded-xl border p-4 sm:p-5">
          <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
            <h5 className="flex items-center gap-2 font-semibold">
              <FileCheck2 className="size-4" />
              Declared licences
            </h5>
            {editLicense && premises.items.length > 0 && (
              <Button
                variant="outline"
                onClick={() => {
                  setPremise(undefined);
                  setLicense(null);
                  focusForm();
                }}
              >
                <Plus className="mr-2 size-4" />
                Declare licence
              </Button>
            )}
          </div>
          {licenses.isError ? (
            <RequestFailure
              message="Could not load private licence declarations."
              retry={licenses.retry}
            />
          ) : licenses.isPending ? (
            <p role="status">Loading licences…</p>
          ) : licenses.items.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              No licences declared. Record a premise before adding its licence.
            </p>
          ) : (
            <ul className="divide-y">
              {licenses.items.map((row) => (
                <li key={row.id} className="flex flex-wrap items-start justify-between gap-3 py-4">
                  <div className="min-w-0">
                    <p className="break-words font-medium">
                      {row.kind} · {row.number}
                    </p>
                    <p className="mt-1 break-words text-sm text-muted-foreground">
                      {row.issuing_authority} · {row.jurisdiction}
                    </p>
                    <p className="mt-1 text-sm text-muted-foreground">
                      Issued {row.issued_on} · Expires {row.expires_on}
                    </p>
                    <p className="mt-2 text-xs font-medium text-muted-foreground">
                      Declaration recorded
                    </p>
                  </div>
                  <div className="flex flex-wrap gap-2">
                    <Button
                      variant="outline"
                      onClick={() => setReview(row)}
                      aria-label={`Review for licence ${row.number}`}
                    >
                      Review
                    </Button>
                    {can("supply.evidence.read") && (
                      <Button
                        variant="outline"
                        onClick={() => setEvidence(row)}
                        aria-label={`Evidence for licence ${row.number}`}
                      >
                        Evidence
                      </Button>
                    )}
                    {editLicense && (
                      <Button
                        variant="outline"
                        onClick={() => {
                          setPremise(undefined);
                          setLicense(row);
                          focusForm();
                        }}
                        aria-label={`Edit licence ${row.number}`}
                      >
                        Edit
                      </Button>
                    )}
                    <Button
                      variant="ghost"
                      onClick={() => setHistory({ kind: "licenses", id: row.id })}
                      aria-label={`History for licence ${row.number}`}
                    >
                      History
                    </Button>
                  </div>
                </li>
              ))}
            </ul>
          )}
          <LoadMore
            hasMore={licenses.hasMore}
            isLoading={licenses.isFetchingMore}
            onClick={licenses.loadMore}
          />
        </section>
      )}
      <div ref={target} className="scroll-mt-32">
        {premise !== undefined && editPremise && (
          <PremiseEditor
            key={premise?.id ?? "new-premise"}
            scope={scope}
            country={country}
            row={premise}
            onClose={() => setPremise(undefined)}
          />
        )}
        {license !== undefined && editLicense && (
          <LicenseEditor
            key={license?.id ?? "new-license"}
            scope={scope}
            row={license}
            premises={premises.items}
            hasMorePremises={premises.hasMore}
            loadingPremises={premises.isFetchingMore}
            loadMorePremises={premises.loadMore}
            onClose={() => setLicense(undefined)}
          />
        )}
      </div>
      {currentReview && (
        <LicenceReview
          key={`${scope.tenantId}:${scope.subject}:${scope.partyId}:${currentReview.id}:${currentReview.version}`}
          scope={{ ...scope, licenseId: currentReview.id }}
          licence={currentReview}
          roles={roles}
          onClose={() => setReview(null)}
        />
      )}
      {currentEvidence && (
        <LicenceEvidence
          key={`${scope.tenantId}:${scope.subject}:${scope.partyId}:${currentEvidence.id}:${currentEvidence.version}`}
          scope={{ ...scope, licenseId: currentEvidence.id }}
          licence={currentEvidence}
          roles={roles}
          onClose={() => setEvidence(null)}
        />
      )}
      {history && (
        <RegistryHistory
          key={`${history.kind}:${history.id}`}
          scope={scope}
          record={history}
          onClose={() => setHistory(null)}
        />
      )}
    </section>
  );
}
function PremiseEditor({
  scope,
  country,
  row,
  onClose,
}: {
  scope: SupplyRegistryScope;
  country: string;
  row: SupplyPremise | null;
  onClose: () => void;
}) {
  const [id] = useState(() => row?.id ?? newSupplyId());
  const profile = countryProfile(country);
  const form = useZodForm(
    supplyPremiseFormSchema,
    row
      ? { ...supplyPremiseFieldsSchema.strip().parse(row), expected_version: row.version }
      : {
          expected_version: 0,
          code: "",
          name: "",
          kind: "warehouse",
          status: "active",
          line1: "",
          line2: "",
          city: "",
          region: "",
          postal_code: "",
          country_code: profile?.code ?? "IN",
          time_zone: profile?.timezone ?? "",
        },
  );
  const save = useApiMutation(
    (value: SupplyPremiseInput) =>
      apiFetch<SupplyPremise>(`${supplyRegistryRoot(scope, "premises")}/${id}`, {
        method: "PUT",
        body: JSON.stringify(value),
      }),
    {
      invalidate: [
        supplyRegistryKey(scope, "premises"),
        supplyRegistryKey(scope, "history", "premises", id),
      ],
      successToast: "Premise saved",
      suppressErrorToast: true,
      onSuccess: onClose,
    },
  );
  return (
    <form
      noValidate
      onSubmit={form.handleSubmit((value) => save.mutate(value))}
      className="space-y-5 rounded-xl border bg-background p-5"
      aria-label="Premise registration"
    >
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h5 className="font-semibold">{row ? "Edit premise" : "New premise"}</h5>
        <Button type="button" variant="ghost" disabled={save.isPending} onClick={onClose}>
          Close premise form
        </Button>
      </div>
      <p className="text-sm text-muted-foreground">
        {profile?.name} · Operating location for this organisation. A hospital affiliation is
        optional.
      </p>
      <fieldset disabled={save.isPending} className="grid gap-4 sm:grid-cols-2">
        <RegistryInput form={form} name="code" label="Premise code" />
        <RegistryInput form={form} name="name" label="Premise name" />
        <FormSelectField
          control={form.control}
          name="kind"
          id="premise-kind"
          label="Premise type"
          options={supplyPremiseFieldsSchema.shape.kind.options.map((value) => ({
            value,
            label: label(value),
          }))}
        />
        <FormSelectField
          control={form.control}
          name="status"
          id="premise-status"
          label="Premise status"
          options={supplyPremiseFieldsSchema.shape.status.options.map((value) => ({
            value,
            label: label(value),
          }))}
        />
        <RegistryInput form={form} name="line1" label="Address line 1" />
        <RegistryInput form={form} name="line2" label="Address line 2" />
        <RegistryInput form={form} name="city" label="City" />
        <RegistryInput form={form} name="region" label="State or province" />
        <RegistryInput form={form} name="postal_code" label="Postal code" />
        <FormSelectField
          control={form.control}
          name="time_zone"
          id="premise-time-zone"
          label="Operating time zone"
          options={(profile?.timezones ?? []).map((value) => ({ value, label: value }))}
        />
      </fieldset>
      {save.isError && (
        <p role="alert" className="text-sm text-destructive">
          {save.error.message}
        </p>
      )}
      <Button type="submit" disabled={save.isPending}>
        {save.isPending ? "Saving…" : "Save premise"}
      </Button>
    </form>
  );
}
function LicenseEditor({
  scope,
  row,
  premises,
  hasMorePremises,
  loadingPremises,
  loadMorePremises,
  onClose,
}: {
  scope: SupplyRegistryScope;
  row: SupplyLicense | null;
  premises: SupplyPremise[];
  hasMorePremises: boolean;
  loadingPremises: boolean;
  loadMorePremises: () => void;
  onClose: () => void;
}) {
  const missingPremise = !!row && !premises.some((p) => p.id === row.premise_id);
  const selectedPremise = useApiQuery<SupplyPremise>(
    supplyRegistryKey(scope, "premise", row?.premise_id),
    `${supplyRegistryRoot(scope, "premises")}/${row?.premise_id ?? ""}`,
    { enabled: missingPremise, gcTime: 0 },
  );
  const options =
    missingPremise && selectedPremise.data && !selectedPremise.isError
      ? [...premises, selectedPremise.data]
      : premises;
  const [id] = useState(() => row?.id ?? newSupplyId());
  const form = useZodForm(
    supplyLicenseFormSchema,
    row
      ? { ...supplyLicenseFieldsSchema.strip().parse(row), expected_version: row.version }
      : {
          expected_version: 0,
          kind: "",
          number: "",
          issuing_authority: "",
          jurisdiction: "",
          premise_id: "",
          issued_on: "",
          expires_on: "",
          product_categories: [],
        },
  );
  const save = useApiMutation(
    (value: SupplyLicenseInput) =>
      apiFetch<SupplyLicense>(`${supplyRegistryRoot(scope, "licenses")}/${id}`, {
        method: "PUT",
        body: JSON.stringify(value),
      }),
    {
      invalidate: [
        supplyRegistryKey(scope, "licenses"),
        supplyRegistryKey(scope, "history", "licenses", id),
      ],
      successToast: "Licence declaration saved",
      suppressErrorToast: true,
      onSuccess: onClose,
    },
  );
  return (
    <form
      noValidate
      onSubmit={form.handleSubmit((value) => save.mutate(value))}
      className="space-y-5 rounded-xl border bg-background p-5"
      aria-label="Licence declaration"
    >
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h5 className="font-semibold">{row ? "Edit licence declaration" : "Declare a licence"}</h5>
        <Button type="button" variant="ghost" disabled={save.isPending} onClick={onClose}>
          Close licence form
        </Button>
      </div>
      <p className="text-sm leading-6 text-muted-foreground">
        Copy the issuing authority’s details and complete dates. This saves a private declaration
        and does not verify the licence.
      </p>
      <fieldset disabled={save.isPending} className="grid gap-4 sm:grid-cols-2">
        <RegistryInput form={form} name="kind" label="Licence type code" />
        <RegistryInput form={form} name="number" label="Licence number" />
        <RegistryInput form={form} name="issuing_authority" label="Issuing authority" />
        <RegistryInput form={form} name="jurisdiction" label="Licence jurisdiction" />
        <FormSelectField
          control={form.control}
          name="premise_id"
          id="license-premise"
          label="Licensed premise"
          options={options.map((p) => ({
            value: p.id,
            label: `${p.code} · ${p.name}${p.status === "inactive" ? " · Inactive" : ""}`,
          }))}
        />
        <div className="sm:col-span-2">
          {missingPremise && selectedPremise.isPending && (
            <p role="status">Loading selected premise…</p>
          )}
          {missingPremise && selectedPremise.isError && (
            <RequestFailure
              message="Could not load the selected premise. Your access may have changed."
              retry={selectedPremise.refetch}
            />
          )}
          <LoadMore
            hasMore={hasMorePremises}
            isLoading={loadingPremises}
            onClick={loadMorePremises}
          />
        </div>
        <RegistryInput form={form} name="issued_on" label="Issue date" type="date" />
        <RegistryInput form={form} name="expires_on" label="Expiry date" type="date" />
        <fieldset className="space-y-3 sm:col-span-2">
          <legend className="mb-2 text-sm font-medium">Declared product categories</legend>
          <div className="grid gap-3 sm:grid-cols-2">
            {supplyLicenseFieldsSchema.shape.product_categories.element.options.map((value) => (
              <label key={value} className="flex items-center gap-2 text-sm">
                <input type="checkbox" value={value} {...form.register("product_categories")} />
                {label(value)}
              </label>
            ))}
          </div>
          {form.formState.errors.product_categories?.message && (
            <p role="alert" className="text-sm text-destructive">
              {form.formState.errors.product_categories.message}
            </p>
          )}
        </fieldset>
      </fieldset>
      {save.isError && (
        <p role="alert" className="text-sm text-destructive">
          {save.error.message}
        </p>
      )}
      <Button
        type="submit"
        disabled={save.isPending || (missingPremise && !selectedPremise.isSuccess)}
      >
        {save.isPending ? "Saving…" : "Save licence declaration"}
      </Button>
    </form>
  );
}
function RegistryInput<T extends FieldValues>({
  form,
  name,
  label,
  type = "text",
}: {
  form: UseFormReturn<T>;
  name: FieldPath<T>;
  label: string;
  type?: "text" | "date";
}) {
  const id = `registration-${name}`;
  const error = form.getFieldState(name, form.formState).error?.message;
  return (
    <Field id={id} label={label} error={error}>
      <Input
        id={id}
        type={type}
        aria-invalid={!!error}
        aria-describedby={error ? `${id}-error` : undefined}
        {...form.register(name)}
      />
    </Field>
  );
}
function RegistryHistory({
  scope,
  record,
  onClose,
}: {
  scope: SupplyRegistryScope;
  record: { kind: "premises" | "licenses"; id: string };
  onClose: () => void;
}) {
  const history = useApiPagedQuery<SupplyRegistryRevision<SupplyPremise | SupplyLicense>>(
    supplyRegistryKey(scope, "history", record.kind, record.id),
    `${supplyRegistryRoot(scope, record.kind)}/${record.id}/revisions`,
  );
  return (
    <section className="rounded-xl border p-5" aria-label="Registration revision history">
      <div className="mb-4 flex items-center justify-between gap-3">
        <h5 className="font-semibold">Revision history</h5>
        <Button variant="ghost" onClick={onClose}>
          Close history
        </Button>
      </div>
      {history.isError ? (
        <RequestFailure
          message="Private history is unavailable. Your access may have changed."
          retry={history.retry}
        />
      ) : history.isPending ? (
        <p role="status">Loading private revisions…</p>
      ) : (
        <ul className="divide-y">
          {history.items.map((row) => (
            <li key={row.id} className="py-3 text-sm">
              <p className="break-words font-medium">
                Version {row.version} ·{" "}
                {"name" in row.snapshot ? row.snapshot.name : row.snapshot.number}
              </p>
              {"name" in row.snapshot ? (
                <div className="mt-2 space-y-1 break-words text-muted-foreground">
                  <p>
                    {row.snapshot.code} · {label(row.snapshot.kind)} · {label(row.snapshot.status)}
                  </p>
                  <p>
                    {[
                      row.snapshot.line1,
                      row.snapshot.line2,
                      row.snapshot.city,
                      row.snapshot.region,
                      row.snapshot.postal_code,
                    ]
                      .filter(Boolean)
                      .join(", ")}
                  </p>
                  <p>
                    {row.snapshot.country_code} · {row.snapshot.time_zone}
                  </p>
                </div>
              ) : (
                <div className="mt-2 space-y-1 break-words text-muted-foreground">
                  <p>
                    {row.snapshot.kind} · {row.snapshot.issuing_authority} ·{" "}
                    {row.snapshot.jurisdiction}
                  </p>
                  <p>
                    Issued {row.snapshot.issued_on} · Expires {row.snapshot.expires_on}
                  </p>
                  <p>{row.snapshot.product_categories.map(label).join(" · ")}</p>
                </div>
              )}
              <p className="mt-1 break-words text-xs text-muted-foreground">
                {formatDateTime(row.created_at)} · {row.actor_subject}
              </p>
            </li>
          ))}
        </ul>
      )}
      <LoadMore
        hasMore={history.hasMore}
        isLoading={history.isFetchingMore}
        onClick={history.loadMore}
      />
    </section>
  );
}
