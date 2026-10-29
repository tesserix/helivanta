"use client";
import { useState } from "react";
import { z } from "zod";
import { Button } from "@tesserix/web";
import {
  apiFetch,
  pharmacyLookupGrantSchema,
  useApiMutation,
  useApiPagedQuery,
  usePermissions,
  type PharmacyLookupGrant,
} from "@helivanta/api";
import {
  ConfirmDialog,
  FormSelectField,
  LoadMore,
  RequestFailure,
  useZodForm,
} from "@helivanta/ui";

type Member = { id: string; subject: string; role_key: string };

/**
 * Named pharmacists who may look up this hospital's patients by MRN for one
 * verified pharmacy (#1416b). Grants end with the link; re-verifying it starts
 * with none.
 */
export function PharmacyLookupAccess({
  hospitalId,
  pharmacyId,
  pharmacyName,
}: {
  hospitalId: string;
  pharmacyId: string;
  pharmacyName: string;
}) {
  const { can } = usePermissions();
  const key = ["patients", "pharmacy-lookup-access", hospitalId, pharmacyId];
  const grants = useApiPagedQuery<PharmacyLookupGrant>(
    key,
    `/patients/hospitals/${hospitalId}/pharmacy-lookup-access?pharmacy_id=${pharmacyId}`,
  );
  const members = useApiPagedQuery<Member>(["iam", "members", "pharmacy-lookup"], "/iam/members", {
    enabled: can("iam.member.manage"),
  });
  const [removing, setRemoving] = useState<string | null>(null);
  const form = useZodForm(pharmacyLookupGrantSchema, { subject: "" });
  const path = (subject: string) =>
    `/patients/hospitals/${hospitalId}/pharmacy-lookup-access/${pharmacyId}/${encodeURIComponent(subject)}`;
  const grant = useApiMutation(
    (values: z.infer<typeof pharmacyLookupGrantSchema>) =>
      apiFetch(path(values.subject), { method: "PUT", body: "{}" }),
    {
      successToast: "Lookup access granted",
      invalidate: [key],
      invalidateOnError: true,
      onSuccess: () => form.reset(),
      suppressErrorToast: true,
    },
  );
  const revoke = useApiMutation(
    (subject: string) => apiFetch(path(subject), { method: "DELETE" }),
    {
      successToast: "Lookup access revoked",
      invalidate: [key],
      onSuccess: () => setRemoving(null),
      suppressErrorToast: true,
    },
  );
  const granted = new Set(grants.items.map((g) => g.subject));
  // Only pharmacists hold patient.pharmacy-lookup; a grant to anyone else would do nothing.
  const options = [
    ...new Set(
      (members.items ?? []).filter((m) => m.role_key === "pharmacist").map((m) => m.subject),
    ),
  ]
    .filter((subject) => !granted.has(subject))
    .map((subject) => ({ value: subject, label: subject }));
  return (
    <div className="mt-3 rounded-md border px-4 py-3">
      <p className="text-sm font-medium">Patient lookup by MRN</p>
      <p className="mt-1 text-xs leading-5 text-muted-foreground">
        Named pharmacists at {pharmacyName} can find one of this hospital&apos;s patients by exact
        MRN and see only the name, age and sex. Every lookup is recorded.
      </p>
      {grants.isError ? (
        <RequestFailure message="Could not load lookup access." retry={grants.retry} />
      ) : grants.items.length === 0 && !grants.isPending ? (
        <p className="mt-2 text-sm text-muted-foreground">No pharmacist has lookup access.</p>
      ) : (
        <ul className="mt-2 divide-y text-sm">
          {grants.items.map((g) => (
            <li key={g.subject} className="flex items-center justify-between gap-3 py-2">
              <span className="break-all">{g.subject}</span>
              <Button variant="outline" size="sm" onClick={() => setRemoving(g.subject)}>
                Revoke
              </Button>
            </li>
          ))}
        </ul>
      )}
      <LoadMore
        label="Load more pharmacists"
        hasMore={grants.hasMore}
        isLoading={grants.isFetchingMore}
        onClick={grants.loadMore}
      />
      {can("iam.member.manage") && (
        <form
          noValidate
          className="mt-3 flex flex-wrap items-end gap-3"
          onSubmit={form.handleSubmit((values) => grant.mutate(values))}
        >
          <FormSelectField
            control={form.control}
            name="subject"
            id={`lookup-subject-${pharmacyId}`}
            label="Pharmacist"
            disabled={grant.isPending}
            options={options}
          />
          <Button type="submit" size="sm" disabled={grant.isPending}>
            {grant.isPending ? "Granting…" : "Grant lookup access"}
          </Button>
          <LoadMore
            label="Load more members"
            hasMore={members.hasMore}
            isLoading={members.isFetchingMore}
            onClick={members.loadMore}
          />
        </form>
      )}
      {(grant.error ?? revoke.error) && (
        <p role="alert" className="mt-2 text-sm text-destructive">
          {(grant.error ?? revoke.error)?.message}
        </p>
      )}
      <ConfirmDialog
        open={removing !== null}
        onOpenChange={(open) => !open && setRemoving(null)}
        title={`Revoke ${removing ?? ""}'s lookup access?`}
        description="Their next lookup is refused."
        confirmLabel="Revoke"
        busy={revoke.isPending}
        onConfirm={() => removing && revoke.mutate(removing)}
      />
    </div>
  );
}
