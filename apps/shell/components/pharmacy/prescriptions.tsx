"use client";

import { useEffect, useRef, useState } from "react";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import {
  apiFetch,
  downloadPrescriptionImage,
  newPharmacyId,
  onTenantTransition,
  pharmacyKey,
  pharmacyRoot,
  preparePrescriptionImage,
  prescriptionImageInputSchema,
  prescriptionImagesKey,
  prescriptionInputSchema,
  retryPrescriptionImage,
  submitPrescriptionImage,
  tenantTransitionPending,
  useApiMutation,
  useApiPagedQuery,
  usePrescriptionImages,
  type PharmacyPrescription,
  type PharmacyPrescriptionImage,
  type PharmacyProduct,
  type PharmacyScope,
  type PrescriptionImageUpload,
} from "@helivanta/api";
import {
  Field,
  FormSelectField,
  LoadMore,
  Panel,
  RequestFailure,
  formatDateTime,
  useFieldArray,
  useZodForm,
} from "@helivanta/ui";

/**
 * Prescription evidence (#1386 B-2a): the pharmacist records what the
 * prescriber wrote and attaches the scanned pages. A page opens only after the
 * server has scanned and released it. Sales cite prescriptions in B-2b.
 */
export function Prescriptions({ scope, canRecord }: { scope: PharmacyScope; canRecord: boolean }) {
  const list = useApiPagedQuery<PharmacyPrescription>(
    pharmacyKey(scope, "prescriptions"),
    `${pharmacyRoot(scope)}/prescriptions?limit=50`,
  );
  const [open, setOpen] = useState<PharmacyPrescription | null>(null);
  return (
    <div className="space-y-6">
      {canRecord ? (
        <RecordPrescription scope={scope} onRecorded={setOpen} />
      ) : (
        <p className="text-sm text-muted-foreground">
          Recording prescriptions needs the pharmacist role and editor access to this outlet.
        </p>
      )}
      {open && (
        <PrescriptionPages
          key={open.id}
          scope={scope}
          prescription={open}
          canRecord={canRecord}
          onClose={() => setOpen(null)}
        />
      )}
      <Panel>
        <h3 className="font-semibold">Recorded prescriptions</h3>
        {list.isError ? (
          <RequestFailure message="Could not load prescriptions." retry={list.retry} />
        ) : list.isPending ? (
          <p role="status">Loading prescriptions…</p>
        ) : list.items.length === 0 ? (
          <p className="mt-2 text-sm text-muted-foreground">No prescriptions recorded yet.</p>
        ) : (
          <ul className="mt-3 divide-y">
            {list.items.map((rx) => (
              <li key={rx.id} className="flex flex-wrap items-center justify-between gap-3 py-3">
                <div className="min-w-0">
                  <p className="font-medium">{rx.patient_name}</p>
                  <p className="text-sm text-muted-foreground">
                    {rx.prescriber_name} · {rx.prescriber_registration} · {rx.prescribed_on}
                  </p>
                </div>
                <Button variant="outline" onClick={() => setOpen(rx)}>
                  Open pages for {rx.patient_name}
                </Button>
              </li>
            ))}
          </ul>
        )}
        <LoadMore
          label="Load more prescriptions"
          hasMore={list.hasMore}
          isLoading={list.isFetchingMore}
          onClick={list.loadMore}
        />
      </Panel>
    </div>
  );
}

// The form keeps the optional addresses as text; empty means the prescription gives none.
const formSchema = prescriptionInputSchema
  .omit({ patient_address: true, prescriber_address: true })
  .extend({
    patient_address: z.string().trim().max(500, "Use at most 500 characters"),
    prescriber_address: z.string().trim().max(500, "Use at most 500 characters"),
  });
type FormValues = z.infer<typeof formSchema>;
const empty: FormValues = {
  prescriber_name: "",
  prescriber_registration: "",
  prescriber_council: "",
  prescribed_on: "",
  patient_name: "",
  patient_address: "",
  prescriber_address: "",
  lines: [{ prescribed_as: "", product_id: "", quantity_units: 1, dispensings_allowed: 1 }],
};

