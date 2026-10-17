"use client";
import { useEffect, useRef, useState } from "react";
import { Button } from "@tesserix/web";
import {
  apiFetch,
  blankDeclarationClaim,
  blankLandlord,
  blankLender,
  formatPaise,
  statutoryDecisionFormSchema,
  taxDeclarationFormOf,
  taxDeclarationFormSchema,
  taxDeclarationPath,
  taxYearsAround,
  toTaxDeclarationInput,
  useApiMutation,
  useApiQuery,
  usePermissions,
  ApiError,
  DECLARATION_PAN_ACTIONS,
  DECLARATION_ROLES,
  MAX_DECLARATION_CLAIMS,
  MAX_DECLARATION_PARTIES,
  STATUTORY_STATUSES,
  type Hospital,
  type Staff,
  type StatutoryDecisionForm,
  type StatutoryDecisionInput,
  type StatutoryWithdrawInput,
  type TaxDeclaration,
  type TaxDeclarationForm,
  type TaxDeclarationInput,
  type TaxDeclarationParty,
  type TaxDeclarationRevealInput,
  type TaxDeclarationRevision,
} from "@helivanta/api";
import { Field, SelectControl, useFieldArray, useZodForm, type UseFormReturn } from "@helivanta/ui";
import { Failure, inputClass, today } from "../roster-policy/policy-ui";
import { SaveError } from "../leave-balances/years-panel";
import { Check, Fact, RevisionHistory } from "./payroll-workspace";
import { TableError } from "./statutory-slabs";
import { Reveal } from "./staff-identifiers";

type Form = UseFormReturn<TaxDeclarationForm>;
type Role = TaxDeclarationParty["role"];

const declarationKey = (hospital: string, staff: string, year: string) => [
  "workforce",
  "tax-declarations",
  hospital,
  staff,
  year,
];

const claimsOf = (r: TaxDeclarationRevision) =>
  r.claims.length
    ? r.claims.map((c) => `${c.code} ${formatPaise(c.amount_paise)}`).join(" · ")
    : "None";

