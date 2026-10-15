import { afterEach, expect, it } from "vitest";
import {
  beginTenantTransition,
  cancelTenantTransition,
  tenantTransitionPending,
  onTenantTransition,
} from "./tenant-transition";

afterEach(() => cancelTenantTransition());
it("notifies before a tenant change and keeps operations blocked until a failed switch is cancelled", () => {
  const observations: boolean[] = [];
  const unsubscribe = onTenantTransition(() => observations.push(tenantTransitionPending()));
  beginTenantTransition();
  expect(tenantTransitionPending()).toBe(true);
  expect(observations).toEqual([true]);
  cancelTenantTransition();
  expect(observations).toEqual([true, false]);
  unsubscribe();
  beginTenantTransition();
  expect(observations).toEqual([true, false]);
});
