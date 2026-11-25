"use client";
import { createContext, useContext, type ReactNode } from "react";

// On the home page a module that is not available yet links to the roadmap
// instead of carrying a Planned or Future badge; the roadmap page keeps the
// badges (#1837). The home page sets this around the chapters it shows.
const RoadmapLinksContext = createContext(false);

export function RoadmapLinks({ children }: { children: ReactNode }) {
  return <RoadmapLinksContext.Provider value>{children}</RoadmapLinksContext.Provider>;
}

export const useRoadmapLinks = () => useContext(RoadmapLinksContext);
