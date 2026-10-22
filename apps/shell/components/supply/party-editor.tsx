"use client";

import { useState } from "react";
import { Button, Input } from "@tesserix/web";
import { Plus, Trash2 } from "lucide-react";
import {
  apiFetch,
  countryConfiguration,
  countryProfile,
  newSupplyId,
  newSupplyPartyInput,
  supplyContactSchema,
  supplyAddressSchema,
  supplyPartyFieldsSchema,
  supplyPartyFormSchema,
  supplyKey,
  useApiMutation,
  type SupplyScope,
  type SupplyParty,
} from "@helivanta/api";
import { Field, FormSelectField, useFieldArray, useZodForm, type FieldPath } from "@helivanta/ui";

const label = (value: string) =>
  value.replaceAll("_", " ").replace(/\b\w/g, (letter) => letter.toUpperCase());
type PartyForm = ReturnType<typeof useZodForm<typeof supplyPartyFormSchema>>;

export function PartyEditor({
  scope,
  party,
  canManageOrganisation,
  onSaved,
  onCancel,
}: {
  scope: SupplyScope;
  party: SupplyParty | null;
  canManageOrganisation: boolean;
  onSaved: (party: SupplyParty) => void;
  onCancel: () => void;
}) {
  const [id] = useState(() => party?.id ?? newSupplyId());
  const form = useZodForm(
    supplyPartyFormSchema,
    party
      ? {
          ...supplyPartyFieldsSchema.strip().parse(party),
          expected_version: party.version,
        }
      : newSupplyPartyInput(),
  );
  const contacts = useFieldArray({ control: form.control, name: "contacts", keyName: "formKey" });
  const addresses = useFieldArray({ control: form.control, name: "addresses", keyName: "formKey" });
  const taxes = useFieldArray({
    control: form.control,
    name: "tax_registrations",
    keyName: "formKey",
  });
  const country = form.watch("country_code");
  const save = useApiMutation(
    (values: ReturnType<typeof supplyPartyFormSchema.parse>) =>
      apiFetch<SupplyParty>(`/supply/parties/${id}`, {
        method: "PUT",
        body: JSON.stringify(values),
      }),
    {
      invalidate: [supplyKey(scope, "parties"), supplyKey(scope, "party", id)],
      successToast: "Draft profile saved",
      suppressErrorToast: true,
      onSuccess: onSaved,
    },
  );
  return (
    <form
      noValidate
      onSubmit={form.handleSubmit((values) => save.mutate(values))}
      className="rounded-2xl border bg-card p-5 sm:p-7"
    >
      <div className="mb-6 flex flex-wrap items-start justify-between gap-3">
        <div>
          <h3 className="text-xl font-semibold">
            {party ? "Edit draft profile" : "Add an organisation"}
          </h3>
          <p className="mt-2 max-w-2xl text-sm leading-6 text-muted-foreground">
            Save legal and contact details privately. A draft profile does not verify licences or
            authorise trading.
          </p>
        </div>
        <Button type="button" variant="ghost" onClick={onCancel} disabled={save.isPending}>
          Close form
        </Button>
      </div>
      <fieldset disabled={save.isPending} className="space-y-7">
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          <TextField form={form} name="code" label="Organisation code" />
          <TextField form={form} name="legal_name" label="Legal name" />
          <TextField form={form} name="trading_name" label="Trading name" />
          <FormSelectField
            control={form.control}
            name="record_kind"
            id="party-record-kind"
            label="Relationship to your tenant"
            disabled={!!party}
            options={[
              { value: "counterparty", label: "Supplier or counterparty" },
              ...(canManageOrganisation
                ? [{ value: "owned_organisation", label: "My organisation" }]
                : []),
            ]}
          />
          <FormSelectField
            control={form.control}
            name="business_kind"
            id="party-business-kind"
            label="Business type"
            options={supplyPartyFieldsSchema.shape.business_kind.options.map((value) => ({
              value,
              label: label(value),
            }))}
          />
          <FormSelectField
            control={form.control}
            name="country_code"
            id="party-country"
            label="Country"
            disabled={!!party}
            options={countryConfiguration.countries.map((p) => ({ value: p.code, label: p.name }))}
          />
          <TextField form={form} name="jurisdiction" label="Registration jurisdiction" />
          <TextField form={form} name="registration_number" label="Business registration number" />
          <TextField form={form} name="corporate_group" label="Corporate group" />
          <TextField form={form} name="website" label="Website" type="url" />
          <TextField
            form={form}
            name="default_credit_days"
            label="Preferred credit days"
            type="number"
          />
        </div>
        <section className="space-y-4 border-t pt-6" aria-label="Organisation contacts">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <h4 className="font-semibold">Contacts</h4>
              <p className="mt-1 text-sm text-muted-foreground">
                Purchasing, finance, responsible pharmacist and emergency contacts.
              </p>
            </div>
            <Button
              type="button"
              variant="outline"
              disabled={contacts.fields.length >= 20}
              onClick={() =>
                contacts.append({
                  id: newSupplyId(),
                  name: "",
                  role: "general",
                  email: "",
                  phone: "",
                  alternate_phone: "",
                })
              }
            >
              <Plus className="mr-2 size-4" />
              Add contact
            </Button>
          </div>
          {contacts.fields.length === 0 && (
            <p className="text-sm text-muted-foreground">No contacts added.</p>
          )}
          {contacts.fields.map((row, index) => (
            <fieldset key={row.formKey} className="rounded-xl border bg-background p-4">
              <legend className="px-2 text-sm font-medium">Contact {index + 1}</legend>
              <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
                <TextField form={form} name={`contacts.${index}.name`} label="Contact name" />
                <FormSelectField
                  control={form.control}
                  name={`contacts.${index}.role`}
                  id={`contact-role-${row.id}`}
                  label="Contact role"
                  options={supplyContactSchema.shape.role.options.map((value) => ({
                    value,
                    label: label(value),
                  }))}
                />
                <TextField
                  form={form}
                  name={`contacts.${index}.email`}
                  label="Email"
                  type="email"
                />
                <TextField
                  form={form}
                  name={`contacts.${index}.phone`}
                  label="Phone (international format)"
                  type="tel"
                />
                <TextField
                  form={form}
                  name={`contacts.${index}.alternate_phone`}
                  label="Alternate phone"
                  type="tel"
                />
              </div>
              <Button
                type="button"
                variant="ghost"
                className="mt-3"
                onClick={() => contacts.remove(index)}
                aria-label={`Remove contact ${index + 1}`}
              >
                <Trash2 className="mr-2 size-4" />
                Remove contact
              </Button>
            </fieldset>
          ))}
        </section>
        <section className="space-y-4 border-t pt-6" aria-label="Organisation addresses">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <h4 className="font-semibold">Addresses</h4>
            <Button
              type="button"
              variant="outline"
              disabled={addresses.fields.length >= 20}
              onClick={() =>
                addresses.append({
                  id: newSupplyId(),
                  kind: "registered",
                  line1: "",
                  line2: "",
                  city: "",
                  region: "",
                  postal_code: "",
                  country_code: country,
                })
              }
            >
              <Plus className="mr-2 size-4" />
              Add address
            </Button>
          </div>
          {addresses.fields.length === 0 && (
            <p className="text-sm text-muted-foreground">No addresses added.</p>
          )}
          {addresses.fields.map((row, index) => (
            <fieldset key={row.formKey} className="rounded-xl border bg-background p-4">
              <legend className="px-2 text-sm font-medium">Address {index + 1}</legend>
              <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
                <FormSelectField
                  control={form.control}
                  name={`addresses.${index}.kind`}
                  id={`address-kind-${row.id}`}
                  label="Address type"
                  options={supplyAddressSchema.shape.kind.options.map((value) => ({
                    value,
                    label: label(value),
                  }))}
                />
                <TextField form={form} name={`addresses.${index}.line1`} label="Address line 1" />
                <TextField form={form} name={`addresses.${index}.line2`} label="Address line 2" />
                <TextField form={form} name={`addresses.${index}.city`} label="City" />
                <TextField
                  form={form}
                  name={`addresses.${index}.region`}
                  label="State or province"
                />
                <TextField
                  form={form}
                  name={`addresses.${index}.postal_code`}
                  label="Postal code"
                />
                <FormSelectField
                  control={form.control}
                  name={`addresses.${index}.country_code`}
                  id={`address-country-${row.id}`}
                  label="Address country"
                  options={countryConfiguration.countries.map((p) => ({
                    value: p.code,
                    label: p.name,
                  }))}
                />
              </div>
              <Button
                type="button"
                variant="ghost"
                className="mt-3"
                onClick={() => addresses.remove(index)}
                aria-label={`Remove address ${index + 1}`}
              >
                <Trash2 className="mr-2 size-4" />
                Remove address
              </Button>
            </fieldset>
          ))}
        </section>
        <section className="space-y-4 border-t pt-6" aria-label="Tax declarations">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <h4 className="font-semibold">Tax registrations</h4>
              <p className="mt-1 text-sm text-muted-foreground">
                Declared identifiers awaiting verification.
              </p>
            </div>
            <Button
              type="button"
              variant="outline"
              disabled={taxes.fields.length >= 20}
              onClick={() =>
                taxes.append({ id: newSupplyId(), kind: "other", number: "", jurisdiction: "" })
              }
            >
              <Plus className="mr-2 size-4" />
              Add tax registration
            </Button>
          </div>
          {taxes.fields.length === 0 && (
            <p className="text-sm text-muted-foreground">No tax registrations added.</p>
          )}
          {taxes.fields.map((row, index) => (
            <fieldset key={row.formKey} className="rounded-xl border bg-background p-4">
              <legend className="px-2 text-sm font-medium">Tax registration {index + 1}</legend>
              <div className="grid gap-4 sm:grid-cols-3">
                <FormSelectField
                  control={form.control}
                  name={`tax_registrations.${index}.kind`}
                  id={`tax-kind-${row.id}`}
                  label="Identifier type"
                  options={(countryProfile(country)?.tax_identifier_kinds ?? []).map((value) => ({
                    value,
                    label: value.toUpperCase(),
                  }))}
                />
                <TextField
                  form={form}
                  name={`tax_registrations.${index}.number`}
                  label="Tax identifier"
                />
                <TextField
                  form={form}
                  name={`tax_registrations.${index}.jurisdiction`}
                  label="Tax jurisdiction"
                />
              </div>
              <Button
                type="button"
                variant="ghost"
                className="mt-3"
                onClick={() => taxes.remove(index)}
                aria-label={`Remove tax registration ${index + 1}`}
              >
                <Trash2 className="mr-2 size-4" />
                Remove tax registration
              </Button>
            </fieldset>
          ))}
        </section>
        {save.isError && (
          <p
            role="alert"
            className="rounded-xl border border-destructive/30 p-4 text-sm text-destructive"
          >
            {save.error.message}
          </p>
        )}
        <div className="flex flex-wrap items-center gap-3 border-t pt-5">
          <Button type="submit">{save.isPending ? "Saving…" : "Save draft profile"}</Button>
          <p className="text-xs text-muted-foreground">
            Private to this tenant and authorised organisation users.
          </p>
        </div>
      </fieldset>
    </form>
  );
}
function TextField({
  form,
  name,
  label,
  type = "text",
}: {
  form: PartyForm;
  name: FieldPath<ReturnType<typeof supplyPartyFormSchema.parse>>;
  label: string;
  type?: "text" | "number" | "email" | "tel" | "url";
}) {
  const id = `party-${name.replaceAll(".", "-")}`;
  const error = form.getFieldState(name, form.formState).error?.message;
  return (
    <Field id={id} label={label} error={error}>
      <Input
        id={id}
        type={type}
        aria-invalid={!!error}
        aria-describedby={error ? `${id}-error` : undefined}
        {...form.register(name, type === "number" ? { valueAsNumber: true } : {})}
      />
    </Field>
  );
}
