"use client";

import { useState } from "react";
import { z } from "zod";
import { Badge, Button } from "@tesserix/web";
import { AlertTriangle } from "lucide-react";
import {
  allergyReactions,
  allergySeverities,
  allergySources,
  ApiError,
  apiFetch,
  newEncounterId,
  nonDrugCategories,
  useApiMutation,
  useApiQuery,
  usePermissions,
  type AllergyEntry,
  type AllergyStatus,
  type DrugSearchResult,
  type Hospital,
  type WeightObservation,
} from "@helivanta/api";
import { Field, FormSelectField, formatTime, RequestFailure, useZodForm } from "@helivanta/ui";

const inputClass =
  "w-full rounded-md border bg-background px-3 py-2.5 text-sm outline-none focus-visible:ring-2 focus-visible:ring-(--hms-accent)";

const labels: Record<string, string> = {
  rash: "Rash",
  urticaria: "Urticaria (hives)",
  angioedema: "Angioedema",
  anaphylaxis: "Anaphylaxis",
  bronchospasm: "Bronchospasm",
  gastrointestinal: "Gastrointestinal",
  other: "Other",
  mild: "Mild",
  moderate: "Moderate",
  severe: "Severe",
  life_threatening: "Life-threatening",
  unknown: "Severity unknown",
  patient_reported: "Patient reported",
  clinician_observed: "Clinician observed",
  prior_record: "Prior record",
  food: "Food",
  latex: "Latex",
  environmental: "Environmental",
  contrast_media: "Contrast media",
};
const options = (values: readonly string[]) =>
  values.map((value) => ({ value, label: labels[value] ?? value }));

/**
 * A patient's allergy status and latest weight at one hospital (#1373).
 * "Unknown" is shown as a warning, never as "no allergies" (IN-CL-08).
 */
export function PatientSafety({
  hospital,
  patientId,
  encounterId,
}: {
  hospital: Hospital;
  patientId: string;
  encounterId: string;
}) {
  const { can } = usePermissions();
  const base = `/medicore/hospitals/${hospital.id}/patients/${patientId}`;
  return (
    <div className="w-full space-y-4 rounded-md border p-4">
      {can("medicore.allergy.read") && (
        <Allergies
          base={base}
          encounterId={encounterId}
          canRecord={can("medicore.allergy.record")}
        />
      )}
      {can("medicore.observation.read") && (
        <Weight
          base={base}
          encounterId={encounterId}
          canRecord={can("medicore.observation.record")}
        />
      )}
    </div>
  );
}

function Allergies({
  base,
  encounterId,
  canRecord,
}: {
  base: string;
  encounterId: string;
  canRecord: boolean;
}) {
  const key = ["medicore", "allergies", base];
  const status = useApiQuery<AllergyStatus>(key, `${base}/allergies`, {
    refetchOnWindowFocus: false,
  });
  const [recording, setRecording] = useState(false);
  // One id per statement, kept across retries so a lost response replays.
  const [nkaId, setNkaId] = useState(() => newEncounterId());
  const noKnown = useApiMutation(
    () =>
      apiFetch(`${base}/no-known-allergy/${nkaId}`, {
        method: "PUT",
        body: JSON.stringify({ encounter_id: encounterId }),
      }),
    {
      successToast: "No known allergies recorded",
      invalidate: [key],
      invalidateOnError: true,
      onSuccess: () => setNkaId(newEncounterId()),
    },
  );
  if (status.isPending) return <p role="status">Loading allergies…</p>;
  if (status.isError)
    return (
      <RequestFailure message="Could not load allergies." retry={() => void status.refetch()} />
    );
  const s = status.data;
  return (
    <section aria-labelledby={`allergies-${base}`} className="space-y-2">
      <h4 id={`allergies-${base}`} className="text-sm font-semibold">
        Allergies
      </h4>
      {s.status === "unknown" && (
        <p
          role="status"
          className="flex items-center gap-2 rounded-md border border-(--hms-warn) bg-(--hms-amber-tint) px-3 py-2 text-sm font-medium"
        >
          <AlertTriangle aria-hidden className="size-4" />
          Allergies unknown: not yet asked
        </p>
      )}
      {s.status === "no_known_allergy" && s.no_known_allergy && (
        <p className="text-sm">
          No known allergies, recorded by {s.no_known_allergy.recorded_by} on{" "}
          {formatTime(s.no_known_allergy.recorded_at)}
        </p>
      )}
      {s.needs_coding && (
        <p role="alert" className="text-sm font-medium text-destructive">
          A drug allergy is recorded as free text. Prescribing checks stay blocked until it is coded
          to a drug.
        </p>
      )}
      {s.entries.length > 0 && (
        <ul className="divide-y rounded-md border" aria-label="Active allergies">
          {s.entries.map((entry) => (
            <AllergyRow key={entry.id} entry={entry} base={base} canRecord={canRecord} />
          ))}
        </ul>
      )}
      {canRecord && (
        <div className="flex flex-wrap gap-2">
          {s.recording_enabled ? (
            <Button
              variant="outline"
              size="sm"
              aria-expanded={recording}
              onClick={() => setRecording(!recording)}
            >
              Record allergy
            </Button>
          ) : (
            <p className="text-sm text-muted-foreground">
              Recording allergies opens once a clinician approves the reaction and severity lists.
            </p>
          )}
          {s.status !== "allergies_recorded" && (
            <Button
              variant="outline"
              size="sm"
              disabled={noKnown.isPending}
              onClick={() => noKnown.mutate()}
            >
              No known allergies
            </Button>
          )}
        </div>
      )}
      {recording && s.recording_enabled && (
        <RecordAllergy
          base={base}
          encounterId={encounterId}
          invalidate={key}
          onDone={() => setRecording(false)}
        />
      )}
    </section>
  );
}

