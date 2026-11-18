"use client";

import type { ComponentProps } from "react";
import { useMembershipCandidates, usePermissions, type StaffAssignmentScope } from "@helivanta/api";
import { AccountSelectControl, useAccountPresentation } from "./account-identity";
import { SelectControl } from "./select";
import { LoadMore } from "./load-more";
import { RequestFailure } from "./request-failure";

type Props = Omit<ComponentProps<typeof AccountSelectControl>, "subjects"> & {
  scope?: StaffAssignmentScope;
};

export function MemberAccountSelect({ scope, ...props }: Props) {
  const identity = usePermissions();
  const current =
    scope ??
    (identity.tenantId && identity.subject
      ? { tenantId: identity.tenantId, subject: identity.subject }
      : null);
  const person = useAccountPresentation(props.value);
  if (props.disabled && props.value)
    return (
      <SelectControl
        {...props}
        disabled
        options={[{ value: props.value, label: person?.name ?? "Account details unavailable" }]}
      />
    );
  if (!current) return <p role="status">Checking the current account…</p>;
  return <CurrentMembers scope={current} {...props} />;
}

function CurrentMembers({
  scope,
  ...props
}: Omit<Props, "scope"> & { scope: StaffAssignmentScope }) {
  const members = useMembershipCandidates(scope);
  if (members.isPending) return <p role="status">Loading staff accounts…</p>;
  if (members.isError)
    return (
      <RequestFailure
        message="Could not verify current staff accounts."
        error={members.error}
        retry={members.refetch}
      />
    );
  const subjects = members.data.pages.flatMap((page) => page.data.map((row) => row.subject));
  return (
    <div className="space-y-2">
      <AccountSelectControl
        {...props}
        subjects={subjects}
        disabled={props.disabled || members.isFetching}
      />
      <LoadMore
        hasMore={members.hasNextPage}
        isLoading={members.isFetching}
        onClick={() => {
          void members.fetchNextPage({ cancelRefetch: false });
        }}
      />
    </div>
  );
}
