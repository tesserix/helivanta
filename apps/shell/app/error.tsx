"use client";

import { ErrorScreen } from "@/components/error-screen";

export default function ErrorBoundary({ error, reset }: { error: Error; reset: () => void }) {
  return <ErrorScreen error={error} reset={reset} source="root" />;
}
