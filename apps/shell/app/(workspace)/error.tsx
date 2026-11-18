"use client";

import { ErrorScreen } from "@/components/error-screen";

// Inside the workspace layout, so a failing page is replaced by this message
// while the rail, panel and idle tracker stay mounted (#1091). The root
// app/error.tsx would replace the layout itself. HmsShell already renders
// <main>, so this is a section.
export default function WorkspaceError({ error, reset }: { error: Error; reset: () => void }) {
  return <ErrorScreen error={error} reset={reset} source="workspace" />;
}