function AllergyRow({
  entry,
  base,
  canRecord,
}: {
  entry: AllergyEntry;
  base: string;
  canRecord: boolean;
}) {
  const [correcting, setCorrecting] = useState(false);
  const form = useZodForm(correctionSchema, { status: "resolved", reason: "" });
  const correct = useApiMutation(
    (values: z.infer<typeof correctionSchema>) =>
      apiFetch(`${base}/allergies/${entry.id}/corrections`, {
        method: "POST",
        body: JSON.stringify({ expected_version: entry.version, ...values }),
      }),
    {
      successToast: "Allergy corrected",
      invalidate: [["medicore", "allergies", base]],
      invalidateOnError: true,
    },
  );
  const severe = entry.severity === "life_threatening" || entry.severity === "severe";
  return (
    <li className="space-y-2 px-3 py-2">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-sm">
          <span className="font-medium">{entry.substance_label}</span> · {labels[entry.reaction]} ·{" "}
          {labels[entry.source]}
          {entry.needs_coding && " · not coded"}
        </p>
        <div className="flex items-center gap-2">
          <Badge variant={severe ? "destructive" : "outline"}>{labels[entry.severity]}</Badge>
          {canRecord && (
            <Button
              variant="ghost"
              size="sm"
              aria-expanded={correcting}
              aria-label={`Correct ${entry.substance_label} allergy`}
              onClick={() => setCorrecting(!correcting)}
            >
              Correct
            </Button>
          )}
        </div>
      </div>
      {correcting && (
        <form
          noValidate
          className="grid gap-2 sm:grid-cols-3"
          onSubmit={form.handleSubmit((values) => correct.mutate(values))}
        >
          <FormSelectField
            control={form.control}
            name="status"
            id={`correct-status-${entry.id}`}
            label="Correction"
            options={[
              { value: "resolved", label: "Resolved" },
              { value: "entered_in_error", label: "Entered in error" },
            ]}
          />
          <Field
            id={`correct-reason-${entry.id}`}
            label="Reason"
            error={form.formState.errors.reason?.message}
          >
            <input
              id={`correct-reason-${entry.id}`}
              className={inputClass}
              {...form.register("reason")}
            />
          </Field>
          <div className="self-end">
            <Button type="submit" size="sm" disabled={correct.isPending}>
              Save correction
            </Button>
          </div>
        </form>
      )}
    </li>
  );
}

const correctionSchema = z.object({
  status: z.enum(["resolved", "entered_in_error"]),
  reason: z.string().trim().min(1, "Give a reason").max(500, "At most 500 characters"),
});

const allergySchema = z
  .object({
    substance_kind: z.enum(["drug_molecule", "drug_unlisted", "non_drug"]),
    molecule_id: z.string(),
    substance_label: z.string().trim().max(200, "At most 200 characters"),
    non_drug_category: z.string(),
    reaction: z.enum(allergyReactions),
    severity: z.enum(allergySeverities),
    source: z.enum(allergySources),
  })
  .superRefine((v, ctx) => {
    if (v.substance_kind === "drug_molecule" && !v.molecule_id)
      ctx.addIssue({ code: "custom", path: ["molecule_id"], message: "Choose a drug" });
    if (v.substance_kind !== "drug_molecule" && !v.substance_label)
      ctx.addIssue({ code: "custom", path: ["substance_label"], message: "Name the substance" });
    if (v.substance_kind === "non_drug" && !v.non_drug_category)
      ctx.addIssue({ code: "custom", path: ["non_drug_category"], message: "Choose a category" });
  });