function RecordPrescription({
  scope,
  onRecorded,
}: {
  scope: PharmacyScope;
  onRecorded: (rx: PharmacyPrescription) => void;
}) {
  // One id per prescription being entered, so a retry after a lost response replays it.
  const [id, setId] = useState(() => newPharmacyId());
  const form = useZodForm(formSchema, empty);
  const lines = useFieldArray({ control: form.control, name: "lines", keyName: "fieldKey" });
  const products = useApiPagedQuery<PharmacyProduct>(
    pharmacyKey(scope, "prescriptions", "products"),
    `${pharmacyRoot(scope)}/catalogue/products?limit=100&status=active`,
  );
  const save = useApiMutation(
    ({ patient_address, prescriber_address, ...values }: FormValues) =>
      apiFetch<PharmacyPrescription>(`${pharmacyRoot(scope)}/prescriptions/${id}`, {
        method: "PUT",
        body: JSON.stringify(
          prescriptionInputSchema.parse({
            ...values,
            ...(patient_address ? { patient_address } : {}),
            ...(prescriber_address ? { prescriber_address } : {}),
          }),
        ),
      }),
    {
      successToast: "Prescription recorded",
      invalidate: [pharmacyKey(scope, "prescriptions")],
      onSuccess: (rx) => {
        form.reset(empty);
        setId(newPharmacyId());
        onRecorded(rx);
      },
    },
  );
  const errors = form.formState.errors;
  return (
    <Panel>
      <form noValidate className="space-y-4" onSubmit={form.handleSubmit((v) => save.mutate(v))}>
        <h3 className="font-semibold">Record a prescription</h3>
        <p className="text-sm text-muted-foreground">
          Copy the details exactly as the prescription shows them. A recorded prescription cannot be
          changed; record a new one to correct it. Schedule H and H1 medicines are dispensed only
          when both addresses are recorded.
        </p>
        <div className="grid gap-4 sm:grid-cols-3">
          <Field id="rx-prescriber" label="Prescriber" error={errors.prescriber_name?.message}>
            <Input id="rx-prescriber" {...form.register("prescriber_name")} />
          </Field>
          <Field
            id="rx-registration"
            label="Registration number"
            error={errors.prescriber_registration?.message}
          >
            <Input id="rx-registration" {...form.register("prescriber_registration")} />
          </Field>
          <Field id="rx-council" label="Council" error={errors.prescriber_council?.message}>
            <Input id="rx-council" {...form.register("prescriber_council")} />
          </Field>
          <Field id="rx-date" label="Prescription date" error={errors.prescribed_on?.message}>
            <Input id="rx-date" type="date" {...form.register("prescribed_on")} />
          </Field>
          <Field id="rx-patient" label="Patient" error={errors.patient_name?.message}>
            <Input id="rx-patient" {...form.register("patient_name")} />
          </Field>
          <Field
            id="rx-address"
            label="Patient address (if written)"
            error={errors.patient_address?.message}
          >
            <Input id="rx-address" {...form.register("patient_address")} />
          </Field>
          <Field
            id="rx-prescriber-address"
            label="Prescriber address (if written)"
            error={errors.prescriber_address?.message}
          >
            <Input id="rx-prescriber-address" {...form.register("prescriber_address")} />
          </Field>
        </div>
        <fieldset className="space-y-3">
          <legend className="font-medium">Prescribed</legend>
          {lines.fields.map((line, i) => (
            <div key={line.fieldKey} className="grid gap-3 rounded-md border p-3 sm:grid-cols-4">
              <Field
                id={`rx-line-${i}-as`}
                label="As written"
                error={errors.lines?.[i]?.prescribed_as?.message}
              >
                <Input id={`rx-line-${i}-as`} {...form.register(`lines.${i}.prescribed_as`)} />
              </Field>
              <FormSelectField
                control={form.control}
                name={`lines.${i}.product_id`}
                id={`rx-line-${i}-product`}
                label="Dispensed as"
                options={products.items.map((p) => ({
                  value: p.id,
                  label: `${p.sku} · ${p.brand_name}`,
                }))}
              />
              <Field
                id={`rx-line-${i}-units`}
                label="Total units"
                error={errors.lines?.[i]?.quantity_units?.message}
              >
                <Input
                  id={`rx-line-${i}-units`}
                  type="number"
                  step="1"
                  {...form.register(`lines.${i}.quantity_units`, { valueAsNumber: true })}
                />
              </Field>
              <Field
                id={`rx-line-${i}-dispensings`}
                label="Times it may be dispensed"
                error={errors.lines?.[i]?.dispensings_allowed?.message}
              >
                <Input
                  id={`rx-line-${i}-dispensings`}
                  type="number"
                  step="1"
                  {...form.register(`lines.${i}.dispensings_allowed`, { valueAsNumber: true })}
                />
              </Field>
              {lines.fields.length > 1 && (
                <Button
                  type="button"
                  variant="ghost"
                  className="sm:col-span-4 justify-self-start"
                  onClick={() => lines.remove(i)}
                >
                  Remove line {i + 1}
                </Button>
              )}
            </div>
          ))}
          {errors.lines?.message && (
            <p role="alert" className="text-sm text-destructive">
              {errors.lines.message}
            </p>
          )}
          {lines.fields.length < 20 && (
            <Button
              type="button"
              variant="outline"
              onClick={() =>
                lines.append({
                  prescribed_as: "",
                  product_id: "",
                  quantity_units: 1,
                  dispensings_allowed: 1,
                })
              }
            >
              Add a prescribed line
            </Button>
          )}
          <p className="text-xs text-muted-foreground">
            A prescription is dispensed once unless the prescriber states it may be dispensed more
            often.
          </p>
        </fieldset>
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? "Recording…" : "Record prescription"}
        </Button>
      </form>
    </Panel>
  );
}

