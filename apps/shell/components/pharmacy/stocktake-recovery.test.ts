import { afterEach, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import type { PharmacyStocktakeScope } from "@helivanta/api";
import {
  pendingStocktakeCommands,
  retainStocktakeCommand,
  forgetStocktakeCommand,
  useStocktakeRecovery,
} from "./stocktake-recovery";
const scope = {
  tenantId: "0199e000-0000-7000-8000-0000000000aa",
  subject: "TEST actor",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};
const marker = {
  kind: "count" as const,
  stocktakeId: "0199e000-0000-7000-8000-000000000002",
  commandId: "0199e000-0000-7000-8000-000000000003",
};
afterEach(() => {
  vi.restoreAllMocks();
  sessionStorage.clear();
});

it("keeps reviewed recovery references separate across authority changes and reloads", () => {
  const operating = { ...scope, authority: "operating" as const };
  retainStocktakeCommand(operating, marker);
  expect(pendingStocktakeCommands(scope)).toEqual([]);
  expect(pendingStocktakeCommands(operating)).toEqual([marker]);
  retainStocktakeCommand(scope, { ...marker, commandId: scope.pharmacyId });
  expect(pendingStocktakeCommands(operating)).toEqual([marker]);
  expect(pendingStocktakeCommands({ ...operating, subject: "TEST another account" })).toEqual([]);
  expect(pendingStocktakeCommands({ ...operating, outletId: scope.pharmacyId })).toEqual([]);
  forgetStocktakeCommand(scope, marker);
  expect(pendingStocktakeCommands(operating)).toEqual([marker]);
});

it("binds the recovery hook to authority changes without losing retained references", () => {
  const operating = { ...scope, authority: "operating" as const };
  const { result, rerender } = renderHook(
    ({ value }: { value: PharmacyStocktakeScope }) => useStocktakeRecovery(value),
    { initialProps: { value: operating } },
  );
  act(() => retainStocktakeCommand(operating, marker));
  expect(result.current.markers).toEqual([marker]);
  rerender({ value: scope });
  expect(result.current.markers).toEqual([]);
  rerender({ value: operating });
  expect(result.current.markers).toEqual([marker]);
});

it("refuses oversized serialized storage before treating it as an empty queue", () => {
  retainStocktakeCommand(scope, marker);
  const key = sessionStorage.key(0)!;
  sessionStorage.setItem(key, "[" + " ".repeat(5000) + "]");
  expect(() => pendingStocktakeCommands(scope)).toThrow(/recovery/i);
});
it("retains only recovery identifiers across reloads and separates actor, tenant and outlet", () => {
  retainStocktakeCommand(scope, marker);
  expect(pendingStocktakeCommands(scope)).toEqual([marker]);
  expect(sessionStorage.getItem(sessionStorage.key(0)!)).toBe(JSON.stringify([marker]));
  expect(pendingStocktakeCommands({ ...scope, subject: "TEST different actor" })).toEqual([]);
  expect(pendingStocktakeCommands({ ...scope, tenantId: scope.pharmacyId })).toEqual([]);
  expect(pendingStocktakeCommands({ ...scope, outletId: scope.pharmacyId })).toEqual([]);
});
it("deduplicates exact references and clears only the confirmed command", () => {
  const other = { ...marker, commandId: scope.pharmacyId };
  retainStocktakeCommand(scope, marker);
  retainStocktakeCommand(scope, marker);
  retainStocktakeCommand(scope, other);
  expect(pendingStocktakeCommands(scope)).toEqual([marker, other]);
  forgetStocktakeCommand(scope, marker);
  expect(pendingStocktakeCommands(scope)).toEqual([other]);
});
it("refuses a ninth reference instead of replacing an unresolved result", () => {
  for (let i = 0; i < 8; i++)
    retainStocktakeCommand(scope, {
      ...marker,
      commandId: `0199e000-0000-7000-8000-00000000000${i}`,
    });
  expect(() => retainStocktakeCommand(scope, { ...marker, commandId: scope.pharmacyId })).toThrow(
    /recovery/i,
  );
  expect(pendingStocktakeCommands(scope)).toHaveLength(8);
});
it("refuses private inputs, corrupt storage and unavailable persistence without erasing references", () => {
  expect(() =>
    retainStocktakeCommand(scope, { ...marker, reason: "TEST private text" } as typeof marker),
  ).toThrow(/recovery/i);
  retainStocktakeCommand(scope, marker);
  sessionStorage.setItem(sessionStorage.key(0)!, "not JSON");
  expect(() => pendingStocktakeCommands(scope)).toThrow(/recovery/i);
  expect(sessionStorage.getItem(sessionStorage.key(0)!)).toBe("not JSON");
  sessionStorage.clear();
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
    throw new DOMException("TEST quota", "QuotaExceededError");
  });
  expect(() => retainStocktakeCommand(scope, marker)).toThrow(/recovery/i);
  expect(sessionStorage.length).toBe(0);
});
