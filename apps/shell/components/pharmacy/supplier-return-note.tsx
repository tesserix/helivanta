"use client";

import { useEffect, useRef, useState } from "react";
import { Button } from "@tesserix/web";
import { RequestFailure, isRefusal } from "@helivanta/ui";
import {
  fetchPharmacySupplierReturnNote,
  tenantTransitionPending,
  type PharmacyActorScope,
  type PharmacySupplierReturn,
} from "@helivanta/api";

export function SupplierReturnNoteDownload({
  scope,
  value,
  disabled,
}: {
  scope: PharmacyActorScope;
  value: PharmacySupplierReturn;
  disabled: boolean;
}) {
  const controller = useRef<AbortController | null>(null);
  const [pending, setPending] = useState(false);
  const [failure, setFailure] = useState<unknown>(null);
  const [complete, setComplete] = useState(false);
  useEffect(() => () => controller.current?.abort(), []);

  async function download() {
    if (controller.current || tenantTransitionPending()) return;
    const current = new AbortController();
    controller.current = current;
    setPending(true);
    setFailure(null);
    setComplete(false);
    try {
      const file = await fetchPharmacySupplierReturnNote(scope, value, current.signal);
      current.signal.throwIfAborted();
      if (tenantTransitionPending()) return;
      const url = URL.createObjectURL(file.blob);
      try {
        const link = document.createElement("a");
        link.href = url;
        link.download = file.filename;
        link.click();
      } finally {
        URL.revokeObjectURL(url);
      }
      setComplete(true);
    } catch (error) {
      if (!current.signal.aborted) setFailure(error);
    } finally {
      if (!current.signal.aborted) {
        controller.current = null;
        setPending(false);
      }
    }
  }

  if (value.state !== "dispatched") return null;
  return (
    <div className="my-4 space-y-2">
      <Button
        variant="outline"
        disabled={disabled || pending || isRefusal(failure)}
        onClick={download}
      >
        {pending ? "Verifying commercial note…" : "Download commercial return note"}
      </Button>
      <p role="status" className="text-sm text-muted-foreground">
        {complete
          ? "Download started. Open the note to print or save as PDF."
          : "A commercial record of the outward handoff. Supplier credit is recorded separately."}
      </p>
      {failure !== null && (
        <RequestFailure
          message="The complete commercial note could not be verified. No file was released."
          error={failure}
          retry={download}
          recheck="Check return-note access"
        />
      )}
    </div>
  );
}