const fileSchema = z.object({
  file: z.custom<File | null>().superRefine((file, ctx) => {
    if (
      !file ||
      !prescriptionImageInputSchema.safeParse({
        filename: file.name,
        content_type: file.type,
        size: file.size,
        sha256: "0".repeat(64),
      }).success
    )
      ctx.addIssue({
        code: "custom",
        message: "Choose a PNG or JPEG image up to 10 MiB with a safe filename.",
      });
  }),
});

const failures: Record<NonNullable<PharmacyPrescriptionImage["failure_code"]>, string> = {
  malware_detected: "the scan found a threat",
  unsupported_content: "the file is not a plain image",
  integrity_mismatch: "the stored file did not match the upload",
  scanner_unavailable: "the scanner was unavailable",
  storage_unavailable: "storage was unavailable",
};
function pageStatus(image: PharmacyPrescriptionImage) {
  switch (image.status) {
    case "upload_pending":
      return "Upload incomplete";
    case "scan_pending":
      return "Scanning";
    case "available":
      return "Ready";
    case "rejected":
      return `Rejected — ${image.failure_code ? failures[image.failure_code] : "choose a new image"}`;
    case "scan_error":
      return "Scan failed";
  }
}

type Operation =
  { kind: "upload" } | { kind: "download" | "retry"; image: PharmacyPrescriptionImage };

