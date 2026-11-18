"use client";

import { useEffect, useId, useState } from "react";
import { Button } from "@tesserix/web";
import { reportFrontendError } from "@/lib/frontend-errors-client";

export function ErrorScreen({
  error,
  reset,
  source,
}: {
  error: Error;
  reset: () => void;
  source: "root" | "workspace" | "global";
}) {
  const heading = useId();
  const [reported, setReported] = useState<{ error: Error; reference?: string } | null>(null);
  useEffect(() => {
    let active = true;
    const complete = (reference?: string) => {
      if (active) setReported({ error, reference });
    };
    void reportFrontendError(error, source).then(
      (report) => complete(report?.reference),
      () => complete(),
    );
    return () => {
      active = false;
    };
  }, [error, source]);

  const workspace = source === "workspace";
  const Container = workspace ? "section" : "main";
  const Heading = workspace ? "h2" : "h1";
  const ready = reported?.error === error;
  const reference = ready ? reported?.reference : undefined;
  return (
    <Container
      aria-labelledby={heading}
      className={
        workspace
          ? "flex flex-col items-center justify-center gap-4 p-6 py-16 text-center"
          : "flex min-h-screen flex-col items-center justify-center gap-4 p-6 text-center"
      }
    >
      <Heading id={heading} className="text-xl font-semibold text-foreground">
        Something went wrong
      </Heading>
      <p className="max-w-md text-sm text-muted-foreground">
        The page could not be loaded. Try again, or share this reference with your administrator.
      </p>
      <p role="status" className="max-w-full break-all text-sm">
        {!ready ? (
          "Preparing support reference…"
        ) : reference ? (
          <>
            <span>Support reference: </span>
            <code>{reference}</code>
          </>
        ) : (
          "Support reference unavailable. Please try again."
        )}
      </p>
      <Button onClick={reset}>Try again</Button>
    </Container>
  );
}
