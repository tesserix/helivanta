import { accountScopedRequest, type AccountRequestScope } from "./account-scoped-request";
import { apiFetch } from "./client";
import {
  PublicReleaseSnapshotContract,
  ReleaseSnapshotContract,
  RELEASE_POLICY_VERSION,
  releaseDefinitions,
  releasePages,
  releasePanels,
  type PublicReleaseSnapshot,
  type ReleaseSnapshot,
} from "./release-capabilities-contract";

export type ReleaseCapabilityKey = (typeof releaseDefinitions)[number]["key"];
export type { ReleaseSnapshot, PublicReleaseSnapshot } from "./release-capabilities-contract";

function currentSnapshot(value: PublicReleaseSnapshot | ReleaseSnapshot) {
  const issued = Date.parse(value.issued_at);
  const expires = Date.parse(value.expires_at);
  const now = Date.now();
  if (
    value.policy_version !== RELEASE_POLICY_VERSION ||
    issued > now + 5_000 ||
    expires <= now ||
    expires <= issued ||
    expires - issued > 60_000
  ) {
    throw new Error("Feature availability changed. Refresh this page.");
  }
}

export async function readReleaseCapabilities(scope: AccountRequestScope, signal?: AbortSignal) {
  const value = ReleaseSnapshotContract.parse(
    await accountScopedRequest(scope, "/workspace/release-capabilities", signal),
  );
  currentSnapshot(value);
  if (
    value.tenant_id !== scope.tenantId.toLowerCase() ||
    value.subject !== scope.subject ||
    value.capabilities.some((row, index) => row.key !== releaseDefinitions[index]?.key)
  ) {
    throw new Error("The current account changed. Refresh this page.");
  }
  const available = new Map(value.capabilities.map((row) => [row.key as string, row.available]));
  for (const definition of releaseDefinitions) {
    if (
      available.get(definition.key) &&
      definition.dependencies.some((key) => !available.get(key))
    ) {
      throw new Error("Feature availability could not be verified.");
    }
  }
  return value;
}

export async function readPublicReleaseCapabilities(signal?: AbortSignal) {
  const value = PublicReleaseSnapshotContract.parse(
    await apiFetch("/auth/release-capabilities", { signal, cache: "no-store" }),
  );
  currentSnapshot(value);
  return value;
}

export function capabilityForPage(pathname: string): ReleaseCapabilityKey | "baseline" | undefined {
  const exact = releasePages.find((page) => page.path === pathname);
  if (exact) return exact.capability;
  const segments = pathname.split("/");
  return releasePages.find((page) => {
    const pattern = page.path.split("/");
    return (
      pattern.length === segments.length &&
      pattern.every(
        (segment, index) =>
          segment === segments[index] || (segment.startsWith(":") && !!segments[index]),
      )
    );
  })?.capability;
}

export function capabilityForPanel(key: string): ReleaseCapabilityKey | undefined {
  return releasePanels.find((panel) => panel.key === key)?.capability;
}
