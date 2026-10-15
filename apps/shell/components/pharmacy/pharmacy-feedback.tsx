"use client";

import { Button } from "@tesserix/web";

export function PharmacyFailure({ message, retry }: { message: string; retry: () => unknown }) {
  return (
    <div role="alert" className="rounded-xl border bg-card p-5">
      <p className="text-sm">{message}</p>
      <Button className="mt-3" variant="outline" onClick={() => void retry()}>
        Try again
      </Button>
    </div>
  );
}