export function TaxDeclarations({
  hospital,
  base,
  member,
  writable,
}: {
  hospital: Hospital;
  base: string;
  member: Staff;
  writable: boolean;
}) {
  const { can, subject } = usePermissions();
  const manager = hospital.access_level === "manager";
  const approver = manager && can("workforce.payroll.approve");
  const revealer = manager && can("workforce.payroll.identifiers.reveal");
  const years = taxYearsAround(today());
  const [year, setYear] = useState(years[1]!);
  const path = taxDeclarationPath(base, member.id, year);
  const key = declarationKey(hospital.id, member.id, year);
  const record = useApiQuery<TaxDeclaration>(key, path);
  const [view, setView] = useState<"propose" | "decide" | "history" | null>(null);
  const opener = useRef<HTMLButtonElement | null>(null);
  const open = (next: typeof view, ev: React.MouseEvent<HTMLButtonElement>) => {
    opener.current = ev.currentTarget;
    setView(next);
  };
  const close = () => {
    setView(null);
    opener.current?.focus();
  };
  const missing = record.error instanceof ApiError && record.error.status === 404;
  const data = missing ? undefined : record.data;
  const pending = data?.pending;
  const mine = pending?.proposed_by_subject === subject;
  const active = member.employment_status === "active";
  return (
    <div role="region" aria-label="Tax declarations" className="border-t p-5">
      <div className="mb-4 flex flex-wrap items-end justify-between gap-4">
        <div>
          <h4 className="font-semibold">Tax declarations</h4>
          <p className="mt-1 max-w-2xl text-xs text-muted-foreground">
            The claims, rent and home loan interest the employee declared (Form 124). A second
            person approves each change after checking the evidence; landlord and lender PANs are
            stored encrypted.
          </p>
        </div>
        <div className="flex flex-wrap items-end gap-2">
          <Field id="tax-declaration-year" label="Tax year">
            <SelectControl
              id="tax-declaration-year"
              value={year}
              disabled={view !== null}
              onValueChange={setYear}
              options={years.map((y) => ({ value: y, label: y }))}
            />
          </Field>
          {data && (
            <Button variant="outline" onClick={(ev) => open("history", ev)}>
              Declaration history
            </Button>
          )}
          {pending && ((approver && !mine) || (writable && mine)) && (
            <Button variant="outline" onClick={(ev) => open("decide", ev)}>
              {mine ? "Withdraw declaration" : "Review declaration"}
            </Button>
          )}
          {writable && active && !pending && !record.isPending && !(record.isError && !missing) && (
            <Button variant="outline" onClick={(ev) => open("propose", ev)}>
              {data?.current ? "Propose declaration change" : "Propose declaration"}
            </Button>
          )}
        </div>
      </div>
      {record.isPending ? (
        <p role="status">Loading declarations…</p>
      ) : record.isError && !missing ? (
        <Failure
          message="Could not load this staff member's tax declarations."
          retry={() => record.refetch()}
        />
      ) : (
        <>
          {data?.current ? (
            <Declaration path={path} revision={data.current} revealer={revealer} />
          ) : (
            <p className="text-sm text-muted-foreground">
              No declaration approved for {member.display_name} in {year}.
            </p>
          )}
          {pending && (
            <p className="mt-3 text-xs">
              <span className="rounded-full bg-amber-100 px-2 py-0.5 font-medium text-amber-900">
                {STATUTORY_STATUSES.pending}
              </span>{" "}
              Change proposed by {mine ? "you" : pending.proposed_by_subject} · {pending.reason}
            </p>
          )}
        </>
      )}
      {view === "propose" && (
        <DeclarationEditor key={year} path={path} queryKey={key} record={data} onClose={close} />
      )}
      {view === "decide" && data && pending && (
        <DeclarationDecision
          key={pending.id}
          path={path}
          queryKey={key}
          record={data}
          proposal={pending}
          mine={mine}
          revealer={revealer}
          onClose={close}
        />
      )}
      {view === "history" && (
        <RevisionHistory<TaxDeclarationRevision>
          label={`${year} declaration history for ${member.display_name}`}
          queryKey={[...key, "history"]}
          path={`${path}/history`}
          rowKey={(r) => r.id}
          render={(r) => (
            <>
              <p className="font-medium">
                {STATUTORY_STATUSES[r.status]}
                {r.approved_version && ` · v${r.approved_version}`}
              </p>
              <p className="mt-1">{claimsOf(r)}</p>
              {r.parties.map((p) => (
                <p key={`${p.role}-${p.position}`} className="mt-1">
                  {DECLARATION_ROLES[p.role]} {p.position}: {p.name} · PAN{" "}
                  {p.pan_masked ?? "not given"}
                </p>
              ))}
              <p className="mt-1 text-muted-foreground">
                {r.reason} · proposed by {r.proposed_by_subject}
                {r.decided_by_subject && ` · decided by ${r.decided_by_subject}`}
                {r.decision_note && ` · “${r.decision_note}”`}
              </p>
            </>
          )}
          onClose={close}
        />
      )}
    </div>
  );
}

function Declaration({
  path,
  revision,
  revealer,
}: {
  path: string;
  revision: TaxDeclarationRevision;
  revealer: boolean;
}) {
  return (
    <>
      <dl className="grid gap-4 text-sm sm:grid-cols-3">
        <Fact label="Claims" value={claimsOf(revision)} />
        <Fact
          label="House rent"
          value={
            revision.hra_metro === undefined
              ? "Not claimed"
              : revision.hra_metro
                ? "Claimed · metro city"
                : "Claimed · other city"
          }
        />
        <Fact
          label="Home loan interest"
          value={
            revision.home_loan_interest_paise === undefined
              ? "Not claimed"
              : formatPaise(revision.home_loan_interest_paise)
          }
        />
      </dl>
      {revision.parties.length > 0 && (
        <ul aria-label="Landlords and lenders" className="mt-4 grid gap-3 text-sm sm:grid-cols-2">
          {revision.parties.map((p) => (
            <PartyFact
              key={`${p.role}-${p.position}`}
              path={path}
              revision={revision}
              party={p}
              revealer={revealer}
            />
          ))}
        </ul>
      )}
      <p className="mt-2 text-xs text-muted-foreground">
        {revision.approved_version ? `v${revision.approved_version} · ` : ""}
        {revision.reason}
      </p>
    </>
  );
}

