"use client";

import type { ReactNode } from "react";
import { Button } from "@tesserix/web";
import { isRefusal } from "./request-failure";

/** Re-reads data that loaded, e.g. "Refresh policies" (#1595). */
export function RefreshButton({
  refresh,
  error,
  busy,
  variant = "outline",
  children,
}: {
  refresh: () => unknown;
  /** The read's error. Required, so a refused screen never offers one. */
  error: unknown;
  busy?: boolean;
  variant?: "outline" | "ghost" | "link";
  children: ReactNode;
}) {
  // Asking again can't change an access answer, and the screen shows the
  // refusal itself through RequestFailure.
  if (isRefusal(error)) return null;
  return (
    <Button type="button" variant={variant} disabled={busy} onClick={() => void refresh()}>
      {children}
    </Button>
  );
}
