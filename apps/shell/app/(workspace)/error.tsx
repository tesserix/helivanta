"use client";

import { Button } from "@tesserix/web";

// Inside the workspace layout, so a failing page is replaced by this message
// while the rail, panel and idle tracker stay mounted (#1091). The root
// app/error.tsx would replace the layout itself. HmsShell already renders
// <main>, so this is a section.
export default function WorkspaceError({ error, reset }: { error: Error; reset: () => void }) {
  return (
    <section className="flex flex-col items-center justify-center gap-4 py-16 text-center">
      <h1 className="text-xl font-semibold text-foreground">Something went wrong</h1>
      <p className="max-w-md text-sm text-muted-foreground">
        {error.message || "An unexpected error occurred."}
      </p>
      <Button onClick={reset}>Try again</Button>
    </section>
  );
}
