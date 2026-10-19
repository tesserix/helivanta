"use client";

import { Skeleton } from "@tesserix/web";

// Inside the workspace layout, so a page still loading keeps the chrome
// around it (#1091). No <main> wrapper: HmsShell already renders one.
export default function Loading() {
  return <Skeleton className="h-32 w-full max-w-2xl" />;
}
