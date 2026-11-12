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
} from "@helivanta/api";
import {
  Button,
  ConfirmDialog,
  endZitadelSession,
  SIGNED_OUT_MARK,
  useConfirmLeave,
} from "@helivanta/ui";

export function TenantPicker() {
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
      <Button
        variant="outline"
        onClick={() => confirmLeave(() => setConfirming(true))}
        disabled={switchAccount.isPending}
      >
        Switch account
      </Button>
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
