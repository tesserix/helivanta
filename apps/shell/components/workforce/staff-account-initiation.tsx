"use client";

import { useId, useState } from "react";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import { Field, Panel, useZodForm, useLeaveGuard, formatDateTime } from "@helivanta/ui";
import {
  ApiError,
  createStaffAccount,
  readStaffAccount,
  observeStaffAccount,
  deliverStaffAccountInvitation,
  newStaffAccountCommandId,
  staffContactInputContract,
  useApiMutation,
  type StaffContactInput,
  type StaffIdentityReceipt,
  type StaffAssignmentScope,
} from "@helivanta/api";
import { StaffMembershipForm } from "../../app/(workspace)/workforce/access/membership-form";

type Command = { id: string; input: StaffContactInput };
const recoverySchema = z.object({ id: z.uuidv7() });
function refused(error: Error | null) {
  return error instanceof ApiError && [401, 403, 404, 409].includes(error.status);
}
export function StaffAccountInitiation({
  scope,
  onEnrolled,
}: {
  scope: StaffAssignmentScope;
  onEnrolled?: (subject: string) => void;
}) {
  const prefix = useId();
  const form = useZodForm(staffContactInputContract, {
    email: "",
    given_name: "",
    family_name: "",
    reason: "",
  });
  const recovery = useZodForm(recoverySchema, { id: "" });
  const [command, setCommand] = useState<Command | null>(null);
  const [receipt, setReceipt] = useState<StaffIdentityReceipt | null>(null);
  const [delivery, setDelivery] = useState<string | null>(null);
  const [enrolled, setEnrolled] = useState(false);
  const [correcting, setCorrecting] = useState(false);
  const create = useApiMutation(
    (value: Command) => createStaffAccount(scope, value.id, value.input),
    {
      suppressErrorToast: true,
      onSuccess: setReceipt,
    },
  );
  const read = useApiMutation((id: string) => readStaffAccount(scope, id), {
    suppressErrorToast: true,
    onSuccess: (value) => {
      observe.reset();
      setReceipt(value);
    },
  });
  const observe = useApiMutation(
    () => {
      if (!receipt) throw new Error("Recover the account command first.");
      return observeStaffAccount(scope, receipt.command_id);
    },
    { suppressErrorToast: true, onSuccess: (value) => setReceipt(value.receipt) },
  );
  const send = useApiMutation(
    (id: string) => {
      if (!receipt) throw new Error("Recover the account command first.");
      return deliverStaffAccountInvitation(scope, receipt.command_id, id);
    },
    { suppressErrorToast: true },
  );
  const busy = create.isPending || read.isPending || observe.isPending || send.isPending;
  const blocked = [create.error, read.error, observe.error, send.error].some(refused);
  const created =
    receipt?.command_state === "created" &&
    !!receipt.user_id &&
    !receipt.recovery_state &&
    !receipt.successor_command_id;
  const delivered =
    send.isSuccess && !["delivery_unresolved", "proof_unresolved"].includes(send.data.state);
  const ready =
    created &&
    observe.isSuccess &&
    !observe.isPending &&
    !observe.isError &&
    observe.data.receipt.command_id === receipt.command_id &&
    observe.data.observation.state === "active" &&
    observe.data.observation.email_verified;
  useLeaveGuard(
    !!(command || receipt) && !enrolled,
    "staff account setup; keep the command reference to recover",
    busy,
  );
  return (
    <Panel className="my-5 space-y-4">
      <h3 className="text-lg font-semibold">Set up a new staff login</h3>
      <p>
        Create a separate account in this organization. The recipient privately verifies their email
        and sets a password. Membership, reviewed Staff links and operating duties are separate
        steps.
      </p>
      {!receipt && (
        <>
          <form
            aria-label="New staff account"
            noValidate
            className="space-y-4"
            onSubmit={form.handleSubmit((input) => {
              const value =
                command && !correcting
                  ? command
                  : { id: command?.id ?? newStaffAccountCommandId(), input };
              setCorrecting(false);
              setCommand(value);
              create.mutate(value);
            })}
          >
            <div className="grid gap-4 sm:grid-cols-2">
              {(
                [
                  ["given_name", "First name"],
                  ["family_name", "Last name"],
                  ["email", "Work email"],
                  ["reason", "Account creation reason"],
                ] as const
              ).map(([name, label]) => (
                <Field
                  key={name}
                  id={`${prefix}-${name}`}
                  label={label}
                  error={form.formState.errors[name]?.message}
                >
                  <Input
                    id={`${prefix}-${name}`}
                    type={name === "email" ? "email" : "text"}
                    disabled={busy || (command !== null && !correcting) || blocked}
                    aria-invalid={!!form.formState.errors[name]}
                    {...form.register(name)}
                  />
                </Field>
              ))}
            </div>
            <Button type="submit" disabled={busy || blocked}>
              {command && !correcting ? "Retry same account command" : "Create staff account"}
            </Button>
          </form>
          {create.error instanceof ApiError && create.error.status === 400 && (
            <Button
              variant="outline"
              disabled={busy}
              onClick={() => {
                setCorrecting(true);
                create.reset();
              }}
            >
              Correct rejected contact
            </Button>
          )}
          <form
            aria-label="Recover staff account command"
            noValidate
            className="space-y-3 border-t pt-4"
            onSubmit={recovery.handleSubmit(({ id }) => read.mutate(id))}
          >
            <Field
              id={`${prefix}-recover`}
              label="Saved account command reference"
              error={recovery.formState.errors.id?.message}
            >
              <Input
                id={`${prefix}-recover`}
                {...recovery.register("id")}
                disabled={busy || blocked || command !== null}
              />
            </Field>
            <Button type="submit" variant="outline" disabled={busy || blocked || command !== null}>
              Recover saved account command
            </Button>
          </form>
        </>
      )}
      {(receipt || command) && (
        <p className="break-all text-sm">
          Account command reference: {receipt?.command_id ?? command?.id}. Keep this reference to
          recover after closing the page.
        </p>
      )}
      {command && !receipt && (
        <Button
          variant="outline"
          disabled={busy || blocked}
          onClick={() => read.mutate(command.id)}
        >
          Check account command
        </Button>
      )}
      {receipt && (
        <>
          <p role="status">
            {created
              ? "Account created. No access has been granted."
              : "Account creation is unresolved. Recheck the retained command before continuing."}
          </p>
          <Button
            variant="outline"
            disabled={busy || blocked}
            onClick={() => read.mutate(receipt.command_id)}
          >
            Refresh account command
          </Button>
          {created && (
            <div className="flex flex-wrap gap-3">
              <Button
                disabled={busy || blocked || delivered || ready}
                onClick={() => {
                  const id = delivery ?? newStaffAccountCommandId();
                  setDelivery(id);
                  observe.reset();
                  send.mutate(id);
                }}
              >
                {delivery ? "Retry same email delivery" : "Send private setup email"}
              </Button>
              <Button variant="outline" disabled={busy || blocked} onClick={() => observe.mutate()}>
                Check recipient setup
              </Button>
            </div>
          )}
          {send.isSuccess && (
            <p role="status">
              Setup delivery: {send.data.state.replaceAll("_", " ")}
              {send.data.expires_at ? ` · expires ${formatDateTime(send.data.expires_at)}` : ""}.
              Only the recipient receives the setup link.
            </p>
          )}
          {delivered &&
            !ready &&
            ["email_action_required", "password_action_required", "expired", "superseded"].includes(
              send.data.state,
            ) && (
              <Button
                variant="outline"
                disabled={busy || blocked}
                onClick={() => {
                  setDelivery(null);
                  send.reset();
                }}
              >
                Start a new delivery attempt
              </Button>
            )}
          {observe.isSuccess && !ready && (
            <p role="status">
              Recipient setup is incomplete or unavailable. The recipient must finish private
              verification and password setup before enrollment.
            </p>
          )}
          {ready && !blocked && (
            <StaffMembershipForm
              key={receipt.user_id}
              scope={scope}
              initialSubject={receipt.user_id}
              onEnrolled={(subject) => {
                setEnrolled(true);
                onEnrolled?.(subject);
              }}
            />
          )}
        </>
      )}
      {enrolled && (
        <Button
          variant="outline"
          onClick={() => {
            setCommand(null);
            setReceipt(null);
            setDelivery(null);
            setEnrolled(false);
            setCorrecting(false);
            create.reset();
            read.reset();
            observe.reset();
            send.reset();
            form.reset();
            recovery.reset();
          }}
        >
          Set up another staff login
        </Button>
      )}
      {[create, read, observe, send].map(
        (operation, index) =>
          operation.isError && (
            <p role="alert" key={index}>
              {blocked
                ? "Access or source state was refused. Reload the workspace and recover the saved command before continuing."
                : operation.error instanceof ApiError && operation.error.status === 400
                  ? operation.error.message
                  : "The result may be unknown. Recheck or retry the same retained command. Do not create a replacement account."}
            </p>
          ),
      )}
    </Panel>
  );
}
