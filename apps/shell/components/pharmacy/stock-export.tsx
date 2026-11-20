"use client";

import { useEffect, useRef, useState } from "react";
import {
  ApiError,
  fetchPharmacyStockCSV,
  fetchPharmacyStockOverviewCSV,
  fetchStocktakeCSV,
  type PharmacyStocktakeSummary,
  type PharmacyStockOverviewScope,
  tenantTransitionPending,
  type PharmacyScope,
} from "@helivanta/api";
import { Button } from "@tesserix/web";

type Props = { disabled: boolean } & (
  | { scope: PharmacyScope; filters: string; overview?: false; stocktake?: never }
  | { scope: PharmacyStockOverviewScope; filters: string; overview: true; stocktake?: never }
  | {
      scope: PharmacyScope;
      stocktake: Pick<PharmacyStocktakeSummary, "id" | "number" | "version" | "snapshot_rows">;
      filters?: never;
      overview?: never;
    }
);

export function StockExport(props: Props) {
  const { disabled } = props;
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
      const file = await (props.stocktake
        ? fetchStocktakeCSV(props.scope, props.stocktake, signal)
        : props.overview
          ? fetchPharmacyStockOverviewCSV(props.scope, props.filters, signal)
          : fetchPharmacyStockCSV(props.scope, props.filters, signal));
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
              ? "CSV preparation timed out. Refresh and try again."
              : props.stocktake
                ? "Could not verify the stocktake register. Refresh and try again."
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
        {pending
          ? "Preparing CSV…"
          : props.stocktake
            ? "Download stocktake register"
            : "Download CSV"}
      </Button>
      <p role="status" aria-label="Stock CSV export" className="text-sm text-muted-foreground">
        {message ||
          (pending
            ? props.stocktake
              ? "Verifying the complete stocktake register…"
              : "Preparing all matching stock positions…"
            : "")}
      </p>
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
    </div>
  );
}
