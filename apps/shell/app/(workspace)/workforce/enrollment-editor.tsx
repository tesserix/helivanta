"use client";

import { useEffect, useRef, useState } from "react";
import { Button } from "@tesserix/web";
import {
  UserRound,
  Phone,
  MapPin,
  Stethoscope,
  GraduationCap,
  BriefcaseBusiness,
} from "lucide-react";
import {
  apiFetch,
  enrollmentRequestOptions,
  isEnrollmentAccessChanged,
  ENROLLMENT_REQUEST_CONTEXT_PROJECTION,
  type EnrollmentRequestScope,
  useApiQuery,
  useApiPagedQuery,
  useApiMutation,
  enrollmentSchema,
  EMPTY_ENROLLMENT,
  ENROLLMENT_PROFILE_MASTERS,
  ENROLLMENT_PROFILE_MASTERS_PROJECTION,
  ENROLLMENT_QUALIFICATION_MASTERS_PROJECTION,
  ENROLLMENT_CONTACT_MASTERS_PROJECTION,
  ENROLLMENT_CONTACT_RELATIONSHIP_MAX_LENGTH,
  EMPTY_ADDRESSES,
  EMPTY_EMERGENCY_CONTACTS,
  EMPTY_CREDENTIALS,
  EMPTY_QUALIFICATIONS,
  EMPTY_EXPERIENCES,
  JOB_LABELS,
  type HRMaster,
  type EnrollmentInput,
  type StaffEnrollment,
  type Hospital,
  type Staff,
} from "@helivanta/api";
import {
  Field,
  FormSelectField,
  useZodForm,
  useFieldArray,
  type FieldPath,
  ConfirmDialog,
  SelectControl,
} from "@helivanta/ui";

const inputClass =
  "w-full rounded-lg border bg-background px-3 py-2.5 text-sm outline-none focus-visible:ring-2 focus-visible:ring-(--hms-accent)";
const sections = [
  { id: "identity", title: "Identity & contact", icon: UserRound },
  { id: "employment", title: "Employment", icon: BriefcaseBusiness },
  { id: "addresses", title: "Addresses", icon: MapPin },
  { id: "emergency", title: "Emergency contacts", icon: Phone },
  { id: "credentials", title: "Professional credentials", icon: Stethoscope },
  { id: "history", title: "Qualifications & experience", icon: GraduationCap },
] as const;

