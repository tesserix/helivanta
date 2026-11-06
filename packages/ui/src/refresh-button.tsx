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
  "aria-label": label,
}: {
  refresh: () => unknown;
  /** The read's error. Required, so a refused screen never offers one. */
  error: unknown;
  busy?: boolean;
  variant?: "outline" | "ghost" | "link";
  children: ReactNode;
  /** Names the button when several refresh the same way, e.g. one per panel. */
  "aria-label"?: string;
}) {
  // Asking again can't change an access answer, and the screen shows the
  // refusal itself through RequestFailure.
  if (isRefusal(error)) return null;
  return (
    <Button
      type="button"
      variant={variant}
      disabled={busy}
      aria-label={label}
      onClick={() => void refresh()}
    >
      {children}
    </Button>
  );
}
