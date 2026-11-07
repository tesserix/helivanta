"use client";
import { useEffect, useState } from "react";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import {
  Field,
  FormSelectField,
  LoadMore,
  Panel,
  RequestFailure,
  useLeaveGuard,
  useZodForm,
} from "@helivanta/ui";
import {
  ApiError,
  PartnerGrantDraftInputContract,
  newPartnerGrantDraftId,
  partnerGrantDraftsKey,
  preparePartnerGrantDraft,
  recoverPartnerGrantDraft,
  membershipRequestOptions,
  useApiMutation,
  useApiPagedQuery,
  type PartnerGrantDraft,
  type PartnerGrantDraftInput,
  type PartnerGrantDraftScope,
  type PharmacyOrganization,
  type PharmacyOutlet,
  type SupplyPartySummary,
  type SupplyPremise,
} from "@helivanta/api";
import { accessChanged } from "./errors";

const date = z
  .string()
  .min(1, "Choose a date and time.")
  .refine((v) => Number.isFinite(Date.parse(v)), "Choose a valid date and time.")
  .transform((value) => new Date(value).toISOString());
const schema = PartnerGrantDraftInputContract.safeExtend({ valid_from: date, expires_at: date });
function localTime(value: Date) {
  return new Date(value.getTime() - value.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
}

export function PartnerGrantDraftForm({
  scope,
  onSaved,
  onChanged,
  onCancel,
}: {
  scope: PartnerGrantDraftScope;
  onSaved: (draft: PartnerGrantDraft, recovered: boolean) => void;
  onChanged: () => void;
  onCancel: () => void;
}) {
  const [id] = useState(() => newPartnerGrantDraftId());
  const [openedAt] = useState(() => Date.now());
  const [retained, setRetained] = useState<PartnerGrantDraftInput | null>(null);
  const form = useZodForm(schema, {
    owner_product: "pharmacy",
    owner_organization_id: "",
    source_location_id: "",
    relationship_id: "",
    relationship_version: 1,
    recipient_kind: "group",
    recipient_tenant_id: "",
    recipient_hospital_id: "",
    template_key: "partner.shipment_reader.v1",
    source_jurisdiction: "IN-MH",
    destination_jurisdiction: "IN-OD",
    valid_from: localTime(new Date(openedAt)),
    expires_at: localTime(new Date(openedAt + 30 * 86400000)),
    reason: "",
  });
  const product = form.watch("owner_product"),
    organization = form.watch("owner_organization_id"),
    kind = form.watch("recipient_kind");
  const request = { headers: membershipRequestOptions(scope).headers, cache: "no-store" as const };
  const organizations = useApiPagedQuery<PharmacyOrganization | SupplyPartySummary>(
    [...partnerGrantDraftsKey(scope), "source-organizations", product],
    product === "pharmacy" ? "/pharmacy/pharmacies" : "/supply/parties",
    { gcTime: 0, enabled: !retained, request },
  );
  const choices = organizations.items.filter((row) =>
    "ownership_type" in row
      ? row.ownership_type !== "hospital_owned"
      : row.record_kind === "owned_organisation" && row.country_code === "IN",
  );
  const organizationCurrent =
    !organizations.isError && choices.some((row) => row.id === organization);
  const locations = useApiPagedQuery<PharmacyOutlet | SupplyPremise>(
    [...partnerGrantDraftsKey(scope), "source-locations", product, organization],
    product === "pharmacy"
      ? `/pharmacy/pharmacies/${organization}/outlets`
      : `/supply/parties/${organization}/premises`,
    { gcTime: 0, enabled: organizationCurrent && !retained, request },
  );
  const locationsCurrent = locations.items.filter((row) => row.country_code === "IN");
  const selectedLocation = form.watch("source_location_id");
  const sourceCurrent =
    organizationCurrent &&
    !locations.isError &&
    locationsCurrent.some((row) => row.id === selectedLocation);
  useEffect(() => {
    form.resetField("owner_organization_id");
    form.resetField("source_location_id");
  }, [product, form]);
  useEffect(() => {
    form.resetField("source_location_id");
  }, [organization, form]);
  useEffect(() => {
    form.unregister(kind === "group" ? "recipient_subject" : "recipient_group_id");
  }, [kind, form]);
  useEffect(() => {
    if (accessChanged(organizations.error) || accessChanged(locations.error)) onChanged();
  }, [organizations.error, locations.error, onChanged]);
  const fail = (error: Error) => {
    if (accessChanged(error)) onChanged();
  };
  const save = useApiMutation(
    (input: PartnerGrantDraftInput) => preparePartnerGrantDraft(scope, id, input),
    {
      invalidate: [partnerGrantDraftsKey(scope)],
      onSuccess: (draft) => onSaved(draft, false),
      onError: fail,
      suppressErrorToast: true,
    },
  );
  const recover = useApiMutation(
    () => {
      if (!retained) throw new Error("No supplier access save is awaiting recovery.");
      return recoverPartnerGrantDraft(scope, id, retained);
    },
    {
      invalidate: [partnerGrantDraftsKey(scope)],
      onSuccess: (draft) => onSaved(draft, true),
      onError: fail,
      suppressErrorToast: true,
    },
  );
  const busy = save.isPending || recover.isPending;
  useLeaveGuard(form.formState.isDirty || !!retained, "supplier access proposal", busy);
  const refusal = save.error instanceof ApiError && [400, 404, 409].includes(save.error.status);
  const references = [
    ["relationship_id", "Relationship reference"],
    ["recipient_tenant_id", "Hospital workspace reference"],
    ["recipient_hospital_id", "Hospital reference"],
  ] as const;
  return (
    <Panel>
      <form
        noValidate
        className="space-y-4"
        onSubmit={form.handleSubmit((values) => {
          if (!retained && !sourceCurrent) return;
          const input = retained ?? PartnerGrantDraftInputContract.parse(values);
          setRetained(input);
          save.mutate(input);
        })}
      >
        <h2 className="text-lg font-semibold">Prepare supplier access</h2>
        <p className="text-sm text-muted-foreground">
          Choose your own supplying organization and location. Enter recipient and relationship
          references supplied for verification. This proposal shares no data and grants no stock,
          clinical or payment authority.
        </p>
        <fieldset disabled={!!retained} className="space-y-4">
          <FormSelectField
            control={form.control}
            name="owner_product"
            id="grant-owner-product"
            label="Stock owner"
            options={[
              { value: "pharmacy", label: "PharmaConnect pharmacy outlet" },
              { value: "supply", label: "SupplyConnect distributor premise" },
            ]}
          />
          {organizations.isPending ? (
            <p role="status">Loading permitted suppliers…</p>
          ) : organizations.isError ? (
            <RequestFailure
              message="Supplier choices are unavailable."
              error={organizations.error}
              retry={organizations.retry}
            />
          ) : (
            <>
              <FormSelectField
                control={form.control}
                name="owner_organization_id"
                id="grant-organization"
                label="Supplier organization"
                options={choices.map((row) => ({
                  value: row.id,
                  label: "name" in row ? row.name : row.legal_name,
                }))}
              />
              <LoadMore
                hasMore={organizations.hasMore}
                isLoading={organizations.isFetchingMore}
                onClick={organizations.loadMore}
                label="Load more supplier organizations"
              />
            </>
          )}
          {organizationCurrent &&
            (locations.isPending ? (
              <p role="status">Loading supplying locations…</p>
            ) : locations.isError ? (
              <RequestFailure
                message="Supplying locations are unavailable."
                error={locations.error}
                retry={locations.retry}
              />
            ) : (
              <>
                <FormSelectField
                  control={form.control}
                  name="source_location_id"
                  id="grant-location"
                  label="Supplying location"
                  options={locationsCurrent.map((row) => ({ value: row.id, label: row.name }))}
                />
                <LoadMore
                  hasMore={locations.hasMore}
                  isLoading={locations.isFetchingMore}
                  onClick={locations.loadMore}
                  label="Load more supplying locations"
                />
              </>
            ))}
          {references.map(([name, label]) => (
            <Field
              key={name}
              id={`grant-${name}`}
              label={label}
              error={form.formState.errors[name]?.message}
            >
              <Input id={`grant-${name}`} {...form.register(name)} />
            </Field>
          ))}
          <Field
            id="grant-relationship-version"
            label="Relationship revision"
            error={form.formState.errors.relationship_version?.message}
          >
            <Input
              id="grant-relationship-version"
              type="number"
              min="1"
              step="1"
              {...form.register("relationship_version", { valueAsNumber: true })}
            />
          </Field>
          <FormSelectField
            control={form.control}
            name="recipient_kind"
            id="grant-recipient-kind"
            label="Recipient"
            options={[
              { value: "group", label: "Hospital-managed team" },
              { value: "subject", label: "Named hospital account" },
            ]}
          />
          {kind === "group" ? (
            <Field
              id="grant-group"
              label="Published hospital team reference"
              error={form.formState.errors.recipient_group_id?.message}
            >
              <Input id="grant-group" {...form.register("recipient_group_id")} />
            </Field>
          ) : (
            <Field
              id="grant-subject"
              label="Named hospital account reference"
              error={form.formState.errors.recipient_subject?.message}
            >
              <Input id="grant-subject" {...form.register("recipient_subject")} />
            </Field>
          )}
          <FormSelectField
            control={form.control}
            name="template_key"
            id="grant-template"
            label="Requested access"
            options={[
              { value: "partner.catalogue_viewer.v1", label: "View agreed catalogue" },
              { value: "partner.shipment_reader.v1", label: "View this hospital’s shipments" },
              { value: "partner.invoice_viewer.v1", label: "View this hospital’s invoices" },
            ]}
          />
          {(["source_jurisdiction", "destination_jurisdiction"] as const).map((name) => (
            <FormSelectField
              key={name}
              control={form.control}
              name={name}
              id={`grant-${name}`}
              label={
                name === "source_jurisdiction"
                  ? "Requested supplying state"
                  : "Requested receiving state"
              }
              options={[
                { value: "IN-MH", label: "Maharashtra" },
                { value: "IN-OD", label: "Odisha" },
              ]}
            />
          ))}
          {(["valid_from", "expires_at"] as const).map((name) => (
            <Field
              key={name}
              id={`grant-${name}`}
              label={name === "valid_from" ? "Starts" : "Expires"}
              error={form.formState.errors[name]?.message}
            >
              <Input id={`grant-${name}`} type="datetime-local" {...form.register(name)} />
            </Field>
          ))}
          <Field id="grant-reason" label="Reason" error={form.formState.errors.reason?.message}>
            <Input id="grant-reason" {...form.register("reason")} />
          </Field>
        </fieldset>
        {save.isError && (
          <p role="alert">
            {refusal
              ? save.error.message
              : "The save may have been recorded. Check the saved proposal or retry this same save before starting another."}
          </p>
        )}
        {recover.isError && (
          <p role="alert">
            {recover.error instanceof ApiError && recover.error.status === 404
              ? "No saved proposal was found yet. Retry the same save to reconcile its outcome."
              : "The retained proposal could not be verified. Keep this proposal and retry recovery."}
          </p>
        )}
        <div className="flex flex-wrap gap-3">
          <Button
            type={retained ? "button" : "submit"}
            disabled={busy || (!retained && !sourceCurrent)}
            onClick={retained ? () => save.mutate(retained) : undefined}
          >
            {retained ? "Retry same proposal" : "Save supplier access proposal"}
          </Button>
          {retained && (
            <Button
              type="button"
              variant="outline"
              disabled={busy}
              onClick={() => recover.mutate()}
            >
              Check saved proposal
            </Button>
          )}
          <Button type="button" variant="outline" disabled={busy} onClick={onCancel}>
            Close preparation
          </Button>
        </div>
      </form>
    </Panel>
  );
}