function RecordAllergy({
  base,
  encounterId,
  invalidate,
  onDone,
}: {
  base: string;
  encounterId: string;
  invalidate: string[];
  onDone: () => void;
}) {
  const [entryId, setEntryId] = useState(() => newEncounterId());
  const search = useZodForm(drugSearchSchema, { q: "" });
  const [query, setQuery] = useState("");
  const drugs = useApiQuery<{ data: DrugSearchResult[] }>(
    ["drugmaster", "search", query],
    `/drugmaster/search?q=${encodeURIComponent(query)}`,
    { enabled: query !== "" },
  );
  const molecules = new Map<string, string>();
  for (const f of drugs.data?.data ?? [])
    for (const i of f.ingredients) molecules.set(i.molecule_id, i.inn_name);
  const form = useZodForm(allergySchema, {
    substance_kind: "drug_molecule",
    molecule_id: "",
    substance_label: "",
    non_drug_category: "",
    reaction: "rash",
    severity: "unknown",
    source: "patient_reported",
  });
  const kind = form.watch("substance_kind");
  const record = useApiMutation(
    (v: z.infer<typeof allergySchema>) =>
      apiFetch<AllergyEntry>(`${base}/allergies/${entryId}`, {
        method: "PUT",
        body: JSON.stringify({
          substance_kind: v.substance_kind,
          reaction: v.reaction,
          severity: v.severity,
          source: v.source,
          encounter_id: encounterId,
          ...(v.substance_kind === "drug_molecule"
            ? { molecule_id: v.molecule_id }
            : { substance_label: v.substance_label }),
          ...(v.substance_kind === "non_drug" ? { non_drug_category: v.non_drug_category } : {}),
        }),
      }),
    {
      successToast: "Allergy recorded",
      invalidate: [invalidate],
      invalidateOnError: true,
      onSuccess: () => {
        setEntryId(newEncounterId());
        onDone();
      },
    },
  );
  return (
    <div className="space-y-3 rounded-md border p-3">
      <form
        noValidate
        className="grid gap-3 sm:grid-cols-3"
        onSubmit={form.handleSubmit((v) => record.mutate(v))}
      >
        <FormSelectField
          control={form.control}
          name="substance_kind"
          id={`allergy-kind-${entryId}`}
          label="Substance"
          options={[
            { value: "drug_molecule", label: "Drug in the catalogue" },
            { value: "drug_unlisted", label: "Drug not in the catalogue" },
            { value: "non_drug", label: "Not a drug" },
          ]}
        />
        {kind === "drug_molecule" ? (
          <FormSelectField
            control={form.control}
            name="molecule_id"
            id={`allergy-molecule-${entryId}`}
            label="Drug"
            disabled={molecules.size === 0}
            options={[...molecules].map(([value, label]) => ({ value, label }))}
          />
        ) : (
          <Field
            id={`allergy-label-${entryId}`}
            label={kind === "non_drug" ? "Substance name" : "Drug name as given"}
            error={form.formState.errors.substance_label?.message}
          >
            <input
              id={`allergy-label-${entryId}`}
              className={inputClass}
              {...form.register("substance_label")}
            />
          </Field>
        )}
        {kind === "non_drug" && (
          <FormSelectField
            control={form.control}
            name="non_drug_category"
            id={`allergy-category-${entryId}`}
            label="Category"
            options={options(nonDrugCategories)}
          />
        )}
        <FormSelectField
          control={form.control}
          name="reaction"
          id={`allergy-reaction-${entryId}`}
          label="Reaction"
          options={options(allergyReactions)}
        />
        <FormSelectField
          control={form.control}
          name="severity"
          id={`allergy-severity-${entryId}`}
          label="Severity"
          options={options(allergySeverities)}
        />
        <FormSelectField
          control={form.control}
          name="source"
          id={`allergy-source-${entryId}`}
          label="Source"
          options={options(allergySources)}
        />
        <div className="sm:col-span-3">
          <Button type="submit" size="sm" disabled={record.isPending}>
            Save allergy
          </Button>
        </div>
      </form>
      {kind === "drug_molecule" && (
        <form
          noValidate
          role="search"
          className="flex flex-wrap items-end gap-2"
          onSubmit={search.handleSubmit(({ q }) => setQuery(q))}
        >
          <Field
            id={`allergy-drug-search-${entryId}`}
            label="Find the drug"
            error={search.formState.errors.q?.message}
          >
            <input
              id={`allergy-drug-search-${entryId}`}
              className={`${inputClass} w-64`}
              placeholder="Generic or brand name"
              {...search.register("q")}
            />
          </Field>
          <Button type="submit" variant="outline" size="sm">
            Search drugs
          </Button>
          {drugs.isSuccess && molecules.size === 0 && (
            <p className="text-sm text-muted-foreground">
              No drug found. Record it as a drug not in the catalogue.
            </p>
          )}
          {drugs.isError && (
            <RequestFailure
              message="Could not search the drug catalogue."
              retry={() => void drugs.refetch()}
            />
          )}
        </form>
      )}
    </div>
  );
}

