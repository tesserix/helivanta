"use client";
import { useId, useState } from "react";
import { Button, Input } from "@tesserix/web";
import {
  Panel,
  Field,
  RequestFailure,
  SelectControl,
  useConfirmLeave,
  useLeaveGuard,
} from "@helivanta/ui";
import {
  ApiError,
  useApiQuery,
  useApiPagedQuery,
  useApiMutation,
  useIndependentStaffLinksPage,
  prepareOperatingDuty,
  recoverOperatingDutyProposal,
  newOperatingDutyId,
  operatingDutiesKey,
  OperatingDutyInputContract,
  operatingDutyTemplates,
  type OperatingDutyInput,
  type OperatingDutyStaffScope,
  type SupplyParty,
  type SupplyPremise,
} from "@helivanta/api";

type Props = {
  scope: OperatingDutyStaffScope;
  staffVersion: number;
  busy: boolean;
  setBusy: (v: boolean) => void;
  onClose: () => void;
};
export function SupplierDutyPreparation({ scope, staffVersion, busy, setBusy, onClose }: Props) {
  const field = useId();
  const [linkCursor, setLinkCursor] = useState<string | null>(null);
  const links = useIndependentStaffLinksPage(scope, linkCursor);
  const party = useApiQuery<SupplyParty>(
    ["operating-duty-source", scope.tenantId, scope.subject, scope.employerId],
    `/supply/parties/${scope.employerId}`,
    {
      gcTime: 0,
      staleTime: 0,
      request: {
        headers: {
          "X-Helivanta-Expected-Tenant": scope.tenantId,
          "X-Helivanta-Expected-Subject": scope.subject,
        },
        cache: "no-store",
      },
    },
  );
  const premises = useApiPagedQuery<SupplyPremise>(
    ["operating-duty-premises", scope.tenantId, scope.subject, scope.employerId],
    `/supply/parties/${scope.employerId}/premises`,
    {
      request: {
        headers: {
          "X-Helivanta-Expected-Tenant": scope.tenantId,
          "X-Helivanta-Expected-Subject": scope.subject,
        },
        cache: "no-store",
      },
    },
  );
  const [account, setAccount] = useState("");
  const [location, setLocation] = useState("");
  const [start, setStart] = useState("");
  const [end, setEnd] = useState("");
  const [reason, setReason] = useState("");
  const [pending, setPending] = useState<{ id: string; input: OperatingDutyInput } | null>(null);
  const [notice, setNotice] = useState("");
  const [validation, setValidation] = useState("");
  const confirmLeave = useConfirmLeave();
  useLeaveGuard(!!pending || !!reason || !!account || !!location, "Receiving duty", !!pending);
  const success = () => {
    setPending(null);
    setBusy(false);
    setNotice("Receiving duty proposed. A separate reviewer must approve it before activation.");
    setReason("");
  };
  const save = useApiMutation(
    (v: NonNullable<typeof pending>) => prepareOperatingDuty(scope, v.id, v.input),
    {
      onSuccess: success,
      onError: (error) => {
        if (error instanceof ApiError && [400, 401, 403, 404, 409, 422].includes(error.status)) {
          setPending(null);
          setBusy(false);
        }
      },
      invalidate: [operatingDutiesKey(scope)],
      suppressErrorToast: true,
    },
  );
  const recover = useApiMutation(
    (v: NonNullable<typeof pending>) => recoverOperatingDutyProposal(scope, v.id, v.input),
    { onSuccess: success, invalidate: [operatingDutiesKey(scope)], suppressErrorToast: true },
  );
  const frozen = busy || !!pending;
  function submit() {
    const selected = links.data?.data.find(
      (r) =>
        r.link.id === account && r.link.status === "active" && r.staff_version === staffVersion,
    );
    const premise = premises.items.find(
      (r) =>
        r.id === location &&
        r.party_id === scope.employerId &&
        r.status === "active" &&
        ["warehouse", "depot"].includes(r.kind) &&
        r.country_code === "IN",
    );
    if (
      !selected ||
      !premise ||
      party.data?.id !== scope.employerId ||
      party.data.record_kind !== "owned_organisation"
    ) {
      setValidation("Choose a current reviewed account and an active owned warehouse or depot.");
      return;
    }
    const result = OperatingDutyInputContract.safeParse({
      scope: {
        employer_kind: "supplier",
        employer_id: scope.employerId,
        location_id: premise.id,
        expected_employer_version: party.data.version,
        expected_location_version: premise.version,
      },
      identity: {
        staff_id: scope.staffId,
        staff_version: staffVersion,
        link_id: selected.link.id,
        link_version: selected.link.version,
      },
      subject: selected.link.subject,
      template_key: operatingDutyTemplates.find((t) => t.employer_kind === "supplier")?.key,
      reason,
      valid_from: start ? `${start}:00Z` : "",
      expires_at: end ? `${end}:00Z` : "",
    });
    if (
      !result.success ||
      Date.parse(result.data.expires_at) <= Date.parse(result.data.valid_from)
    ) {
      setValidation("Enter a reason and a valid start and later expiry in UTC.");
      return;
    }
    const attempt = { id: newOperatingDutyId(), input: result.data };
    setPending(attempt);
    setBusy(true);
    setValidation("");
    setNotice("");
    save.mutate(attempt);
  }
  const accessError = recover.error ?? save.error;
  if (accessError instanceof ApiError && [401, 403, 404].includes(accessError.status))
    return (
      <RequestFailure
        message="Duty preparation is unavailable."
        error={accessError}
        retry={() => {
          if (pending) recover.mutate(pending);
          else onClose();
        }}
      />
    );
  if (!pending && (party.isError || premises.isError || links.isError))
    return (
      <div>
        <RequestFailure
          message="Current supplier locations or reviewed accounts could not be verified."
          error={party.error ?? premises.error ?? links.error}
          retry={() => {
            void party.refetch();
            void premises.retry();
            void links.refetch();
          }}
        />
        <Button variant="ghost" onClick={onClose}>
          Close duty preparation
        </Button>
      </div>
    );
  return (
    <Panel aria-label="Prepare receiving duty" className="space-y-4">
      <h4 className="font-medium">Prepare warehouse receiving duty</h4>
      <p className="text-sm text-muted-foreground">
        Receiving records remain quarantined. This duty does not authorize quality release,
        dispatch, dispensing or payments.
      </p>
      {notice && <p role="status">{notice}</p>}
      {!pending && (
        <>
          <Field id={`${field}-account`} label="Reviewed account">
            <SelectControl
              id={`${field}-account`}
              disabled={frozen}
              value={account}
              onValueChange={setAccount}
              options={(links.data?.data ?? [])
                .filter((r) => r.link.status === "active" && r.staff_version === staffVersion)
                .map((r) => ({ value: r.link.id, label: r.link.subject }))}
            />
          </Field>
          {links.data?.page.has_more && (
            <Button
              variant="outline"
              disabled={frozen}
              onClick={() => {
                setAccount("");
                setLinkCursor(links.data.page.next_cursor);
              }}
            >
              More reviewed accounts
            </Button>
          )}
          {linkCursor && (
            <Button
              variant="outline"
              disabled={frozen}
              onClick={() => {
                setAccount("");
                setLinkCursor(null);
              }}
            >
              First accounts page
            </Button>
          )}
          <Field id={`${field}-location`} label="Warehouse or depot">
            <SelectControl
              id={`${field}-location`}
              disabled={frozen}
              value={location}
              onValueChange={setLocation}
              options={premises.items
                .filter(
                  (r) =>
                    r.status === "active" &&
                    ["warehouse", "depot"].includes(r.kind) &&
                    r.country_code === "IN",
                )
                .map((r) => ({ value: r.id, label: `${r.code} — ${r.name}` }))}
            />
          </Field>
          {premises.hasMore && (
            <Button
              variant="outline"
              disabled={frozen || premises.isFetchingMore}
              onClick={premises.loadMore}
            >
              More locations
            </Button>
          )}
          <Field id={`${field}-start`} label="Valid from (UTC)">
            <Input
              type="datetime-local"
              id={`${field}-start`}
              value={start}
              onChange={(e) => setStart(e.target.value)}
              disabled={frozen}
            />
          </Field>
          <Field id={`${field}-end`} label="Expires at (UTC)">
            <Input
              type="datetime-local"
              id={`${field}-end`}
              value={end}
              onChange={(e) => setEnd(e.target.value)}
              disabled={frozen}
            />
          </Field>
          <Field id={`${field}-reason`} label="Duty reason">
            <Input
              id={`${field}-reason`}
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              disabled={frozen}
            />
          </Field>
          <Button
            disabled={frozen || party.isPending || premises.isPending || links.isPending}
            onClick={submit}
          >
            Propose receiving duty
          </Button>
        </>
      )}
      {pending && (
        <>
          <p role="status">
            The proposal result is unconfirmed. Recover this exact proposal before continuing.
          </p>
          <Button
            disabled={save.isPending || recover.isPending}
            onClick={() => recover.mutate(pending)}
          >
            Recover duty proposal
          </Button>
        </>
      )}
      {(validation || save.error || recover.error) && (
        <p role="alert">{validation || (recover.error ?? save.error)?.message}</p>
      )}
      <Button variant="ghost" disabled={!!pending} onClick={() => confirmLeave(onClose)}>
        Close duty preparation
      </Button>
    </Panel>
  );
}
