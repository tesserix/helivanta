"use client";

import { useCallback, useSyncExternalStore } from "react";
import { z } from "@/lib/csp-zod";
import type { PharmacyStocktakeScope } from "@helivanta/api";

const id = z.string().guid();
const markerSchema = z
  .object({ kind: z.enum(["start", "count", "decision"]), stocktakeId: id, commandId: id })
  .strict()
  .refine((value) => value.kind !== "start" || value.stocktakeId === value.commandId);
const markersSchema = z
  .array(markerSchema)
  .max(8)
  .refine((values) => new Set(values.map(markerIdentity)).size === values.length);
export type StocktakeRecoveryMarker = z.infer<typeof markerSchema>;
const markerEvent = "helivanta:stocktake-recovery-updated";
const unavailable = "unavailable";

export class StocktakeRecoveryError extends Error {
  constructor() {
    super(
      "Stocktake recovery references are unavailable. Check browser storage or resolve pending results before retrying.",
    );
    this.name = "StocktakeRecoveryError";
  }
}
function markerIdentity(value: StocktakeRecoveryMarker) {
  return `${value.kind}:${value.stocktakeId}:${value.commandId}`;
}
function storageKey(scope: PharmacyStocktakeScope) {
  return (
    "helivanta:stocktake-recovery:v1:" +
    JSON.stringify([
      id.parse(scope.tenantId),
      z.string().min(1).max(200).parse(scope.subject),
      id.parse(scope.pharmacyId),
      id.parse(scope.outletId),
    ])
  );
}
export function pendingStocktakeCommands(scope: PharmacyStocktakeScope): StocktakeRecoveryMarker[] {
  try {
    const value = sessionStorage.getItem(storageKey(scope));
    if (value !== null && value.length > 4096) throw new StocktakeRecoveryError();
    return value === null ? [] : markersSchema.parse(JSON.parse(value));
  } catch {
    throw new StocktakeRecoveryError();
  }
}
export function retainStocktakeCommand(
  scope: PharmacyStocktakeScope,
  value: StocktakeRecoveryMarker,
) {
  try {
    const marker = markerSchema.parse(value);
    const current = pendingStocktakeCommands(scope);
    if (current.some((row) => markerIdentity(row) === markerIdentity(marker))) return;
    const next = JSON.stringify(markersSchema.parse([...current, marker]));
    const key = storageKey(scope);
    sessionStorage.setItem(key, next);
    if (sessionStorage.getItem(key) !== next) throw new StocktakeRecoveryError();
    window.dispatchEvent(new Event(markerEvent));
  } catch {
    throw new StocktakeRecoveryError();
  }
}
export function forgetStocktakeCommand(
  scope: PharmacyStocktakeScope,
  value: StocktakeRecoveryMarker,
): boolean {
  try {
    const marker = markerSchema.parse(value);
    const remaining = pendingStocktakeCommands(scope).filter(
      (row) => markerIdentity(row) !== markerIdentity(marker),
    );
    const key = storageKey(scope);
    const next = remaining.length ? JSON.stringify(remaining) : null;
    if (next === null) sessionStorage.removeItem(key);
    else sessionStorage.setItem(key, next);
    if (sessionStorage.getItem(key) !== next) return false;
    window.dispatchEvent(new Event(markerEvent));
    return true;
  } catch {
    // A confirmed stock command remains successful if its browser hint cannot
    // be cleared. Retain the hint for later acknowledgement instead.
    return false;
  }
}
function subscribe(notify: () => void) {
  window.addEventListener(markerEvent, notify);
  window.addEventListener("storage", notify);
  return () => {
    window.removeEventListener(markerEvent, notify);
    window.removeEventListener("storage", notify);
  };
}
function serverSnapshot() {
  return null;
}
export function useStocktakeRecovery(scope: PharmacyStocktakeScope) {
  const { tenantId, subject, pharmacyId, outletId } = scope;
  const getSnapshot = useCallback(() => {
    try {
      const values = pendingStocktakeCommands({ tenantId, subject, pharmacyId, outletId });
      return values.length ? JSON.stringify(values) : null;
    } catch {
      return unavailable;
    }
  }, [tenantId, subject, pharmacyId, outletId]);
  const snapshot = useSyncExternalStore(subscribe, getSnapshot, serverSnapshot);
  return {
    markers:
      snapshot === null || snapshot === unavailable
        ? []
        : markersSchema.parse(JSON.parse(snapshot)),
    unavailable: snapshot === unavailable,
    refresh: () => window.dispatchEvent(new Event(markerEvent)),
  };
}