function PrescriptionPages({
  scope,
  prescription,
  canRecord,
  onClose,
}: {
  scope: PharmacyScope;
  prescription: PharmacyPrescription;
  canRecord: boolean;
  onClose: () => void;
}) {
  const pages = usePrescriptionImages(scope, prescription.id);
  const form = useZodForm(fileSchema, { file: null });
  const input = useRef<HTMLInputElement>(null);
  const controller = useRef<AbortController | null>(null);
  // A prepared upload whose submission failed is retried with the same id.
  const prepared = useRef<PrescriptionImageUpload | null>(null);
  const urls = useRef(new Set<string>());
  useEffect(() => {
    const resources = urls.current;
    const release = () => {
      controller.current?.abort();
      for (const url of resources) URL.revokeObjectURL(url);
      resources.clear();
    };
    const unsubscribe = onTenantTransition(() => {
      if (tenantTransitionPending()) release();
    });
    return () => {
      unsubscribe();
      release();
    };
  }, []);
  const perform = async (operation: Operation) => {
    if (tenantTransitionPending()) throw new Error("Wait for your workspace to finish switching.");
    const active = new AbortController();
    controller.current = active;
    if (operation.kind === "upload") {
      const file = form.getValues("file");
      if (!file) throw new Error("Choose a page image first.");
      prepared.current ??= await preparePrescriptionImage(newPharmacyId(), file, active.signal);
      await submitPrescriptionImage(scope, prescription.id, prepared.current, active.signal);
      prepared.current = null;
      form.reset({ file: null });
      if (input.current) input.current.value = "";
    } else if (operation.kind === "retry") {
      await retryPrescriptionImage(scope, operation.image, active.signal);
    } else {
      const blob = await downloadPrescriptionImage(scope, operation.image, active.signal);
      const url = URL.createObjectURL(blob);
      urls.current.add(url);
      const link = document.createElement("a");
      link.href = url;
      link.download = operation.image.filename;
      link.click();
      setTimeout(() => {
        URL.revokeObjectURL(url);
        urls.current.delete(url);
      }, 1500);
    }
  };
  const invalidate = [prescriptionImagesKey(scope, prescription.id)];
  const upload = useApiMutation(() => perform({ kind: "upload" }), {
    successToast: "Page added",
    invalidate,
  });
  const retry = useApiMutation(
    (image: PharmacyPrescriptionImage) => perform({ kind: "retry", image }),
    { successToast: "Scan requested", invalidate },
  );
  const open = useApiMutation((image: PharmacyPrescriptionImage) =>
    perform({ kind: "download", image }),
  );
  const busy = upload.isPending || retry.isPending || open.isPending;
  const images = pages.data?.pages.flatMap((page) => page.data) ?? [];
  return (
    <Panel>
      <div className="flex items-start justify-between gap-4">
        <div>
          <h3 className="font-semibold">Pages for {prescription.patient_name}</h3>
          <p className="text-sm text-muted-foreground">
            {prescription.prescriber_name} ({prescription.prescriber_registration},{" "}
            {prescription.prescriber_council}) · {prescription.prescribed_on}
          </p>
        </div>
        <Button variant="ghost" onClick={onClose}>
          Close
        </Button>
      </div>
      <p className="mt-2 text-sm text-muted-foreground">
        Pages stay private. Each one is scanned before it can be opened.
      </p>
      {canRecord && (
        <form
          noValidate
          className="mt-4 space-y-3"
          onSubmit={form.handleSubmit(() => upload.mutate())}
        >
          <Field id="rx-page" label="Scanned page" error={form.formState.errors.file?.message}>
            <input
              ref={input}
              id="rx-page"
              type="file"
              accept="image/png,image/jpeg"
              disabled={busy}
              className="w-full min-w-0 rounded-md border p-2 text-sm"
              onChange={(event) => {
                prepared.current = null;
                upload.reset();
                form.setValue("file", event.target.files?.[0] ?? null, { shouldValidate: true });
              }}
            />
          </Field>
          <Button type="submit" disabled={busy}>
            {upload.isPending ? "Uploading…" : "Add page"}
          </Button>
        </form>
      )}
      {pages.isError ? (
        <RequestFailure message="Could not load this prescription's pages." retry={pages.refetch} />
      ) : pages.isPending ? (
        <p role="status">Loading pages…</p>
      ) : images.length === 0 ? (
        <p className="mt-4 text-sm text-muted-foreground">No pages attached yet.</p>
      ) : (
        <ul className="mt-4 divide-y">
          {images.map((image) => (
            <li key={image.id} className="flex flex-wrap items-center justify-between gap-3 py-3">
              <div className="min-w-0">
                <p className="break-all font-medium">{image.filename}</p>
                <p className="text-sm text-muted-foreground">
                  {pageStatus(image)} · {formatDateTime(image.created_at)}
                </p>
              </div>
              {image.status === "available" && (
                <Button variant="outline" disabled={busy} onClick={() => open.mutate(image)}>
                  Open {image.filename}
                </Button>
              )}
              {canRecord && image.status === "scan_error" && (
                <Button variant="outline" disabled={busy} onClick={() => retry.mutate(image)}>
                  Try again
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
      <LoadMore
        label="Load more pages"
        hasMore={pages.hasNextPage}
        isLoading={pages.isFetchingNextPage}
        onClick={() => void pages.fetchNextPage()}
      />
    </Panel>
  );
}
