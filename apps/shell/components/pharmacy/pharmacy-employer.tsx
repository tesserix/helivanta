"use client";
import { Button } from "@tesserix/web";
import {
  grantPharmacyManagerAccess,
  pharmacyEmployerKey,
  registerPharmacyEmployer,
  useApiMutation,
  useApiQuery,
  usePharmacyEmployer,
  type PharmacyOrganization,
} from "@helivanta/api";
import { Panel, RequestFailure } from "@helivanta/ui";

export function PharmacyEmployer({
  tenantId,
  pharmacy,
  canManage,
}: {
  tenantId: string;
  pharmacy: PharmacyOrganization;
  canManage: boolean;
}) {
  const standalone = pharmacy.ownership_type !== "hospital_owned";
  const employer = usePharmacyEmployer(tenantId, pharmacy.id, standalone && canManage);
  const identity = useApiQuery<{ subject: string }>(
    ["iam", "me", "permissions"],
    "/iam/me/permissions",
    { enabled: standalone && canManage },
  );
  // Both writes are idempotent PUTs, so a retry after a failed grant repeats them safely.
  const register = useApiMutation(
    async () => {
      const subject = identity.data?.subject;
      if (!subject) throw new Error("Your account could not be verified. Reload and try again.");
      const registered = await registerPharmacyEmployer(pharmacy.id);
      await grantPharmacyManagerAccess(pharmacy.id, subject);
      return registered;
    },
    {
      successToast: "Pharmacy registered with you as its Staff manager",
      invalidate: [pharmacyEmployerKey(tenantId, pharmacy.id)],
      invalidateOnError: true,
      suppressErrorToast: true,
    },
  );
  return (
    <Panel aria-labelledby="pharmacy-employer" className="mt-6">
      <h3 id="pharmacy-employer" className="font-semibold">
        Employer
      </h3>
      {!standalone ? (
        <p className="mt-2 text-sm leading-6 text-muted-foreground">
          Staff at this pharmacy are employed by the hospital that owns it.
        </p>
      ) : !canManage ? (
        <p className="mt-2 text-sm leading-6 text-muted-foreground">
          Employer registration is managed by people with hospital management access.
        </p>
      ) : employer.isPending ? (
        <p className="mt-2 text-sm text-muted-foreground">Checking employer registration…</p>
      ) : employer.isError ? (
        <RequestFailure
          message="Could not load employer registration."
          retry={employer.refetch}
          error={employer.error}
        />
      ) : employer.data ? (
        <div className="mt-2 space-y-3">
          <p className="text-sm leading-6">
            Registered as an employer on{" "}
            {new Date(employer.data.created_at).toLocaleDateString("en-IN", {
              dateStyle: "medium",
            })}
            .
          </p>
          {register.isError && (
            <>
              <p role="alert" className="text-sm text-destructive">
                {register.error.message}
              </p>
              <Button
                type="button"
                variant="outline"
                disabled={register.isPending}
                onClick={() => register.mutate()}
              >
                Retry manager access
              </Button>
            </>
          )}
        </div>
      ) : (
        <div className="mt-2 space-y-3">
          <p className="text-sm leading-6 text-muted-foreground">
            This pharmacy employs its own staff but is not yet registered as an employer. The name
            and ownership come from its pharmacy record.
          </p>
          {register.isError && (
            <p role="alert" className="text-sm text-destructive">
              {register.error.message}
            </p>
          )}
          <Button
            type="button"
            disabled={register.isPending || !identity.data}
            onClick={() => register.mutate()}
          >
            {register.isPending ? "Registering…" : "Register as employer"}
          </Button>
        </div>
      )}
    </Panel>
  );
}
