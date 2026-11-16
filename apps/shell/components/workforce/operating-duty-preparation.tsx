"use client";
import { useId, useState } from "react";
import { Button, Input } from "@tesserix/web";
import {
  Panel,
  Field,
  firstError,
  RequestFailure,
  RefreshButton,
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
  useOperatingLocationChoices,
  type OperatingGovernanceStaffChoice,
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
  type PharmacyOrganization,
  type PharmacyOutlet,
  enrollmentRequestOptions,
} from "@helivanta/api";

type Props = {
  scope: OperatingDutyStaffScope;
  staffVersion: number;
  busy: boolean;
  setBusy: (v: boolean) => void;
  onClose: () => void;
  reviewedAccount?: OperatingGovernanceStaffChoice;
  accessSuspended?: boolean;
};
type Location = { id: string; label: string; version: number; employerVersion?: number };
type Accounts = {
  rows: Array<{
    id: string;
    subject: string;
    version: number;
    staffVersion: number;
    label: string;
  }>;
  fixed?: boolean;
  isPending: boolean;
  isError: boolean;
  error: unknown;
  refresh: () => void;
  more?: () => void;
  first?: () => void;
};
type Source = {
  employerVersion?: number;
  locations: Location[];
  isPending: boolean;
  isError: boolean;
  error: unknown;
  refresh: () => void;
  more?: () => void;
  first?: () => void;
  isFetchingMore: boolean;
};
export function OperatingDutyPreparation(props: Props) {
  if (props.scope.administration === "governance") {
    const account = props.reviewedAccount;
    if (
      !account ||
      account.staff_id !== props.scope.staffId ||
      account.staff_version !== props.staffVersion
    )
      return (
        <Panel>
          <p role="alert">Refresh and select a current independently reviewed Staff account.</p>
          <Button variant="ghost" onClick={props.onClose}>
            Close duty preparation
          </Button>
        </Panel>
      );
    return <NativeSource {...props} reviewedAccount={account} />;
  }

  return props.scope.employerKind === "supplier" ? (
    <SupplierSource {...props} />
  ) : (
    <PharmacySource {...props} />
  );
}
function SupplierSource(props: Props) {
  const { scope } = props;
  const request = enrollmentRequestOptions(scope);
  const party = useApiQuery<SupplyParty>(
    ["operating-duty-source", scope.tenantId, scope.subject, scope.employerId],
    `/supply/parties/${scope.employerId}`,
    { gcTime: 0, staleTime: 0, request },
  );
  const premises = useApiPagedQuery<SupplyPremise>(
    ["operating-duty-premises", scope.tenantId, scope.subject, scope.employerId],
    `/supply/parties/${scope.employerId}/premises`,
    { gcTime: 0, request },
  );
  const owned =
    party.data?.id === scope.employerId && party.data.record_kind === "owned_organisation";
  return (
    <LegacyAccounts
      {...props}
      source={{
        employerVersion: owned ? party.data?.version : undefined,
        locations: premises.items
          .filter(
            (r) =>
              r.party_id === scope.employerId &&
              r.status === "active" &&
              ["warehouse", "depot"].includes(r.kind) &&
              r.country_code === "IN",
          )
          .map((r) => ({ id: r.id, label: `${r.code} — ${r.name}`, version: r.version })),
        isPending: party.isPending || premises.isPending,
        isError: party.isError || premises.isError,
        error: firstError(party.error, premises.error),
        refresh: () => {
          void party.refetch();
          premises.retry();
        },
        more: premises.hasMore ? premises.loadMore : undefined,
        isFetchingMore: premises.isFetchingMore,
      }}
    />
  );
}
function PharmacySource(props: Props) {
  const { scope } = props;
  const request = enrollmentRequestOptions(scope);
  const pharmacies = useApiPagedQuery<PharmacyOrganization>(
    ["operating-duty-pharmacies", scope.tenantId, scope.subject, scope.employerId],
    "/pharmacy/pharmacies",
    { gcTime: 0, request },
  );
  const outlets = useApiPagedQuery<PharmacyOutlet>(
    ["operating-duty-outlets", scope.tenantId, scope.subject, scope.employerId],
    `/pharmacy/pharmacies/${scope.employerId}/outlets`,
    { gcTime: 0, request },
  );
  const owned = pharmacies.items.some(
    (r) => r.id === scope.employerId && ["independent", "chain"].includes(r.ownership_type),
  );
  return (
    <>
      <LegacyAccounts
        {...props}
        source={{
          // Pharma's structural identities are immutable version 1; legal eligibility is a separate owner check.
          employerVersion: owned ? 1 : undefined,
          locations: outlets.items
            .filter((r) => r.pharmacy_id === scope.employerId && r.country_code === "IN")
            .map((r) => ({ id: r.id, label: `${r.code} — ${r.name}`, version: 1 })),
          isPending: pharmacies.isPending || outlets.isPending,
          isError: pharmacies.isError || outlets.isError,
          error: firstError(pharmacies.error, outlets.error),
          refresh: () => {
            pharmacies.retry();
            outlets.retry();
          },
          more: outlets.hasMore ? outlets.loadMore : undefined,
          isFetchingMore: outlets.isFetchingMore,
        }}
      />
      {pharmacies.hasMore && (
        <Button
          variant="outline"
          disabled={props.busy || pharmacies.isFetchingMore}
          onClick={pharmacies.loadMore}
        >
          More pharmacy organizations
        </Button>
      )}
    </>
  );
}
function LegacyAccounts(props: Props & { source: Source }) {
  const [cursor, setCursor] = useState<string | null>(null);
  const links = useIndependentStaffLinksPage(props.scope, cursor);
  return (
    <DutyForm
      {...props}
      accounts={{
        rows: (links.data?.data ?? [])
          .filter((row) => row.link.status === "active")
          .map((row) => ({
            id: row.link.id,
            subject: row.link.subject,
            version: row.link.version,
            staffVersion: row.staff_version,
            label: row.link.subject,
          })),
        isPending: links.isPending,
        isError: links.isError,
        error: links.error,
        refresh: () => {
          void links.refetch();
        },
        more: links.data?.page.has_more ? () => setCursor(links.data.page.next_cursor) : undefined,
        first: cursor ? () => setCursor(null) : undefined,
      }}
    />
  );
}
function NativeSource(props: Props & { reviewedAccount: OperatingGovernanceStaffChoice }) {
  const [cursor, setCursor] = useState<string | null>(null);
  const locations = useOperatingLocationChoices(
    props.scope,
    cursor,
    !props.busy && !props.accessSuspended,
  );
  const account = props.reviewedAccount;
  return (
    <DutyForm
      {...props}
      accounts={{
        rows: [
          {
            id: account.link_id,
            subject: account.subject,
            version: account.link_version,
            staffVersion: account.staff_version,
            label: account.display_name,
          },
        ],
        fixed: true,
        isPending: false,
        isError: !!props.accessSuspended,
        error: null,
        refresh: () => {},
      }}
      source={{
        locations: (locations.data?.data ?? []).map((row) => ({
          id: row.location_id,
          label: `${row.code} — ${row.name}`,
          version: row.location_version,
          employerVersion: row.employer_version,
        })),
        employerVersion: locations.data?.data[0]?.employer_version,
        isPending: locations.isPending,
        isError: locations.isError,
        error: locations.error,
        refresh: () => {
          void locations.refetch();
        },
        more: locations.data?.page.has_more
          ? () => setCursor(locations.data.page.next_cursor)
          : undefined,
        first: cursor ? () => setCursor(null) : undefined,
        isFetchingMore: locations.isFetching,
      }}
    />
  );
}

