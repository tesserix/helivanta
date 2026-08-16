"use client";

import {
  Button,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@tesserix/web";

// The idle-timeout warning (design spec D5). This is a MODAL, deliberately
// not a toast: a toast on a terminal nobody is watching is invisible, and
// it's easy to miss on glancing back — which is exactly the moment this
// warning exists for. Unsaved clinical work is expensive to lose. Do not
// "simplify" this into a toast later.
//
// Not `ConfirmDialog` (packages/ui/src/confirm-dialog.tsx): that primitive
// is reserved for destructive, hard-to-undo actions
// (docs/standards/frontend.md §5 — deletes and similar). Staying signed in
// is neither destructive nor an undo, so this composes the same underlying
// `@tesserix/web` `Dialog` primitives directly instead of reaching for a
// primitive built for the wrong situation.
export interface IdleWarningProps {
  /**
   * Seconds until the session's server-side `idle_deadline`. The caller
   * recomputes this on every tick from the deadline it holds, never by
   * decrementing a local counter (spec "Errors and failure handling": the
   * countdown is a display derived from the server's deadline, never the
   * client's own arithmetic on a locally-stored timestamp) — this
   * component only renders whatever value it is given.
   */
  secondsRemaining: number;
  /**
   * Fired by the "Stay signed in" button AND by any dismissal (Escape,
   * clicking the overlay). Dismissing the warning must extend the session,
   * not merely hide it — hiding it without extending would leave the
   * clinician thinking they're safe while the session ends underneath
   * them, which is worse than never having warned them at all.
   */
  onStay: () => void;
}

function formatCountdown(totalSeconds: number): string {
  const safeSeconds = Math.max(0, Math.round(totalSeconds));
  const minutes = Math.floor(safeSeconds / 60);
  const seconds = safeSeconds % 60;
  return `${minutes}:${seconds.toString().padStart(2, "0")}`;
}

// A coarse, screen-reader-only announcement, separate from the visible
// digit countdown. Rounds to the minute so assistive tech announces
// progress roughly once a minute rather than being spammed every second
// (spec D5 accessibility note).
function announceRemaining(totalSeconds: number): string {
  const minutes = Math.ceil(Math.max(0, totalSeconds) / 60);
  if (minutes <= 0) return "Your session is about to end because of inactivity.";
  if (minutes === 1) return "About 1 minute remaining before your session ends from inactivity.";
  return `About ${minutes} minutes remaining before your session ends from inactivity.`;
}

export function IdleWarning({ secondsRemaining, onStay }: IdleWarningProps) {
  const countdown = formatCountdown(secondsRemaining);
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        // Escape / overlay-click both route through here. Both count as
        // "stay" — see onStay's doc comment above for why a silent
        // dismissal is not an acceptable outcome.
        if (!open) onStay();
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Still there?</DialogTitle>
          <DialogDescription>
            This session will end in{" "}
            <span className="font-medium tabular-nums text-foreground">{countdown}</span> because
            of inactivity. Select &ldquo;Stay signed in&rdquo; to keep working.
          </DialogDescription>
        </DialogHeader>
        <p aria-live="polite" className="sr-only">
          {announceRemaining(secondsRemaining)}
        </p>
        <DialogFooter>
          <Button onClick={onStay}>Stay signed in</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
