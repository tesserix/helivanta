"use client";

import { useEffect, useRef, useState } from "react";
import {
  ApiError,
  fetchPharmacyStockCSV,
  fetchPharmacyStockOverviewCSV,
  type PharmacyStockOverviewScope,
  tenantTransitionPending,
  type PharmacyScope,
} from "@helivanta/api";
import { Button } from "@tesserix/web";

type Props = { filters: string; disabled: boolean } & (
  { scope: PharmacyScope; overview?: false } | { scope: PharmacyStockOverviewScope; overview: true }
);

export function StockExport(props: Props) {
  const { filters, disabled } = props;
  const controller = useRef<AbortController | null>(null);
  const [pending, setPending] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  useEffect(() => () => controller.current?.abort(), []);

  async function download() {
    if (controller.current || tenantTransitionPending()) return;
    const current = new AbortController();
    controller.current = current;
    setPending(true);
    setMessage("");
    setError("");
    try {
      const signal = AbortSignal.any([current.signal, AbortSignal.timeout(20_000)]);
      const file = await (props.overview
        ? fetchPharmacyStockOverviewCSV(props.scope, filters, signal)
        : fetchPharmacyStockCSV(props.scope, filters, signal));
      signal.throwIfAborted();
      if (tenantTransitionPending()) return;
      const objectURL = URL.createObjectURL(file.blob);
      try {
        const link = document.createElement("a");
        link.href = objectURL;
        link.download = file.filename;
        link.click();
      } finally {
        URL.revokeObjectURL(objectURL);
      }
      setMessage("CSV download started.");
    } catch (cause) {
      if (!current.signal.aborted)
        setError(
          cause instanceof ApiError
            ? cause.message
            : cause instanceof Error && cause.name === "TimeoutError"
              ? "CSV preparation timed out. Narrow the filters and try again."
              : "Could not download the stock CSV. Try again.",
        );
    } finally {
      if (!current.signal.aborted) {
        controller.current = null;
        setPending(false);
      }
    }
  }

  return (
    <div className="space-y-2">
      <Button variant="outline" disabled={disabled || pending} onClick={download}>
        {pending ? "Preparing CSV…" : "Download CSV"}
      </Button>
      <p role="status" aria-label="Stock CSV export" className="text-sm text-muted-foreground">
        {message || (pending ? "Preparing all matching stock positions…" : "")}
      </p>
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
    </div>
  );
}
