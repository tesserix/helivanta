import { useEffect, useState } from "react";

// A render-safe clock for time-relative labels; refreshes so they never go stale on screen.
export function useNow(refreshMs = 30_000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const interval = setInterval(() => setNow(Date.now()), refreshMs);
    return () => clearInterval(interval);
  }, [refreshMs]);
  return now;
}
