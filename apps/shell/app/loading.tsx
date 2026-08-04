"use client";

import { Skeleton } from "@tesserix/web";

export default function Loading() {
  return (
    <main className="p-6">
      <Skeleton className="h-32 w-full max-w-2xl" />
    </main>
  );
}
