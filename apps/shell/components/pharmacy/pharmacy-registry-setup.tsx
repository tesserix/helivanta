"use client";
import { useState } from "react";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import {
  apiFetch,
  countryConfiguration,
  countryProfile,
  useApiMutation,
  useApiPagedQuery,
  newPharmacyId,
  pharmacyOrganizationInputSchema,
  pharmacyOutletInputSchema,
  pharmacyAffiliationInputSchema,
  type PharmacyOrganization,
  type PharmacyHospitalAffiliation,
} from "@helivanta/api";
import {
  Field,
  FormSelectField,
  LoadMore,
  Panel,
  RequestFailure,
  SelectControl,
  useZodForm,
} from "@helivanta/ui";
import { useReportState, type FormState } from "@/lib/use-list-detail";
export function PharmacyRegistrySetup({
  tenantId,
  pharmacy,
}: {
  tenantId: string;
  pharmacy?: PharmacyOrganization;
}) {
  return (
    <Panel aria-labelledby="pharmacy-registry" className="mt-6">
      <h3 id="pharmacy-registry" className="font-semibold">
        Pharmacies and outlets
      </h3>
      <p className="mt-1 text-sm leading-6 text-muted-foreground">
        Register independent, chain or hospital-owned pharmacies without a mandatory hospital. Each
        outlet needs its own access grant and regulatory review.
      </p>
      {/* Outlets register in the Outlets & access pane, beside their list (#1303 E6). */}
      <div className="mt-6 max-w-2xl">
        <OrganizationForm tenantId={tenantId} />
      </div>
      {pharmacy && (
        <AffiliationForm
          key={`affiliation:${pharmacy.id}`}
          tenantId={tenantId}
          pharmacy={pharmacy}
        />
      )}
    </Panel>
  );
}
function OrganizationForm({ tenantId }: { tenantId: string }) {
  const [id, setId] = useState(() => newPharmacyId());
  const form = useZodForm(pharmacyOrganizationInputSchema, {
    code: "",
    name: "",
    ownership_type: "independent",
  });
  const save = useApiMutation(
    (values: z.infer<typeof pharmacyOrganizationInputSchema>) =>
      apiFetch(`/pharmacy/pharmacies/${id}`, { method: "PUT", body: JSON.stringify(values) }),
    {
      successToast: "Pharmacy registered",
      invalidate: [["pharmacy", tenantId, "organizations"]],
      onSuccess: () => {
        setId(newPharmacyId());
        form.reset();
      },
      suppressErrorToast: true,
    },
  );
  return (
    <Panel>
      <form
        noValidate
        className="space-y-4"
        onSubmit={form.handleSubmit((values) => save.mutate(values))}
      >
        <h3 className="font-semibold">Register pharmacy</h3>
        <Field
          id="organization-code"
          label="Pharmacy code"
          error={form.formState.errors.code?.message}
        >
          <Input id="organization-code" {...form.register("code")} />
        </Field>
        <Field
          id="organization-name"
          label="Pharmacy name"
          error={form.formState.errors.name?.message}
        >
          <Input id="organization-name" {...form.register("name")} />
        </Field>
        <FormSelectField
          control={form.control}
          name="ownership_type"
          id="organization-type"
          label="Ownership type"
          options={[
            { value: "independent", label: "Independent pharmacy" },
            { value: "chain", label: "Pharmacy chain" },
            { value: "hospital_owned", label: "Hospital-owned pharmacy" },
          ]}
        />
        {save.isError && (
          <p role="alert" className="text-sm text-destructive">
            {save.error.message}
          </p>
        )}
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? "Registering…" : "Register pharmacy"}
        </Button>
      </form>
    </Panel>
  );
}
export function OutletForm({
  tenantId,
  pharmacy,
  onSaved,
  onStateChange,
}: {
  tenantId: string;
  pharmacy: PharmacyOrganization;
  /** The registered outlet opens in the pane (#1303 E6). */
  onSaved?: (id: string) => void;
  onStateChange?: FormState;
}) {
  const [id, setId] = useState(() => newPharmacyId());
  const form = useZodForm(pharmacyOutletInputSchema, {
    code: "",
    name: "",
    country_code: "IN",
    timezone: "Asia/Kolkata",
  });
  const save = useApiMutation(
    (values: z.infer<typeof pharmacyOutletInputSchema>) =>
      apiFetch(`/pharmacy/pharmacies/${pharmacy.id}/outlets/${id}`, {
        method: "PUT",
        body: JSON.stringify(values),
      }),
    {
      successToast: "Outlet registered",
      invalidate: [["pharmacy", tenantId, pharmacy.id, "outlets"]],
      onSuccess: () => {
        onSaved?.(id);
        setId(newPharmacyId());
        form.reset();
      },
      suppressErrorToast: true,
    },
  );
  useReportState(onStateChange, form.formState.isDirty, save.isPending);
  return (
    <Panel>
      <form
        noValidate
        className="space-y-4"
        onSubmit={form.handleSubmit((values) => save.mutate(values))}
      >
        {(
          [
            ["code", "Outlet code"],
            ["name", "Outlet name"],
          ] as const
        ).map(([name, label]) => (
          <Field
            key={name}
            id={`outlet-${name}`}
            label={label}
            error={form.formState.errors[name]?.message}
          >
            <Input id={`outlet-${name}`} {...form.register(name)} />
          </Field>
        ))}
        <Field
          id="outlet-country_code"
          label="Country"
          error={form.formState.errors.country_code?.message}
        >
          <SelectControl
            id="outlet-country_code"
            value={form.watch("country_code")}
            onValueChange={(value) => {
              const profile = countryProfile(value);
              if (profile) {
                form.setValue("country_code", profile.code, { shouldValidate: true });
                form.setValue("timezone", profile.timezone, { shouldValidate: true });
              }
            }}
            options={countryConfiguration.countries.map((p) => ({ value: p.code, label: p.name }))}
          />
        </Field>
        <Field
          id="outlet-timezone"
          label="IANA time zone"
          error={form.formState.errors.timezone?.message}
        >
          <Input id="outlet-timezone" {...form.register("timezone")} readOnly />
        </Field>
        <p className="text-xs leading-6 text-muted-foreground">
          {countryProfile(form.watch("country_code"))?.currency} · Country metadata requires
          regulatory review before trading.
        </p>
        {save.isError && (
          <p role="alert" className="text-sm text-destructive">
            {save.error.message}
          </p>
        )}
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? "Registering…" : "Register outlet"}
        </Button>
      </form>
    </Panel>
  );
}
function AffiliationForm({
  tenantId,
  pharmacy,
}: {
  tenantId: string;
  pharmacy: PharmacyOrganization;
}) {
  const [id, setId] = useState(() => newPharmacyId());
  const form = useZodForm(pharmacyAffiliationInputSchema, {
    hospital_id: "",
    relationship: "serving",
  });
  const key = ["pharmacy", tenantId, pharmacy.id, "affiliations"];
  const links = useApiPagedQuery<PharmacyHospitalAffiliation>(
    key,
    `/pharmacy/pharmacies/${pharmacy.id}/hospital-affiliations`,
  );
  const save = useApiMutation(
    (values: z.infer<typeof pharmacyAffiliationInputSchema>) =>
      apiFetch(`/pharmacy/pharmacies/${pharmacy.id}/hospital-affiliations/${id}`, {
        method: "PUT",
        body: JSON.stringify(values),
      }),
    {
      successToast: "Hospital affiliation declared",
      invalidate: [key],
      onSuccess: () => {
        setId(newPharmacyId());
        form.reset();
      },
      suppressErrorToast: true,
    },
  );
  return (
    <Panel className="mt-6">
      <h3 className="font-semibold">Optional hospital affiliations</h3>
      <p className="mt-2 text-sm leading-6 text-muted-foreground">
        Use the canonical hospital reference supplied during onboarding. Each declaration remains
        pending identity and authority validation. It does not share records or activate dispensing.
      </p>
      <form
        noValidate
        className="mt-4 grid items-end gap-4 sm:grid-cols-2 lg:grid-cols-3"
        onSubmit={form.handleSubmit((values) => save.mutate(values))}
      >
        <Field
          id="affiliation-hospital"
          label="Hospital reference ID"
          error={form.formState.errors.hospital_id?.message}
        >
          <Input id="affiliation-hospital" {...form.register("hospital_id")} />
        </Field>
        <FormSelectField
          control={form.control}
          name="relationship"
          id="affiliation-relationship"
          label="Relationship"
          options={[
            { value: "owner", label: "Hospital owner" },
            { value: "serving", label: "Serving hospital" },
            { value: "partner", label: "Hospital partner" },
          ]}
        />
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? "Declaring…" : "Declare hospital affiliation"}
        </Button>
        {save.isError && (
          <p role="alert" className="text-sm text-destructive">
            {save.error.message}
          </p>
        )}
      </form>
      {links.isError ? (
        <RequestFailure message="Could not load hospital affiliations." retry={links.retry} />
      ) : (
        <ul className="mt-4 divide-y text-sm">
          {links.items.map((link) => (
            <li key={link.id} className="py-3">
              <p className="break-all font-medium">{link.hospital_id}</p>
              <p className="mt-1 text-muted-foreground">
                {link.relationship} · Pending validation · No access granted
              </p>
            </li>
          ))}
        </ul>
      )}
      <LoadMore
        label="Load more hospital links"
        hasMore={links.hasMore}
        isLoading={links.isFetchingMore}
        onClick={links.loadMore}
      />
    </Panel>
  );
}
