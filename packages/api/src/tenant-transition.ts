"use client";
import { useSyncExternalStore } from "react";
let pending = false;
const listeners = new Set<() => void>();
export function tenantTransitionPending(): boolean {
  return pending;
}
export function onTenantTransition(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}
export function beginTenantTransition(): void {
  pending = true;
  for (const listener of listeners) listener();
}
export function cancelTenantTransition(): void {
  pending = false;
  for (const listener of listeners) listener();
}
export function useTenantTransition(): boolean {
  return useSyncExternalStore(onTenantTransition, tenantTransitionPending, () => false);
}
