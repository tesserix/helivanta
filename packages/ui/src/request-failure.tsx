"use client";

import { ApiError } from "@helivanta/api";
import { Button } from "@tesserix/web";
import { Panel } from "./panel";

export function RequestFailure({
  message,
  retry,
  error,
  refused,
}: {
  message: string;
  retry: () => unknown;
  /** The failed request's error; with `refused`, a refusal shows as one. */
  error?: unknown;
  /** Said instead of `message` when the server refused or hid the records. */
  refused?: string;
}) {
  // Fail closed: retrying cannot change an authorization answer, and offering
  // it would say the records exist and merely failed to arrive.
  if (refused && error instanceof ApiError && [401, 403, 404].includes(error.status))
    return (
      <Panel as="div" role="alert">
        <p className="text-sm">{refused}</p>
      </Panel>
    );
  return (
    <Panel as="div" role="alert">
      <p className="text-sm">{message}</p>
      <Button className="mt-3" variant="outline" onClick={() => void retry()}>
        Try again
      </Button>
    </Panel>
  );
}
