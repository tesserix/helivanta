"use client";

import { ApiError } from "@helivanta/api";
import { Button } from "@tesserix/web";
import { Panel } from "./panel";

export const refusedDefault = "This wasn't found, or you don't have access to it.";
export const sessionEnded = "Your session has ended. Sign in again to continue.";

/** True when the server refused or hid the records, or the session has ended. */
export function isRefusal(error: unknown): boolean {
  return error instanceof ApiError && [401, 403, 404].includes(error.status);
}

export function RequestFailure({
  message,
  retry,
  error,
  refused,
}: {
  message: string;
  retry: () => unknown;
  /** The failed request's error. Required, so no screen can forget a refusal (#1591). */
  error: unknown;
  /** Said instead of the default when the server refused or hid the records. */
  refused?: string;
}) {
  // Fail closed: retrying cannot change an authorization answer, and offering
  // it would say the records exist and merely failed to arrive. An error whose
  // status can't be read keeps the retry, which discloses nothing.
  if (isRefusal(error))
    return (
      <Panel as="div" role="alert">
        <p className="text-sm">
          {(error as ApiError).status === 401 ? sessionEnded : (refused ?? refusedDefault)}
        </p>
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
