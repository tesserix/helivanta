"use client";

import type { ReactNode } from "react";
import { usePathname } from "next/navigation";
import { AppProviders } from "@helivanta/api";
import { isPublicPath } from "@/lib/session-routes";

export function ApplicationProviders({ children }: { children: ReactNode }) {
  const pathname = usePathname();
  return <AppProviders redirectOnSessionChange={!isPublicPath(pathname)}>{children}</AppProviders>;
}