function PartyFact({
  path,
  revision,
  party,
  revealer,
}: {
  path: string;
  revision: TaxDeclarationRevision;
  party: TaxDeclarationParty;
  revealer: boolean;
}) {
  const title = `${DECLARATION_ROLES[party.role]} ${party.position}`;
  return (
    <li className="rounded-lg border p-3">
      <p className="text-xs text-muted-foreground">{title}</p>
      <p className="mt-1 font-medium">{party.name}</p>
      <p className="text-muted-foreground">{party.address}</p>
      {party.rent_paise !== undefined && <p>Rent paid {formatPaise(party.rent_paise)}</p>}
      <p>PAN {party.pan_masked ?? "not given"}</p>
      {revealer && party.pan_masked && (
        <Reveal
          path={path}
          id={`reveal-${revision.id}-${party.role}-${party.position}`}
          label={`${title} PAN`}
          input={
            {
              revision_id: revision.id,
              role: party.role,
              position: party.position,
            } satisfies Omit<TaxDeclarationRevealInput, "reason">
          }
        />
      )}
    </li>
  );
}

function Text({
  form,
  name,
  label,
  error,
  decimal = false,
}: {
  form: Form;
  name: string;
  label: string;
  error: string | undefined;
  decimal?: boolean;
}) {
  const id = `tax-declaration-${name.replaceAll(".", "-")}`;
  return (
    <Field id={id} label={label} error={error}>
      <input
        id={id}
        className={inputClass}
        inputMode={decimal ? "decimal" : undefined}
        spellCheck={false}
        aria-invalid={!!error}
        {...form.register(name as never)}
      />
    </Field>
  );
}

const panOptions = (present: boolean) =>
  (Object.keys(DECLARATION_PAN_ACTIONS) as (keyof typeof DECLARATION_PAN_ACTIONS)[])
    .filter((a) => present || a !== "keep")
    .map((a) => ({ value: a, label: DECLARATION_PAN_ACTIONS[a] }));

function Parties({
  form,
  kind,
  inForce,
}: {
  form: Form;
  kind: Role;
  inForce: TaxDeclarationParty[];
}) {
  const name = kind === "landlord" ? "landlords" : "lenders";
  const rows = useFieldArray({ control: form.control, name, keyName: "fieldKey" });
  const value = form.watch(name);
  const errors = form.formState.errors[name];
  const label = DECLARATION_ROLES[kind];
  return (
    <>
      {rows.fields.map((row, index) => {
        const rowErrors = errors?.[index];
        const present = inForce.some(
          (p) => p.role === kind && p.position === index + 1 && p.pan_masked,
        );
        const id = `tax-declaration-${name}-${index}-pan`;
        return (
          <fieldset key={row.fieldKey} className="space-y-3 rounded-lg border p-3">
            <legend className="px-1 text-sm font-medium">
              {label} {index + 1}
            </legend>
            <div className="grid items-end gap-4 sm:grid-cols-2">
              <Text
                form={form}
                name={`${name}.${index}.name`}
                label="Name"
                error={rowErrors?.name?.message}
              />
              <Text
                form={form}
                name={`${name}.${index}.address`}
                label="Address"
                error={rowErrors?.address?.message}
              />
              {kind === "landlord" && (
                <Text
                  form={form}
                  name={`landlords.${index}.rent`}
                  label="Rent paid this year (₹)"
                  decimal
                  error={form.formState.errors.landlords?.[index]?.rent?.message}
                />
              )}
              <Field id={id} label="PAN">
                <SelectControl
                  id={id}
                  value={value[index]?.pan ?? "none"}
                  onValueChange={(v) =>
                    form.setValue(`${name}.${index}.pan`, v as "keep" | "set" | "none", {
                      shouldValidate: form.formState.isSubmitted,
                    })
                  }
                  options={panOptions(present)}
                />
              </Field>
              {value[index]?.pan === "set" && (
                <Text
                  form={form}
                  name={`${name}.${index}.pan_value`}
                  label={`${label} ${index + 1} PAN`}
                  error={rowErrors?.pan_value?.message}
                />
              )}
            </div>
            <Button
              type="button"
              variant="ghost"
              aria-label={`Remove ${label.toLowerCase()} ${index + 1}`}
              disabled={rows.fields.length === 1}
              onClick={() => rows.remove(index)}
            >
              Remove
            </Button>
          </fieldset>
        );
      })}
      <TableError message={errors?.root?.message ?? errors?.message} />
      <Button
        type="button"
        variant="outline"
        disabled={rows.fields.length >= MAX_DECLARATION_PARTIES}
        onClick={() => rows.append(kind === "landlord" ? blankLandlord() : blankLender())}
      >
        Add {label.toLowerCase()}
      </Button>
    </>
  );
}

