"use client";

import { Button } from "@tesserix/web";

/**
 * LoadMore renders nothing when there is nothing more.
 *
 * That absence is the point: it is a positive statement that the client has
 * the whole collection, which is what #816's silently-truncated lists could
 * never say. A button that stayed visible and did nothing would leave the
 * same ambiguity #816 exists to remove.
 */
export function LoadMore({
  hasMore,
  isLoading,
  onClick,
}: {
  hasMore: boolean;
  isLoading: boolean;
  onClick: () => void;
}) {
  if (!hasMore) return null;
  return (
    <div className="border-t px-5 py-3">
      <Button variant="outline" size="sm" onClick={onClick} disabled={isLoading}>
        {isLoading ? "Loading…" : "Load more"}
      </Button>
    </div>
  );
}
