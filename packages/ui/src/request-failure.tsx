"use client";

import { Button } from "@tesserix/web";
import { Panel } from "./panel";

export function RequestFailure({ message, retry }: { message: string; retry: () => unknown }) {
  return (
    <Panel as="div" role="alert">
      <p className="text-sm">{message}</p>
      <Button className="mt-3" variant="outline" onClick={() => void retry()}>
        Try again
      </Button>
    </Panel>
  );
}
