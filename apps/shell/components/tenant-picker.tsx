"use client";

import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import {
  beginTenantTransition,
  cancelTenantTransition,
  clearPermissionsCache,
  clearRenewAt,
  notifySessionChanged,
  useApiMutation,
  useApiQuery,
  accountProfileSchema,
} from "@helivanta/api";
import {
  Button,
  ConfirmDialog,
  endZitadelSession,
  SIGNED_OUT_MARK,
  useConfirmLeave,
} from "@helivanta/ui";

export function TenantPicker() {
  const profileQuery = useApiQuery(["iam", "me", "profile"], "/iam/me/profile");
  const profile = accountProfileSchema.safeParse(profileQuery.data);
  const tenantName =
    !profileQuery.isError && profile.success ? profile.data.tenant_name : undefined;
  const [confirming, setConfirming] = useState(false);
  const confirmLeave = useConfirmLeave();
  const cache = useQueryClient();
  const switchAccount = useApiMutation(
    async () => {
      beginTenantTransition();
      try {
        const result = await fetch("/logout", { method: "POST" });
        if (!result.ok || (await result.json()).ok !== true) {
          throw new Error(
            "Account switching could not be confirmed. Your current account has not been replaced. Please try again.",
          );
        }
      } catch {
        cancelTenantTransition();
        throw new Error(
          "Account switching could not be confirmed. Your current account has not been replaced. Please try again.",
        );
      }
      clearPermissionsCache();
      clearRenewAt();
      cache.clear();
      notifySessionChanged();
      await endZitadelSession(SIGNED_OUT_MARK);
    },
    { suppressErrorToast: true },
  );

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-3">
        <span
          role="status"
          aria-label="Current tenant"
          title={tenantName}
          className="max-w-48 truncate text-sm font-medium"
        >
          {tenantName ?? (profileQuery.isPending ? "Loading account…" : "Account name unavailable")}
        </span>
        <Button
          variant="outline"
          onClick={() => confirmLeave(() => setConfirming(true))}
          disabled={switchAccount.isPending}
        >
          Switch account
        </Button>
      </div>
      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title="Switch account?"
        description="This signs out your current account on all devices. You can then choose another hospital or pharmacy account using its own password and two-step verification."
        confirmLabel="Sign out and choose account"
        busy={switchAccount.isPending}
        onConfirm={() => {
          setConfirming(false);
          switchAccount.mutate();
        }}
      />
      {switchAccount.error ? (
        <p role="alert" className="text-sm text-destructive">
          {switchAccount.error.message}
        </p>
      ) : null}
    </div>
  );
}