export function EnrollmentEditor({
  hospital,
  staff,
  onClose,
  scope,
  onContextChanged,
}: {
  hospital: Hospital;
  staff?: Staff;
  onClose: () => void;
  scope: EnrollmentRequestScope;
  onContextChanged: () => void;
}) {
  const [id] = useState(() => staff?.id ?? crypto.randomUUID());
  const [reload, setReload] = useState(0);
  const path = `/workforce/hospitals/${hospital.id}/staff/${id}/enrollment?${ENROLLMENT_PROFILE_MASTERS_PROJECTION.query}=${ENROLLMENT_PROFILE_MASTERS_PROJECTION.version}&${ENROLLMENT_QUALIFICATION_MASTERS_PROJECTION.query}=${ENROLLMENT_QUALIFICATION_MASTERS_PROJECTION.version}&${ENROLLMENT_CONTACT_MASTERS_PROJECTION.query}=${ENROLLMENT_CONTACT_MASTERS_PROJECTION.version}&${ENROLLMENT_REQUEST_CONTEXT_PROJECTION.query}=${ENROLLMENT_REQUEST_CONTEXT_PROJECTION.version}`;
  const query = useApiQuery<StaffEnrollment>(
    [
      "workforce",
      "enrollment",
      scope.tenantId,
      scope.subject,
      hospital.id,
      id,
      reload,
      ENROLLMENT_PROFILE_MASTERS_PROJECTION.version,
      ENROLLMENT_QUALIFICATION_MASTERS_PROJECTION.query,
      ENROLLMENT_QUALIFICATION_MASTERS_PROJECTION.version,
      ENROLLMENT_CONTACT_MASTERS_PROJECTION.query,
      ENROLLMENT_CONTACT_MASTERS_PROJECTION.version,
    ],
    path,
    {
      enabled: !!staff,
      gcTime: 0,
      refetchOnWindowFocus: false,
      request: enrollmentRequestOptions(scope),
    },
  );
  useEffect(() => {
    if (isEnrollmentAccessChanged(query.error)) onContextChanged();
  }, [query.error, onContextChanged]);
  if (staff && query.isPending)
    return (
      <section className="mt-6 rounded-2xl border bg-card p-8" role="status">
        Loading protected enrollment…
      </section>
    );
  if (staff && (!query.data || query.isError))
    return (
      <section className="mt-6 rounded-2xl border bg-card p-8">
        <p role="alert">Could not load enrollment. Your existing record has not been changed.</p>
        <div className="mt-4 flex gap-3">
          <Button variant="outline" onClick={() => void query.refetch()}>
            Try again
          </Button>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
        </div>
      </section>
    );
  if (
    staff &&
    (query.data?.staff?.id !== id ||
      query.data?.request_context_version !== ENROLLMENT_REQUEST_CONTEXT_PROJECTION.version ||
      query.data?.profile_masters_version !== ENROLLMENT_PROFILE_MASTERS_PROJECTION.version ||
      query.data?.qualification_masters_version !==
        ENROLLMENT_QUALIFICATION_MASTERS_PROJECTION.version ||
      query.data?.contact_masters_version !== ENROLLMENT_CONTACT_MASTERS_PROJECTION.version)
  )
    return (
      <section className="mt-6 rounded-2xl border bg-card p-8">
        <p role="alert">This enrollment view is not available yet. Refresh and try again.</p>
        <div className="mt-4 flex gap-3">
          <Button variant="outline" onClick={() => void query.refetch()}>
            Try again
          </Button>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
        </div>
      </section>
    );
  const value: EnrollmentInput = query.data
    ? {
        profile: { ...EMPTY_ENROLLMENT.profile, ...query.data.profile },
        addresses: query.data.addresses,
        emergency_contacts: query.data.emergency_contacts,
        credentials: query.data.credentials,
        qualifications: query.data.qualifications,
        experiences: query.data.experiences,
        staff: {
          display_name: query.data.staff.display_name,
          employee_code: query.data.staff.employee_code,
          job_category: query.data.staff.job_category,
          employment_status: query.data.staff.employment_status,
        },
        expected_version: query.data.version,
      }
    : EMPTY_ENROLLMENT;
  return (
    <EnrollmentForm
      key={`${id}:${reload}`}
      hospital={hospital}
      scope={scope}
      onContextChanged={onContextChanged}
      path={path}
      editing={!!staff}
      value={value}
      onClose={onClose}
      onReload={() => setReload((current) => current + 1)}
    />
  );
}