const drugSearchSchema = z.object({
  q: z.string().trim().min(2, "At least two letters").max(100),
});

const weightSchema = z.object({
  value: z
    .string()
    .trim()
    .regex(/^\d{1,6}(\.\d{1,3})?$/, "A number, at most three decimals"),
  unit: z.enum(["kg", "g"]),
  method: z.enum(["measured", "estimated", "reported"]),
});

function Weight({
  base,
  encounterId,
  canRecord,
}: {
  base: string;
  encounterId: string;
  canRecord: boolean;
}) {
  const key = ["medicore", "weight", base];
  // No weight is a 404; the app's single retry is the only cost of it.
  const latest = useApiQuery<WeightObservation>(key, `${base}/weights/latest`, {
    refetchOnWindowFocus: false,
  });
  const [id, setId] = useState(() => newEncounterId());
  const [open, setOpen] = useState(false);
  const form = useZodForm(weightSchema, { value: "", unit: "kg", method: "measured" });
  const record = useApiMutation(
    (v: z.infer<typeof weightSchema>) =>
      apiFetch<WeightObservation>(`${base}/weights/${id}`, {
        method: "PUT",
        body: JSON.stringify({
          ...v,
          measured_at: new Date().toISOString().replace(/\.\d{3}Z$/, "Z"),
          encounter_id: encounterId,
        }),
      }),
    {
      successToast: "Weight recorded",
      invalidate: [key],
      invalidateOnError: true,
      onSuccess: () => {
        setId(newEncounterId());
        setOpen(false);
        form.reset({ value: "", unit: "kg", method: "measured" });
      },
    },
  );
  const none = latest.error instanceof ApiError && latest.error.status === 404;
  return (
    <section className="space-y-2" aria-labelledby={`weight-${base}`}>
      <h4 id={`weight-${base}`} className="text-sm font-semibold">
        Weight
      </h4>
      {latest.isPending ? (
        <p role="status">Loading weight…</p>
      ) : none ? (
        <p className="text-sm text-muted-foreground">No weight recorded.</p>
      ) : latest.isError ? (
        <RequestFailure message="Could not load weight." retry={() => void latest.refetch()} />
      ) : (
        <p className="text-sm">
          {latest.data.weight_kg} kg ({latest.data.method}),{" "}
          {latest.data.age_days === 0 ? "today" : `${latest.data.age_days} days ago`}
        </p>
      )}
      {canRecord && (
        <Button variant="outline" size="sm" aria-expanded={open} onClick={() => setOpen(!open)}>
          Record weight
        </Button>
      )}
      {open && (
        <form
          noValidate
          className="grid gap-3 sm:grid-cols-4"
          onSubmit={form.handleSubmit((v) => record.mutate(v))}
        >
          <Field
            id={`weight-value-${id}`}
            label="Weight"
            error={form.formState.errors.value?.message}
          >
            <input
              id={`weight-value-${id}`}
              inputMode="decimal"
              className={inputClass}
              {...form.register("value")}
            />
          </Field>
          <FormSelectField
            control={form.control}
            name="unit"
            id={`weight-unit-${id}`}
            label="Unit"
            options={[
              { value: "kg", label: "kg" },
              { value: "g", label: "g" },
            ]}
          />
          <FormSelectField
            control={form.control}
            name="method"
            id={`weight-method-${id}`}
            label="Method"
            options={[
              { value: "measured", label: "Measured" },
              { value: "estimated", label: "Estimated" },
              { value: "reported", label: "Reported" },
            ]}
          />
          <div className="self-end">
            <Button type="submit" size="sm" disabled={record.isPending}>
              Save weight
            </Button>
          </div>
        </form>
      )}
    </section>
  );
}
