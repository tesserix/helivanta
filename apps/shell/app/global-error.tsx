"use client";

import { ErrorScreen } from "@/components/error-screen";
import "./globals.css";

export default function GlobalError({ error, reset }: { error: Error; reset: () => void }) {
  return (
    <html lang="en">
      <body>
        <ErrorScreen error={error} reset={reset} source="global" />
      </body>
    </html>
  );
}