function DeclarationEditor({
  path,
  queryKey,
  record,
  onClose,
}: {
  path: string;
  queryKey: unknown[];
  record?: TaxDeclaration;
  onClose: () => void;
}) {
  const current = record?.current;
  const form = useZodForm(taxDeclarationFormSchema, taxDeclarationFormOf(current));
  const claims = useFieldArray({ control: form.control, name: "claims", keyName: "fieldKey" });
  const save = useApiMutation(
    (input: TaxDeclarationInput) =>
      apiFetch<TaxDeclaration>(path, { method: "PUT", body: JSON.stringify(input) }),
    {
      invalidate: [queryKey],
      successToast: "Declaration proposed",
      suppressErrorToast: true,
      onSuccess: onClose,
    },
  );
  useEffect(() => form.setFocus("reason"), [form]);
  const errors = form.formState.errors;
  const hra = form.watch("hra");
  const homeLoan = form.watch("home_loan");
  const metro = form.watch("metro");
  const set = (name: "hra" | "home_loan" | "metro", next: boolean) =>
    form.setValue(name, next, { shouldValidate: form.formState.isSubmitted });
  const inForce = current?.parties ?? [];
  return (
    <form
      noValidate
      autoComplete="off"
      aria-label={current ? "Propose declaration change" : "Propose declaration"}
      onSubmit={form.handleSubmit((values) =>
        save.mutate(toTaxDeclarationInput(values, record?.version ?? 0)),
      )}
      className="mt-4 space-y-6 border-t bg-muted/10 pt-5"
    >
      <fieldset disabled={save.isPending} className="space-y-6">
        <fieldset className="space-y-4 rounded-xl border p-4">
          <legend className="px-1 text-sm font-semibold">Claims</legend>
          {claims.fields.map((claim, index) => (
            <div key={claim.fieldKey} className="grid items-end gap-4 sm:grid-cols-[1fr_1fr_auto]">
              <Text
                form={form}
                name={`claims.${index}.code`}
                label={`Claim ${index + 1} code`}
                error={errors.claims?.[index]?.code?.message}
              />
              <Text
                form={form}
                name={`claims.${index}.amount`}
                label={`Claim ${index + 1} amount (₹)`}
                decimal
                error={errors.claims?.[index]?.amount?.message}
              />
              <Button
                type="button"
                variant="ghost"
                aria-label={`Remove claim ${index + 1}`}
                onClick={() => claims.remove(index)}
              >
                Remove
              </Button>
            </div>
          ))}
          <TableError message={errors.claims?.root?.message ?? errors.claims?.message} />
          <Button
            type="button"
            variant="outline"
            disabled={claims.fields.length >= MAX_DECLARATION_CLAIMS}
            onClick={() => claims.append(blankDeclarationClaim())}
          >
            Add claim
          </Button>
        </fieldset>
        <fieldset className="space-y-4 rounded-xl border p-4">
          <legend className="px-1 text-sm font-semibold">House rent</legend>
          <Check label="Claim house rent allowance" checked={hra} onChange={(v) => set("hra", v)} />
          {hra && (
            <>
              <Check
                label="Rented home is in a metro city"
                checked={metro}
                onChange={(v) => set("metro", v)}
              />
              <Parties form={form} kind="landlord" inForce={inForce} />
            </>
          )}
        </fieldset>
        <fieldset className="space-y-4 rounded-xl border p-4">
          <legend className="px-1 text-sm font-semibold">Home loan</legend>
          <Check
            label="Claim home loan interest"
            checked={homeLoan}
            onChange={(v) => set("home_loan", v)}
          />
          {homeLoan && (
            <>
              <Text
                form={form}
                name="interest"
                label="Interest payable this year (₹)"
                decimal
                error={errors.interest?.message}
              />
              <Parties form={form} kind="lender" inForce={inForce} />
            </>
          )}
        </fieldset>
        <Field id="tax-declaration-reason" label="Reason" error={errors.reason?.message}>
          <textarea
            id="tax-declaration-reason"
            rows={2}
            className={inputClass}
            aria-invalid={!!errors.reason}
            {...form.register("reason")}
          />
        </Field>
      </fieldset>
      {save.error && (
        <SaveError
          message={
            save.error instanceof ApiError && save.error.code === "landlord_pan_required"
              ? save.error.message
              : `${save.error.message} Close and reload to see the current declaration.`
          }
        />
      )}
      <div className="flex justify-end gap-2">
        <Button type="button" variant="outline" disabled={save.isPending} onClick={onClose}>
          Cancel
        </Button>
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? "Proposing…" : "Propose declaration"}
        </Button>
      </div>
    </form>
  );
}