function DutyForm({
  scope,
  staffVersion,
  busy,
  setBusy,
  onClose,
  source,
  accounts,
  accessSuspended,
}: Props & { source: Source; accounts: Accounts }) {
  const field = useId();

  const templates = operatingDutyTemplates.filter((t) => t.employer_kind === scope.employerKind);
  const [template, setTemplate] = useState(templates[0]?.key ?? "");
  const fixedAccount = accounts.fixed ? (accounts.rows[0]?.id ?? "") : "";
  const [account, setAccount] = useState(fixedAccount);
  const [location, setLocation] = useState("");
  const [start, setStart] = useState("");
  const [end, setEnd] = useState("");
  const [reason, setReason] = useState("");
  const [pending, setPending] = useState<{ id: string; input: OperatingDutyInput } | null>(null);
  const [notice, setNotice] = useState("");
  const [validation, setValidation] = useState("");
  const confirmLeave = useConfirmLeave();
  useLeaveGuard(
    !!pending ||
      !!reason ||
      account !== fixedAccount ||
      !!location ||
      !!start ||
      !!end ||
      template !== templates[0]?.key,
    "Operating duty",
    !!pending,
  );
  const success = () => {
    setPending(null);
    setBusy(false);
    setNotice("Operating duty proposed. A separate reviewer must approve it before activation.");
    setReason("");
    setAccount(fixedAccount);
    setLocation("");
    setStart("");
    setEnd("");
    setTemplate(templates[0]?.key ?? "");
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
    const selected = accounts.rows.find(
      (row) => row.id === account && row.staffVersion === staffVersion,
    );
    const locationRecord = source.locations.find((r) => r.id === location);
    const role = templates.find((t) => t.key === template);
    if (
      !selected ||
      !locationRecord ||
      !source.employerVersion ||
      !role ||
      source.isPending ||
      source.isError ||
      accounts.isError ||
      accessSuspended
    ) {
      setValidation("Choose a current reviewed account, operating role and owned source location.");
      return;
    }
    const result = OperatingDutyInputContract.safeParse({
      scope: {
        employer_kind: scope.employerKind,
        employer_id: scope.employerId,
        location_id: locationRecord.id,
        expected_employer_version: locationRecord.employerVersion ?? source.employerVersion,
        expected_location_version: locationRecord.version,
      },
      identity: {
        staff_id: scope.staffId,
        staff_version: staffVersion,
        link_id: selected.id,
        link_version: selected.version,
      },
      subject: selected.subject,
      template_key: role.key,
      reason,
      valid_from: start ? `${start}:00Z` : "",
      expires_at: end ? `${end}:00Z` : "",
    });
    if (
      !result.success ||
      Date.parse(result.data.expires_at) <= Date.parse(result.data.valid_from) ||
      Date.parse(result.data.expires_at) - Date.parse(result.data.valid_from) >
        role.max_validity_hours * 3_600_000
    ) {
      setValidation(
        "Enter a reason and a valid start and later expiry within the role’s maximum validity in UTC.",
      );
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
  if (
    accessSuspended ||
    (accessError instanceof ApiError && [401, 403, 404].includes(accessError.status))
  )
    return (
      <RequestFailure
        message="Duty preparation is unavailable."
        recheck={pending ? "Recover duty proposal" : undefined}
        busy={save.isPending || recover.isPending}
        error={accessError}
        retry={() => {
          if (pending) recover.mutate(pending);
          else onClose();
        }}
      />
    );
  if (!pending && (source.isError || accounts.isError))
    return (
      <div>
        <RequestFailure
          message="Current source locations or reviewed accounts could not be verified."
          error={firstError(source.error, accounts.error)}
          retry={() => {
            source.refresh();
            void accounts.refresh();
          }}
        />
        <Button variant="ghost" onClick={onClose}>
          Close duty preparation
        </Button>
      </div>
    );
  return (
    <Panel aria-label="Prepare operating duty" className="space-y-4">
      <h4 className="font-medium">Prepare operating duty</h4>
      <p className="text-sm text-muted-foreground">
        Choose one role for this exact source location. An independently approved account and
        separate duty approval are required. Trading, legal eligibility and clinical authority
        remain separate.
      </p>
      <RefreshButton
        busy={frozen}
        refresh={() => {
          source.refresh();
          void accounts.refresh();
        }}
        error={firstError(source.error, accounts.error)}
      >
        Refresh preparation sources
      </RefreshButton>
      {notice && <p role="status">{notice}</p>}
      {!pending && (
        <>
          <Field id={`${field}-role`} label="Operating role">
            <SelectControl
              id={`${field}-role`}
              disabled={frozen}
              value={template}
              onValueChange={setTemplate}
              options={templates.map((t) => ({ value: t.key, label: t.label }))}
            />
          </Field>
          {!accounts.isPending &&
            accounts.rows.every((row) => row.staffVersion !== staffVersion) && (
              <p>
                No current reviewed account on this page. Propose and independently approve an
                account link in Reviewed accounts, or continue to another accounts page.
              </p>
            )}
          {accounts.fixed ? (
            <div className="text-sm">
              <p className="font-medium">Reviewed account</p>
              <p>{accounts.rows[0]?.label} · independently reviewed</p>
            </div>
          ) : (
            <Field id={`${field}-account`} label="Reviewed account">
              <SelectControl
                id={`${field}-account`}
                disabled={frozen}
                value={account}
                onValueChange={setAccount}
                options={accounts.rows
                  .filter((row) => row.staffVersion === staffVersion)
                  .map((row) => ({ value: row.id, label: row.label }))}
              />
            </Field>
          )}
          {accounts.more && (
            <Button
              variant="outline"
              disabled={frozen}
              onClick={() => {
                setAccount("");
                accounts.more?.();
              }}
            >
              More reviewed accounts
            </Button>
          )}
          {accounts.first && (
            <Button
              variant="outline"
              disabled={frozen}
              onClick={() => {
                setAccount("");
                accounts.first?.();
              }}
            >
              First accounts page
            </Button>
          )}
          <Field
            id={`${field}-location`}
            label={scope.employerKind === "pharmacy" ? "Pharmacy outlet" : "Warehouse or depot"}
          >
            <SelectControl
              id={`${field}-location`}
              disabled={frozen}
              value={location}
              onValueChange={setLocation}
              options={source.locations.map((r) => ({ value: r.id, label: r.label }))}
            />
          </Field>
          {source.more && (
            <Button
              variant="outline"
              disabled={frozen || source.isFetchingMore}
              onClick={source.more}
            >
              More locations
            </Button>
          )}
          {source.first && (
            <Button
              variant="outline"
              disabled={frozen}
              onClick={() => {
                setLocation("");
                source.first?.();
              }}
            >
              First locations page
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
          <Button disabled={frozen || source.isPending || accounts.isPending} onClick={submit}>
            Propose operating duty
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