function EnrollmentForm({
  hospital,
  path,
  editing,
  value,
  onClose,
  onReload,
  scope,
  onContextChanged,
}: {
  hospital: Hospital;
  scope: EnrollmentRequestScope;
  onContextChanged: () => void;
  path: string;
  editing: boolean;
  value: EnrollmentInput;
  onClose: () => void;
  onReload: () => void;
}) {
  const form = useZodForm(enrollmentSchema, value);
  const firstAddressUse = form.watch("addresses.0.use") ?? "current";
  const [confirmReload, setConfirmReload] = useState(false);
  const supervisors = useApiPagedQuery<Staff>(
    ["workforce", "supervisors", scope.tenantId, scope.subject, hospital.id],
    `/workforce/hospitals/${hospital.id}/staff`,
    { gcTime: 0, request: enrollmentRequestOptions(scope) },
  );
  const masters = useApiPagedQuery<HRMaster>(
    ["workforce", "master-choices", scope.tenantId, scope.subject, hospital.id],
    `/workforce/hospitals/${hospital.id}/hr/masters`,
    { gcTime: 0, request: enrollmentRequestOptions(scope) },
  );
  useEffect(() => {
    if (isEnrollmentAccessChanged(supervisors.error) || isEnrollmentAccessChanged(masters.error))
      onContextChanged();
  }, [supervisors.error, masters.error, onContextChanged]);
  const sectionRef = useRef<HTMLElement>(null);
  const addresses = useFieldArray({
    control: form.control,
    name: "addresses",
    keyName: "fieldKey",
  });
  const contacts = useFieldArray({
    control: form.control,
    name: "emergency_contacts",
    keyName: "fieldKey",
  });
  const credentials = useFieldArray({
    control: form.control,
    name: "credentials",
    keyName: "fieldKey",
  });
  const qualifications = useFieldArray({
    control: form.control,
    name: "qualifications",
    keyName: "fieldKey",
  });
  const experiences = useFieldArray({
    control: form.control,
    name: "experiences",
    keyName: "fieldKey",
  });
  const save = useApiMutation(
    (input: EnrollmentInput) =>
      apiFetch<StaffEnrollment>(path, {
        ...enrollmentRequestOptions(scope),
        method: "PUT",
        body: JSON.stringify({
          ...input,
          request_context_version: ENROLLMENT_REQUEST_CONTEXT_PROJECTION.version,
        }),
      }),
    {
      invalidate: [["workforce", "staff", hospital.id]],
      successToast: "Staff enrollment saved",
      onSuccess: onClose,
      suppressErrorToast: true,
    },
  );
  useEffect(() => {
    if (isEnrollmentAccessChanged(save.error)) onContextChanged();
  }, [save.error, onContextChanged]);
  useEffect(() => {
    sectionRef.current?.scrollIntoView?.({
      behavior: window.matchMedia?.("(prefers-reduced-motion: reduce)").matches
        ? "instant"
        : "smooth",
      block: "start",
    });
    form.setFocus("staff.display_name");
  }, [form]);

  const childMasterField = (kind: "qualification" | "relationship", index: number) => {
    const isContact = kind === "relationship";
    const label = isContact ? "Relationship" : "Qualification";
    const nameKey = isContact
      ? (`emergency_contacts.${index}.relationship` as const)
      : (`qualifications.${index}.qualification` as const);
    const revisionKey = isContact
      ? (`emergency_contacts.${index}.relationship_revision_id` as const)
      : (`qualifications.${index}.qualification_revision_id` as const);
    const rowID = form.watch(
      isContact
        ? (`emergency_contacts.${index}.id` as const)
        : (`qualifications.${index}.id` as const),
    );
    const revision = form.watch(revisionKey) ?? "";
    const name = form.watch(nameKey);
    const choices = masters.items.filter((row) => row.kind === kind && row.active);
    const tooLong = (row: HRMaster) =>
      isContact && Array.from(row.name).length > ENROLLMENT_CONTACT_RELATIONSHIP_MAX_LENGTH;
    if (!choices.length && !revision) return field(nameKey, label);
    const recorded =
      (isContact
        ? value.emergency_contacts.find((row) => row.id === rowID)?.relationship
        : value.qualifications.find((row) => row.id === rowID)?.qualification) ?? name;
    const id = `enrollment-${kind}-${rowID}-choice`;
    return (
      <>
        <Field
          id={id}
          label={label}
          error={form.getFieldState(revisionKey, form.formState).error?.message}
        >
          <SelectControl
            id={id}
            value={revision}
            onValueChange={(selected) => {
              const row = choices.find((choice) => choice.revision_id === selected);
              if (row && tooLong(row)) return;
              form.setValue(revisionKey, row?.revision_id ?? "", { shouldDirty: true });
              form.setValue(nameKey, row?.name ?? name, { shouldDirty: true });
            }}
            options={[
              { value: "", label: `Enter ${label.toLowerCase()} text` },
              ...choices.map((row) => ({
                value: row.revision_id,
                label: `${row.name} · ${row.code}`,
                disabled: tooLong(row),
              })),
              ...(revision && !choices.some((row) => row.revision_id === revision)
                ? [{ value: revision, label: `Recorded: ${recorded}` }]
                : []),
            ]}
          />
        </Field>
        {!revision && field(nameKey, `${label} text`)}
        {choices.some(tooLong) && (
          <p className="text-sm text-muted-foreground">
            Names longer than {ENROLLMENT_CONTACT_RELATIONSHIP_MAX_LENGTH} characters are
            unavailable for emergency contacts.
          </p>
        )}
      </>
    );
  };

  const masterField = (kind: keyof typeof ENROLLMENT_PROFILE_MASTERS, label: string) => {
    const spec = ENROLLMENT_PROFILE_MASTERS[kind];
    const key = spec.field;
    const revisionKey = `profile.${key}_revision_id` as const;
    const nameKey = `profile.${key}` as const;
    const revision = form.watch(revisionKey) ?? "";
    const name = form.watch(nameKey) ?? "";
    const choices = masters.items.filter((row) => row.kind === kind && row.active);
    if (!choices.length && !revision && !spec.selectionOnly) return field(nameKey, label);
    const recorded = revision ? (value.profile[key] ?? "") : name;
    const id = `enrollment-${key}-choice`;
    const error = form.getFieldState(revisionKey, form.formState).error?.message;
    return (
      <Field id={id} label={label} error={error}>
        <SelectControl
          id={id}
          value={revision || (!spec.selectionOnly && name ? "__recorded" : "")}
          onValueChange={(selected) => {
            if (selected === "__recorded") {
              form.setValue(revisionKey, "", { shouldDirty: true });
              form.setValue(nameKey, recorded, { shouldDirty: true });
              return;
            }
            const row = choices.find((item) => item.revision_id === selected);
            form.setValue(revisionKey, row?.revision_id ?? "", { shouldDirty: true });
            form.setValue(nameKey, row?.name ?? "", { shouldDirty: true });
          }}
          options={[
            { value: "", label: "Not assigned" },
            ...(!spec.selectionOnly && !value.profile[`${key}_revision_id`] && recorded
              ? [{ value: "__recorded", label: `Recorded text: ${recorded}` }]
              : []),
            ...choices.map((row) => ({
              value: row.revision_id,
              label: `${row.name} · ${row.code}`,
            })),
            ...(revision && !choices.some((row) => row.revision_id === revision)
              ? [{ value: revision, label: `${name} · recorded revision` }]
              : []),
          ]}
        />
      </Field>
    );
  };

  const field = (name: FieldPath<EnrollmentInput>, label: string, type = "text") => {
    const error = form.getFieldState(name, form.formState).error?.message;
    const id = `enrollment-${name.replaceAll(".", "-")}`;
    return (
      <Field key={name} id={id} label={label} error={error}>
        <input
          id={id}
          type={type}
          className={inputClass}
          aria-invalid={!!error}
          aria-describedby={error ? `${id}-error` : undefined}
          {...form.register(
            name,
            type === "number"
              ? { setValueAs: (raw: string) => (raw === "" ? 0 : Number(raw)) }
              : {},
          )}
        />
      </Field>
    );
  };
  const select = (
    name: FieldPath<EnrollmentInput>,
    label: string,
    options: { value: string; label: string }[],
  ) => (
    <FormSelectField
      key={name}
      control={form.control}
      name={name}
      id={`enrollment-${name.replaceAll(".", "-")}`}
      label={label}
      options={options}
    />
  );
  const remove = (label: string, action: () => void) => (
    <Button type="button" size="sm" variant="ghost" onClick={action} disabled={save.isPending}>
      Remove {label}
    </Button>
  );
  const sectionHeading = (id: (typeof sections)[number]["id"], subtitle: string) => {
    const { title, icon: Icon } = sections.find((section) => section.id === id)!;
    return (
      <div className="mb-5 flex gap-3">
        <span className="mt-0.5 rounded-xl bg-(--hms-accent-dim) p-2.5 text-(--hms-accent)">
          <Icon className="h-5 w-5" aria-hidden="true" />
        </span>
        <div>
          <h4 className="font-semibold">{title}</h4>
          <p className="mt-1 text-sm text-muted-foreground">{subtitle}</p>
        </div>
      </div>
    );
  };
  const cardClass = "scroll-mt-24 rounded-2xl border bg-card p-5 sm:p-6";
  const rowClass = "grid gap-4 rounded-xl border bg-muted/15 p-4 sm:grid-cols-2 lg:grid-cols-3";
  if (
    isEnrollmentAccessChanged(save.error) ||
    isEnrollmentAccessChanged(supervisors.error) ||
    isEnrollmentAccessChanged(masters.error)
  )
    return null;
  return (
    <section ref={sectionRef} aria-labelledby="staff-editor-heading" className="mt-8 scroll-mt-24">
      <header className="mb-6 flex flex-wrap items-start justify-between gap-4">
        <div>
          <h3 id="staff-editor-heading" className="text-xl font-semibold">
            {editing ? "Edit staff enrollment" : "Enroll a staff member"}
          </h3>
          <p className="mt-2 text-sm text-muted-foreground">
            {hospital.name} · Personal details stay within authorized enrollment access.
          </p>
        </div>
        <Button variant="ghost" onClick={onClose} disabled={save.isPending}>
          Cancel
        </Button>
      </header>
      <nav aria-label="Enrollment sections" className="mb-6 flex flex-wrap gap-2">
        {sections.map(({ id, title }) => (
          <Button
            key={id}
            type="button"
            variant="outline"
            size="sm"
            onClick={() => {
              const target = sectionRef.current?.querySelector<HTMLElement>(`#staff-${id}`);
              target?.scrollIntoView?.({ behavior: "smooth", block: "start" });
              target?.querySelector<HTMLElement>("input,button")?.focus({ preventScroll: true });
            }}
          >
            {title}
          </Button>
        ))}
      </nav>
      <form
        noValidate
        autoComplete="off"
        onSubmit={form.handleSubmit((input) => save.mutate(input))}
        className="space-y-5"
      >
        <fieldset disabled={save.isPending} className="contents">
          <section id="staff-identity" className={cardClass}>
            {sectionHeading(
              "identity",
              "Full name and employee code are required. Collect other details only when needed.",
            )}
            <div className="grid gap-5 sm:grid-cols-2 lg:grid-cols-3">
              {field("staff.display_name", "Full name")}
              {field("staff.employee_code", "Employee code")}
              {field("profile.preferred_name", "Preferred name")}
              {field("profile.given_name", "Given name")}
              {field("profile.family_name", "Family name")}
              {field("profile.date_of_birth", "Date of birth", "date")}
              {select("profile.gender", "Gender", [
                { value: "", label: "Not recorded" },
                { value: "female", label: "Female" },
                { value: "male", label: "Male" },
                { value: "other", label: "Other" },
                { value: "prefer_not_to_say", label: "Prefer not to say" },
              ])}
              {field("profile.work_email", "Work email", "email")}
              {field("profile.personal_email", "Personal email", "email")}
              {field("profile.mobile_phone", "Mobile number", "tel")}
              {field("profile.work_phone", "Work phone", "tel")}
              {masterField("language", "Preferred language")}
              {select("profile.blood_group", "Reported blood group", [
                { value: "", label: "Not known / not recorded" },
                ...["A+", "A-", "B+", "B-", "AB+", "AB-", "O+", "O-"].map((value) => ({
                  value,
                  label: value,
                })),
              ])}
            </div>
            <p className="mt-4 text-xs text-muted-foreground">
              Phone numbers include country codes. Blood group is optional and reported; it is not a
              verified clinical result or a donor assessment.
            </p>
          </section>
          <section id="staff-employment" className={cardClass}>
            {sectionHeading(
              "employment",
              "Employment details do not grant application access or clinical privileges.",
            )}
            <div className="grid gap-5 sm:grid-cols-2 lg:grid-cols-3">
              {select(
                "staff.job_category",
                "Job category",
                Object.entries(JOB_LABELS).map(([value, label]) => ({ value, label })),
              )}
              {select("staff.employment_status", "Employment status", [
                { value: "active", label: "Active" },
                { value: "inactive", label: "Inactive" },
              ])}
              {select(
                "profile.engagement_type",
                "Engagement type",
                ["employee", "consultant", "contractor", "locum", "trainee", "volunteer"].map(
                  (value) => ({ value, label: value[0]!.toUpperCase() + value.slice(1) }),
                ),
              )}
              {masterField("contract_type", "Contract type")}
              {masterField("department", "Department")}
              {masterField("designation", "Designation")}
              {select("profile.supervisor_id", "Supervisor", [
                { value: "", label: "Not assigned" },
                ...supervisors.items
                  .filter((row) => !path.includes(`/staff/${row.id}/`))
                  .map((row) => ({
                    value: row.id,
                    label: `${row.display_name} · ${row.employee_code}${row.employment_status === "inactive" ? " · Inactive" : ""}`,
                  })),
                ...(value.profile.supervisor_id &&
                !supervisors.items.some((row) => row.id === value.profile.supervisor_id)
                  ? [
                      {
                        value: value.profile.supervisor_id,
                        label: "Current supervisor (load more to view name)",
                      },
                    ]
                  : []),
              ])}
              {masterField("grade", "Grade")}
              {field("profile.start_date", "Start date", "date")}
              {field("profile.end_date", "End date", "date")}
              {field("profile.probation_end_date", "Probation end date", "date")}
              {field("profile.confirmation_date", "Confirmation date", "date")}
              {masterField("exit_reason", "Exit reason")}
            </div>
          </section>
          {masters.hasMore && (
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={masters.isFetchingMore}
              onClick={masters.loadMore}
            >
              Load more HR choices
            </Button>
          )}
          {masters.isError && (
            <p className="text-sm text-muted-foreground">
              Could not load HR choices. Existing text and recorded assignments are preserved.{" "}
              <Button type="button" variant="ghost" size="sm" onClick={masters.retry}>
                Retry HR choices
              </Button>
            </p>
          )}
          {supervisors.hasMore && (
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={supervisors.isFetchingMore}
              onClick={supervisors.loadMore}
            >
              Load more supervisors
            </Button>
          )}
          {supervisors.isError && (
            <p className="text-sm text-muted-foreground">
              Could not load supervisor choices.{" "}
              <Button type="button" variant="ghost" size="sm" onClick={supervisors.retry}>
                Retry supervisor list
              </Button>
            </p>
          )}
          <section id="staff-addresses" className={cardClass}>
            {sectionHeading(
              "addresses",
              "Keep one current and one permanent address, with international country codes.",
            )}
            <div className="space-y-4">
              {addresses.fields.map((row, i) => (
                <div key={row.fieldKey} className={rowClass}>
                  {select(`addresses.${i}.use`, "Address type", [
                    { value: "current", label: "Current" },
                    { value: "permanent", label: "Permanent" },
                  ])}
                  {field(`addresses.${i}.line1`, "Address line 1")}
                  {field(`addresses.${i}.line2`, "Address line 2")}
                  {field(`addresses.${i}.city`, "City")}
                  {field(`addresses.${i}.region`, "State / region")}
                  {field(`addresses.${i}.postal_code`, "Postal code")}
                  {field(`addresses.${i}.country`, "Country code")}
                  <div className="flex flex-wrap gap-2 sm:col-span-2 lg:col-span-3">
                    {remove("address", () => addresses.remove(i))}
                    {i === 0 && addresses.fields.length === 1 && (
                      <Button
                        type="button"
                        size="sm"
                        variant="outline"
                        onClick={() =>
                          addresses.append({
                            ...form.getValues(`addresses.${i}`),
                            id: crypto.randomUUID(),
                            use: firstAddressUse === "current" ? "permanent" : "current",
                          })
                        }
                      >
                        Copy to {firstAddressUse === "current" ? "permanent" : "current"} address
                      </Button>
                    )}
                  </div>
                </div>
              ))}
            </div>
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="mt-4"
              disabled={addresses.fields.length >= 2}
              onClick={() =>
                addresses.append({
                  ...EMPTY_ADDRESSES,
                  id: crypto.randomUUID(),
                  use: form.getValues("addresses").some((row) => row.use === "current")
                    ? "permanent"
                    : "current",
                })
              }
            >
              Add address
            </Button>
          </section>
          <section id="staff-emergency" className={cardClass}>
            {sectionHeading(
              "emergency",
              "Name the person to contact, their relationship and the order in which to call.",
            )}
            <div className="space-y-4">
              {contacts.fields.map((row, i) => (
                <div key={row.fieldKey} className={rowClass}>
                  {field(`emergency_contacts.${i}.name`, "Contact name")}
                  {childMasterField("relationship", i)}
                  {field(`emergency_contacts.${i}.phone`, "Emergency phone", "tel")}
                  {field(`emergency_contacts.${i}.email`, "Emergency email", "email")}
                  {field(`emergency_contacts.${i}.priority`, "Contact priority", "number")}
                  <div className="self-end">{remove("contact", () => contacts.remove(i))}</div>
                </div>
              ))}
            </div>
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="mt-4"
              disabled={contacts.fields.length >= 5}
              onClick={() =>
                contacts.append({
                  ...EMPTY_EMERGENCY_CONTACTS,
                  id: crypto.randomUUID(),
                  priority:
                    [1, 2, 3, 4, 5].find(
                      (priority) =>
                        !form
                          .getValues("emergency_contacts")
                          .some((row) => row.priority === priority),
                    ) ?? 1,
                })
              }
            >
              Add emergency contact
            </Button>
          </section>
          <section id="staff-credentials" className={cardClass}>
            {sectionHeading(
              "credentials",
              "Record professional registration and validity. Review and privileging remain separate.",
            )}
            <div className="space-y-4">
              {credentials.fields.map((row, i) => (
                <div key={row.fieldKey} className={rowClass}>
                  {field(`credentials.${i}.type`, "Credential type")}
                  {field(`credentials.${i}.number`, "Registration number")}
                  {field(`credentials.${i}.issuer`, "Issuing authority")}
                  {field(`credentials.${i}.country`, "Credential country")}
                  {field(`credentials.${i}.specialty`, "Specialty")}
                  {field(`credentials.${i}.valid_from`, "Valid from", "date")}
                  {field(`credentials.${i}.valid_until`, "Valid until", "date")}
                  <div className="self-end">
                    {remove("credential", () => credentials.remove(i))}
                  </div>
                </div>
              ))}
            </div>
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="mt-4"
              disabled={credentials.fields.length >= 20}
              onClick={() => credentials.append({ ...EMPTY_CREDENTIALS, id: crypto.randomUUID() })}
            >
              Add credential
            </Button>
          </section>
          <section id="staff-history" className={cardClass}>
            {sectionHeading(
              "history",
              "Record relevant qualifications and previous employment, without salary or unnecessary identifiers.",
            )}
            <div className="space-y-4">
              {qualifications.fields.map((row, i) => (
                <div key={row.fieldKey} className={rowClass}>
                  {childMasterField("qualification", i)}
                  {field(`qualifications.${i}.institution`, "Institution")}
                  {field(`qualifications.${i}.country`, "Qualification country")}
                  {field(`qualifications.${i}.completion_year`, "Completion year", "number")}
                  <div className="self-end">
                    {remove("qualification", () => qualifications.remove(i))}
                  </div>
                </div>
              ))}
            </div>
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="mt-4"
              disabled={qualifications.fields.length >= 20}
              onClick={() =>
                qualifications.append({ ...EMPTY_QUALIFICATIONS, id: crypto.randomUUID() })
              }
            >
              Add qualification
            </Button>
            <div className="mt-6 space-y-4">
              {experiences.fields.map((row, i) => (
                <div key={row.fieldKey} className={rowClass}>
                  {field(`experiences.${i}.employer`, "Previous employer")}
                  {field(`experiences.${i}.role`, "Previous role")}
                  {field(`experiences.${i}.start_date`, "Experience start date", "date")}
                  {field(`experiences.${i}.end_date`, "Experience end date", "date")}
                  <div className="self-end">
                    {remove("experience", () => experiences.remove(i))}
                  </div>
                </div>
              ))}
            </div>
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="mt-4"
              disabled={experiences.fields.length >= 20}
              onClick={() => experiences.append({ ...EMPTY_EXPERIENCES, id: crypto.randomUUID() })}
            >
              Add experience
            </Button>
          </section>
        </fieldset>
        <div className="sticky bottom-3 z-10 flex flex-wrap items-center justify-between gap-4 rounded-xl border bg-card/95 p-4 shadow-lg backdrop-blur">
          <div className="max-w-xl text-sm text-muted-foreground">
            {save.isError ? (
              <p role="alert" className="text-destructive">
                {save.error.message}
              </p>
            ) : (
              "Required: full name and employee code. Optional sections can be completed later."
            )}
          </div>
          <div className="flex flex-wrap gap-3">
            {editing && save.isError && (
              <Button type="button" variant="outline" onClick={() => setConfirmReload(true)}>
                Reload latest profile
              </Button>
            )}
            <Button type="submit" disabled={save.isPending}>
              {save.isPending ? "Saving…" : "Save staff"}
            </Button>
          </div>
        </div>
      </form>
      <ConfirmDialog
        open={confirmReload}
        onOpenChange={setConfirmReload}
        title="Reload the latest profile?"
        description="Your unsaved changes will be discarded. The saved enrollment will not be changed."
        confirmLabel="Discard edits and reload"
        onConfirm={onReload}
      />
    </section>
  );
}