function DeclarationDecision({
  path,
  queryKey,
  record,
  proposal,
  mine,
  revealer,
  onClose,
}: {
  path: string;
  queryKey: unknown[];
  record: TaxDeclaration;
  proposal: TaxDeclarationRevision;
  mine: boolean;
  revealer: boolean;
  onClose: () => void;
}) {
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => heading.current?.focus(), []);
  const form = useZodForm(statutoryDecisionFormSchema, {
    decision: mine ? "reject" : "approve",
    note: "",
  });
  const settle = (successToast: string) => ({
    invalidate: [queryKey],
    successToast,
    suppressErrorToast: true,
    onSuccess: onClose,
  });
  const post = (input: StatutoryDecisionInput) =>
    apiFetch<TaxDeclaration>(`${path}/decision`, {
      method: "POST",
      body: JSON.stringify(input),
    });
  const approve = useApiMutation(post, settle("Declaration approved"));
  const reject = useApiMutation(post, settle("Declaration rejected"));
  const withdraw = useApiMutation(
    (input: StatutoryWithdrawInput) =>
      apiFetch<TaxDeclaration>(`${path}/withdraw`, {
        method: "POST",
        body: JSON.stringify(input),
      }),
    settle("Proposal withdrawn"),
  );
  const busy = approve.isPending || reject.isPending || withdraw.isPending;
  const failure = approve.error ?? reject.error ?? withdraw.error;
  const errors = form.formState.errors;
  const submit = (decision: StatutoryDecisionForm["decision"]) =>
    form.handleSubmit((values) =>
      (decision === "approve" ? approve : reject).mutate({
        proposal_id: proposal.id,
        expected_version: record.version,
        decision,
        note: values.note.trim(),
      }),
    );
  return (
    <div role="region" aria-label="Declaration decision" className="mt-4 border-t pt-5">
      <div className="mb-4 flex items-center justify-between gap-4">
        <h4 ref={heading} tabIndex={-1} className="font-semibold outline-none">
          {mine ? "Withdraw your declaration proposal" : "Review declaration proposal"}
        </h4>
        <Button variant="outline" onClick={onClose}>
          Close
        </Button>
      </div>
      {/* Only someone other than the proposer may check proposed PANs against the evidence. */}
      <Declaration path={path} revision={proposal} revealer={revealer && !mine} />
      <dl className="my-4 grid gap-4 text-sm sm:grid-cols-3">
        <Fact
          label="In force now"
          value={
            record.current
              ? `v${record.version} · ${claimsOf(record.current)}`
              : "Nothing approved yet"
          }
        />
        <Fact label="Proposed by" value={mine ? "You" : proposal.proposed_by_subject} />
      </dl>
      <form
        noValidate
        aria-label={mine ? "Withdraw declaration proposal" : "Decide declaration proposal"}
        onSubmit={
          mine
            ? (ev) => {
                ev.preventDefault();
                withdraw.mutate({ proposal_id: proposal.id, note: form.getValues("note").trim() });
              }
            : submit("approve")
        }
        className="space-y-4"
      >
        <Field
          id="tax-declaration-decision-note"
          label={
            mine
              ? "Reason for withdrawing (optional)"
              : "Evidence checked, or why you reject (required to reject)"
          }
          error={errors.note?.message}
        >
          <textarea
            id="tax-declaration-decision-note"
            rows={2}
            className={inputClass}
            aria-invalid={!!errors.note}
            disabled={busy}
            {...form.register("note")}
          />
        </Field>
        {failure && (
          <SaveError
            message={
              failure instanceof ApiError && failure.code === "landlord_pan_required"
                ? `${failure.message} Reject the proposal so a PAN can be added.`
                : `${failure.message} Close and reload to see the current declaration.`
            }
          />
        )}
        <div className="flex justify-end gap-2">
          {mine ? (
            <Button type="submit" disabled={busy}>
              {withdraw.isPending ? "Withdrawing…" : "Withdraw proposal"}
            </Button>
          ) : (
            <>
              <Button
                type="button"
                variant="outline"
                disabled={busy}
                onClick={(ev) => {
                  form.setValue("decision", "reject");
                  void submit("reject")(ev);
                }}
              >
                {reject.isPending ? "Rejecting…" : "Reject declaration"}
              </Button>
              <Button
                type="submit"
                disabled={busy}
                onClick={() => form.setValue("decision", "approve")}
              >
                {approve.isPending ? "Approving…" : "Approve declaration"}
              </Button>
            </>
          )}
        </div>
      </form>
    </div>
  );
}
